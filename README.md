# Smart CS · 智能客服多 Agent 系统

Go + Vue3 实现的多 Agent 智能客服系统，面向金融 / 电商客服场景。编排层基于字节跳动 CloudWeGo 的 [Eino](https://github.com/cloudwego/eino) 框架，对话经 SSE 流式返回。

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Eino](https://img.shields.io/badge/Eino-CloudWeGo-00ADD8)](https://github.com/cloudwego/eino)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs)](https://vuejs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript)](https://www.typescriptlang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

---

## 这个仓库主要想讲三件事

**1. 用有向图替代手写 switch 做 Agent 编排。**
链路以 `compose.Graph[*State, *State]` 声明：意图路由节点后接一个条件分支，分派到四个子 Agent，再汇聚回合规审查与结果汇总。新增一个子 Agent 只需注册节点、把节点名加进分支目标集合，编排逻辑本身不动；节点连通性与类型不匹配在 `Compile` 阶段暴露，而不是等到线上某个分支被走到才报错。

**2. 流式输出和「出口统一审查」是冲突的，这里给了一个解法。**
合规审查是图的出口节点，只能看到子 Agent 的完整输出；但流式响应必须在它执行之前就把 token 发出去。做法是把审查前移到输出管道上：每个增量都对已累积文本重做一次规则审查，同时**始终保留 17 个字符的尾巴不下发**，让「保证」+「收益」这类跨 token 拼出的违规词在首次完整出现时仍然被拦在服务端。详见[流式输出与合规闸门](#流式输出与合规闸门)。

**3. 每一层都有降级路径。**
意图识别以大模型分类为主、关键词打分兜底；未配置 API Key 时服务照常启动并走模板回复；RAG 检索为空时明确回答「知识库中没有」而不是编造。

---

## 架构

```
用户 (Vue3 聊天界面)
    │  POST /api/chat/stream          ▲  SSE: meta / node / delta / replace / done
    ▼                                 │
┌────────────────────────────────────────────┐
│            API Gateway (Gin)               │
│  GuardedSink：增量合规审查 + 尾部回压        │
└──────────────────┬─────────────────────────┘
                   ▼
       Eino Graph (compose.Graph[*State, *State])

  START → intent_router ─┬→ knowledge_rag ──┐
                         ├→ tool_agent      │
      (AddBranch 条件分支) ├→ ticket_handler  ├→ compliance → synthesize → END
                         └→ chitchat ───────┘
```

图在服务启动时 `Compile` 成 `Runnable` 并全局复用，每次请求由 Gin handler 传入 `context` 后 `Invoke`。`*State` 作为统一的图输入输出类型在节点间流转，承载会话历史、意图、子 Agent 结果与合规结论；它同时携带两个运行期钩子（token sink 与 node sink），流式输出与编排进度都由此上报，不需要为流式单独搭一条链路。

---

## 核心设计

### 流式输出与合规闸门

`GuardedSink` 位于子 Agent 与 HTTP 响应之间，是流式路径上唯一的出口：

- **前移审查**：每收到一个增量，对已累积的全部文本重跑一次规则审查，命中即停止透传。
- **尾部回压**：只下发 `len(累积文本) - holdback` 之前的部分。`holdback` 由规则集自动算出——取最长敏感词与各 PII 正则最短匹配长度的最大值再减一（当前为 `18 - 1 = 17`，由 18 位身份证号决定）。只有再多一个字符时违规内容才可能首次被完整匹配，因此保留这段尾巴就足以保证「凡是能被规则识别的违规文本，都不会有任何片段先到达用户」。
- **权威结果**：`delta` 只是首字延迟的优化。若最终响应与已下发内容不一致（模板兜底、合规拦截），服务端补发一个 `replace` 事件整段纠正，前端据此覆盖气泡。

实际调用中可以观察到这个回压：`compliance`、`synthesize` 两个节点事件已经发出之后，才会看到最后一个补齐尾巴的 `delta`。

规则引擎是纯字符串与正则匹配，`/api/metrics` 里 `compliance_checker` 的平均耗时为 0ms —— 这道闸门几乎不占延迟预算。

### Eino Graph 编排

`SupervisorAgent` 启动时构图：每个子 Agent 的 `Process` 方法经 `compose.InvokableLambda` 适配为图节点，意图路由后用 `AddBranch` 按 `State.Intent` 做条件分支，四条分支再汇聚到合规审查与结果汇总。

节点包装同时承担两件事：把图提供的 `ctx` 写回 `State`（供子 Agent 传给上游模型调用，客户端断连即可取消），以及上报节点进入事件（前端因此能实时显示实际走过的编排路径）。

### 意图识别

优先用大模型分类为 `knowledge_rag` / `tool_agent` / `ticket_handler` / `chitchat`，模型不可用时回退关键词打分。分类结果直接作为分支条件的目标节点名，路由表和意图标签因此不会各自漂移。

这一步是**阻塞**的：它决定走哪条分支，无法与生成并行，因此构成首字延迟的下限（实测约 1.3s，见下）。

### Function Calling

`ToolAgent` 向模型暴露 `order_query` / `ticket_create` / `knowledge_search` 的 JSON Schema，模型自主决定是否调用与传参，服务端执行后以 `role=tool` 回灌结果。

两个收敛控制：循环最多 4 轮；最后一轮**强制去掉 `tools` 参数**，逼模型输出自然语言总结而不是继续要工具。流式模式下 `tool_calls` 是分片到达的（`id` 与函数名只在首片，`arguments` 被切成多段），由 `mergeToolCallDeltas` 按 `index` 拼回完整调用。

### RAG 问答

先检索知识库片段，再由大模型结合片段与会话历史生成回答，system prompt 约束不得编造知识库中不存在的细节。检索为空时走模板兜底。

### 会话记忆

短期记忆用读写锁保护，保留最近 20 轮的滑动窗口。HTTP 层按「先取历史、再写入本轮用户消息」的顺序构造 `State`，保证注入模型的是当前轮之前的上下文，多轮指代（「那个产品」「刚才说的」）因此可解。

---

## 实测数据

单机、DeepSeek-V3（硅基流动）、知识问答类问题，每组 3 次取平均。样本量小，只用于说明量级：

| 指标 | 阻塞式 `/api/chat` | 流式 `/api/chat/stream` |
|------|------|------|
| 用户看到首个字 | 3729 ms | **2520 ms**（↓ 32%） |
| 完整响应结束 | 3729 ms | 3614 ms |

`/api/metrics` 给出的分节点耗时（8 次请求）：

| 节点 | 平均耗时 | 说明 |
|------|------|------|
| `intent_router` | 1267 ms | 阻塞的分类调用，首字延迟的下限 |
| `knowledge_rag` | 2601 ms | 检索 + 生成 |
| `tool_agent` | 7244 ms | 含一轮工具调用往返 |
| `compliance_checker` | 0 ms | 纯规则匹配 |

结论是流式之后**首字延迟的主要成本已经从生成转移到了路由**：1267ms 的分类调用占了首字时间的一半。下一步优化应针对路由本身（换更小的分类模型、规则先行只在低置信度时问模型、或先乐观流式再校正），而不是继续优化生成侧。

---

## 技术栈

| 层次 | 技术 | 说明 |
|------|------|------|
| 后端 | Go 1.22+ / Gin | REST + SSE |
| 编排 | Eino (CloudWeGo) | Graph 节点 / 条件分支 / 编译执行 |
| LLM | OpenAI 兼容接口 | 意图分类、RAG 生成、Function Calling |
| 记忆 | 工作记忆 + 短期会话记忆 | 进程内，可扩展 Redis |
| 知识库 | 关键词 + 中文二字切分检索 | 可替换为向量库 |
| 前端 | Vue 3 / TypeScript / Vite | 聊天 UI，手写 SSE 解析 |

---

## 项目结构

```
.
├── go-impl/                     # Go 多 Agent 后端
│   ├── main.go                  # 启动入口与依赖装配
│   ├── agent/
│   │   ├── supervisor.go         # Eino 图构建与编译
│   │   ├── state.go              # 图内流转的 State 与运行期钩子
│   │   ├── stream.go             # GuardedSink 流式合规闸门
│   │   ├── intent_router.go      # LLM 分类 + 规则兜底
│   │   ├── knowledge_rag.go      # 检索增强生成
│   │   ├── tool_agent.go         # Function Calling 工具定义与执行
│   │   ├── ticket_handler.go     # 工单创建
│   │   ├── chitchat.go           # 闲聊接待
│   │   └── compliance_checker.go # 敏感词 / PII 规则引擎
│   ├── api/server.go             # Gin 路由，含 SSE 接口
│   ├── llm/client.go             # Chat Completions 客户端（含 SSE 解析）
│   ├── memory/                   # 工作记忆 / 短期记忆 / 长期知识库
│   ├── mcp/                      # 工具注册表
│   └── tracing/                  # 调用耗时与指标
└── frontend/                     # Vue3 + TypeScript 前端
    ├── src/api/client.ts         # 后端请求封装与 SSE 帧解析
    ├── src/components/           # 聊天主界面
    └── vite.config.ts            # 开发代理到后端 8090
```

---

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| POST | `/api/chat` | 多 Agent 对话，一次性返回 |
| POST | `/api/chat/stream` | 多 Agent 对话，SSE 流式返回 |
| GET | `/api/history/:sessionId` | 会话历史 |
| GET | `/api/tools` | 工具注册表 |
| GET | `/api/metrics` | Agent 调用指标 |

`/api/chat/stream` 的事件类型：

| 事件 | 载荷 | 说明 |
|------|------|------|
| `meta` | `session_id` | 会话建立，首个事件 |
| `node` | `node` | 进入某个图节点，可据此展示编排路径 |
| `delta` | `text` | 通过合规闸门的文本增量 |
| `replace` | `text` | 整段纠正，出现即以此为准 |
| `done` | `intent` / `tools_used` / `compliance_passed` / `first_token_ms` / `total_ms` | 最终元信息 |

```bash
curl -N -X POST http://localhost:8090/api/chat/stream \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"理财产品A的收益和期限是多少？\",\"user_id\":\"demo\"}"
```

---

## 快速开始

### 环境要求

- Go 1.22+
- Node.js 18+
- 兼容 OpenAI 协议的 LLM API Key（例如硅基流动）

### 1. 配置并启动后端

```bash
cd go-impl
cp .env.example .env
```

在 `.env` 中填写：

```env
OPENAI_API_KEY=你的密钥
OPENAI_BASE_URL=https://api.siliconflow.cn/v1
OPENAI_MODEL=deepseek-ai/DeepSeek-V3
PORT=8090
```

未配置 Key 时服务仍可启动，自动回退到规则 / 模板回复。

```bash
go mod tidy
go run .          # http://localhost:8090
```

### 2. 启动前端

```bash
cd frontend
npm install
npm run dev       # http://localhost:5173
```

开发环境下 Vite 会把 `/api`、`/health` 代理到后端。

---

## 已知局限

有意保留的简化实现，替换点都已隔离在单个函数内：

- **知识检索**是关键词 + 中文二字切分打分，不是向量检索；替换 `memory.LongTermMemory.Search` 即可接入 Milvus / pgvector。
- **记忆是进程内的**，重启即丢失，`NewShortTermMemory` 已预留 Redis 连接串参数但尚未使用。
- **指标是进程内聚合的**，未接入 OpenTelemetry，导出器可在 `tracing` 包替换。
- **`mcp` 包只是工具注册表**，提供进程内的注册与发现，未实现 MCP 的 JSON-RPC 传输层；工具的真实执行逻辑在 `agent.ToolAgent`。
- **合规规则是启发式的**：银行卡正则 `\d{16,19}` 会误伤长数字串，且与身份证规则区间重叠；生产环境需要二阶段的模型审查。
- **暂无自动化测试**。
- 图内分支是串行的，没有并发 fan-out、按节点重试或超时控制。

---

## 安全说明

- 真实 API Key 仅保存在本地 `.env`，不提交到仓库
- 仓库仅包含 `.env.example` 作为配置模板

---

## 致谢

整体多 Agent 客服架构参考了开源项目 [smart-cs-multi-agent](https://github.com/bcefghj/smart-cs-multi-agent)（MIT License）。本仓库聚焦 Go 实现，在此基础上完成了以下工作：

- 编排层由手写分发迁移到 Eino Graph（节点 / 条件分支 / 编译期校验）
- 接入 OpenAI 兼容大模型，实现意图分类、RAG 生成与 Function Calling 工具调用
- 新增 SSE 流式输出，并用尾部回压的增量审查解决流式与出口合规审查的冲突
- 把请求 `context` 沿图透传到上游模型调用，支持客户端断连后取消
- 补齐多轮会话上下文注入与安全的会话 ID 生成
- 新增 Vue3 + TypeScript 聊天前端与前后端联调配置

---

## License

MIT License
