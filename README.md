# Smart CS · 智能客服多 Agent 系统

Go + Vue3 实现的多 Agent 智能客服系统，面向金融 / 电商客服场景。

采用 Supervisor 编排：中央 Agent 负责意图识别与任务分发，知识检索、工单处理、闲聊接待等子 Agent 协同完成回复，输出前经过合规审查。

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs)](https://vuejs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript)](https://www.typescriptlang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

---

## 功能概览

- **Supervisor 多 Agent 编排**：统一调度意图路由、知识问答、工单、闲聊与合规审查
- **LLM 意图识别**：大模型分类 + 规则兜底，保证服务可用性
- **RAG 知识问答**：检索知识库片段后，由大模型组织自然语言回答
- **合规审查**：拦截违规金融话术与手机号 / 身份证 / 银行卡等 PII
- **会话记忆**：工作记忆记录当前推理状态，短期记忆保留多轮对话
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
┌──────────────────────────────────────────────┐
│           Supervisor 编排 Agent               │
│  工作记忆 · 会话短期记忆 · 调用链追踪          │
└──────┬──────────┬──────────┬─────────────────┘
       ▼          ▼          ▼
  意图路由     知识 RAG     工单处理 / 闲聊
  (LLM+规则)  (检索+生成)   (CRUD / LLM)
       │          │
       └────┬─────┘
            ▼
       合规审查 Agent
            ▼
        最终回复
```

处理流程示例：用户询问「有哪些理财产品？」→ 意图路由到知识 Agent → 检索相关文档 → LLM 基于片段生成回答 → 合规审查 → 返回前端。

---

## 技术栈

| 层次 | 技术 | 说明 |
|------|------|------|
| 后端 | Go 1.22+ / Gin | REST API 与 Agent 编排 |
| 编排 | Supervisor Multi-Agent | 中央调度 + 专业子 Agent |
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
│   ├── agent/               # Supervisor / 意图 / RAG / 工单 / 闲聊 / 合规
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

### Supervisor 编排

由 `SupervisorAgent` 统一完成意图路由、子 Agent 分发、合规审查与结果汇总。链路集中、便于追踪和兜底，避免 Agent 之间循环调用。

### 意图识别

优先使用大模型将请求分类为 `knowledge_rag` / `ticket_handler` / `chitchat`；模型不可用时回退关键词规则。

### RAG 问答

先检索知识库相关片段，再由大模型基于片段生成回答，并约束不得编造知识库中不存在的细节。

### 合规审查

规则引擎检测违规金融表述（如「稳赚不赔」「保证收益」）以及手机号、身份证、银行卡等敏感信息；未通过时返回人工转接提示。

---

## 安全说明

- 真实 API Key 仅保存在本地 `.env`，不要提交到仓库
- 仓库仅包含 `.env.example` 作为配置模板

---

## 致谢

整体多 Agent 客服架构参考了开源项目 [smart-cs-multi-agent](https://github.com/bcefghj/smart-cs-multi-agent)（MIT License）。本仓库在此基础上聚焦 Go 实现，并完成了大模型接入、Vue3 前端与工程化配置。

---

## License

MIT License
