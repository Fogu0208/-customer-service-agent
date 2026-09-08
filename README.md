# Smart CS · 智能客服多 Agent 系统

Go + Vue3 实现的多 Agent 智能客服系统，面向金融 / 电商客服场景。

基于字节跳动 CloudWeGo 的 [Eino](https://github.com/cloudwego/eino) 框架实现 Supervisor 编排：意图路由节点识别意图后经条件分支进入知识检索、工具调用、工单处理或闲聊接待子 Agent，最终统一经过合规审查节点输出。

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Eino](https://img.shields.io/badge/Eino-CloudWeGo-00ADD8)](https://github.com/cloudwego/eino)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs)](https://vuejs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript)](https://www.typescriptlang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

---

## 功能概览

- **Eino Graph 编排**：以有向图描述 Agent 协作链路，节点、条件分支与汇聚由框架统一调度
- **LLM 意图识别**：大模型分类 + 规则兜底，保证服务可用性
- **Function Calling**：模型自主选择 `order_query` / `ticket_create` / `knowledge_search` 等工具
- **RAG 知识问答**：检索知识库片段后，由大模型结合多轮上下文组织回答
- **合规审查**：拦截违规金融话术与手机号 / 身份证 / 银行卡等 PII
- **会话记忆**：短期记忆保留多轮对话，并注入后续 LLM 调用
- **前后端联调**：Gin REST API + Vue3 聊天界面

---

## 系统架构

```
用户 (Vue3 聊天界面)
        │  HTTP JSON
        ▼
┌──────────────────────┐
│   API Gateway (Gin)  │  /api/chat  /health  /api/metrics
└──────────┬───────────┘
           ▼
     Eino Graph (compose.Graph[*State, *State])

  START → intent_router ─┬→ knowledge_rag ──┐
                         ├→ tool_agent      │
        (AddBranch 条件分支) ├→ ticket_handler  ├→ compliance → synthesize → END
                         └→ chitchat ───────┘
```

图在服务启动时 `Compile` 成 `Runnable`，每次请求由 Gin handler 传入 `context` 后 `Invoke`。
`*State` 作为统一的图输入输出类型在节点间流转，承载会话历史、意图、子 Agent 结果与合规结论。

处理流程示例：
- 知识问答：「有哪些理财产品？」→ 检索文档 → LLM 结合历史生成回答
- 工具调用：「查一下订单 ORD-2024-001」→ 模型选择 `order_query` → 返回订单状态

---

## 技术栈

| 层次 | 技术 | 说明 |
|------|------|------|
| 后端 | Go 1.22+ / Gin | REST API |
| 编排 | Eino (CloudWeGo) | Graph 节点 / 条件分支 / 编译执行 |
| LLM | OpenAI 兼容接口 | 意图分类、RAG 生成、闲聊 |
| 记忆 | 工作记忆 + 短期会话记忆 | 可扩展 Redis |
| 知识库 | 关键词 / 中文切分检索 | 可替换为向量库 |
| 前端 | Vue 3 / TypeScript / Vite | 聊天 UI |
| 配置 | godotenv | 环境变量注入 |

---

## 项目结构

```
.
├── go-impl/                 # Go 多 Agent 后端
│   ├── main.go              # 启动入口
│   ├── .env.example         # 环境变量模板
│   ├── agent/               # Eino 图编排 + 意图 / RAG / 工具 / 工单 / 闲聊 / 合规
│   ├── api/                 # Gin HTTP 接口
│   ├── llm/                 # Chat Completions 客户端
│   ├── memory/              # 工作记忆 / 短期记忆 / 长期知识库
│   ├── mcp/                 # MCP 工具协议
│   └── tracing/             # 调用耗时与指标
└── frontend/                # Vue3 + TypeScript 前端
    ├── src/api/             # 后端请求封装
    ├── src/components/      # 聊天主界面
    └── vite.config.ts       # 开发代理到后端 8090
```

---

## 快速开始

### 环境要求

- Go 1.22+
- Node.js 18+
- 兼容 OpenAI 协议的 LLM API Key（例如硅基流动等）

### 1. 配置后端

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

未配置 Key 时，服务仍可启动，并自动回退到规则 / 模板回复。

### 2. 启动后端

```bash
cd go-impl
go mod tidy
go run .
```

服务地址：`http://localhost:8090`

```bash
curl http://localhost:8090/health
```

### 3. 启动前端

```bash
cd frontend
npm install
npm run dev
```

访问：`http://localhost:5173`  
开发环境下，Vite 会将 `/api`、`/health` 代理到后端。

### 4. API 说明

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| POST | `/api/chat` | 多 Agent 对话 |
| GET | `/api/history/:sessionId` | 会话历史 |
| GET | `/api/tools` | MCP 工具列表 |
| GET | `/api/metrics` | Agent 调用指标 |

```bash
curl -X POST http://localhost:8090/api/chat \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"你好\",\"user_id\":\"demo_user\"}"
```

---

## 核心设计

### Eino Graph 编排

`SupervisorAgent` 在启动时用 `compose.NewGraph[*State, *State]()` 构图：每个子 Agent 的 `Process` 方法通过 `compose.InvokableLambda` 适配为图节点，意图路由后用 `AddBranch` 做条件分支，四条分支再汇聚到合规审查与结果汇总节点。

相比手写 `switch` 分发，图编排把「谁在什么条件下执行」变成声明式的拓扑描述：新增子 Agent 只需注册节点并加入分支目标集合，编排逻辑无需改动；图在 `Compile` 阶段即可校验节点连通性与类型匹配，问题在启动时暴露而非运行时。

### 意图识别

优先使用大模型将请求分类为 `knowledge_rag` / `tool_agent` / `ticket_handler` / `chitchat`；模型不可用时回退关键词规则。分类结果直接作为分支条件的目标节点名。

### RAG 问答

先检索知识库相关片段，再由大模型基于片段与会话历史生成回答，并约束不得编造知识库中不存在的细节。

### Function Calling

`ToolAgent` 向模型暴露 `order_query`、`ticket_create`、`knowledge_search` 等工具。模型自主决定是否调用、传入何种参数；服务端执行工具后将结果回传模型，再生成最终自然语言回复。

### 合规审查

规则引擎检测违规金融表述（如「稳赚不赔」「保证收益」）以及手机号、身份证、银行卡等敏感信息；未通过时返回人工转接提示。

---

## 安全说明

- 真实 API Key 仅保存在本地 `.env`，不要提交到仓库
- 仓库仅包含 `.env.example` 作为配置模板

---



---

## License

MIT License
