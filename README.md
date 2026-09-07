# Smart CS · 智能客服多 Agent 系统

> Go + Vue3 实现的企业级多 Agent 智能客服 Demo  
> Supervisor 编排 · RAG 知识问答 · LLM 意图路由 · 合规审查 · 全链路可观测

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs)](https://vuejs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript)](https://www.typescriptlang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

---

## 项目简介

本项目模拟金融 / 电商场景下的智能客服系统，采用 **Supervisor 多 Agent 编排架构**：由中央编排 Agent 识别用户意图，再调度知识检索、工单处理、闲聊接待等专业子 Agent 协同完成回复，并在输出前经过合规审查。

适合作为 **Go / 后端 / AI 应用** 方向的简历项目，可讲清架构选型、Agent 协作链路、RAG 与 LLM 的分工，以及前后端联调落地。

### 你可以在面试中讲清的点

- 为什么用 Supervisor，而不是 Agent 互相直接调用
- 意图路由如何结合「LLM 分类 + 规则兜底」
- RAG 如何「检索文档 → 大模型组织自然语言回答」
- 合规审查如何拦截违规金融话术 / PII
- 短期记忆、工作记忆在多轮对话中的作用
- Gin API + Vue3 前端如何联调与代理

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

### 请求示例

1. 用户：「有哪些理财产品？」
2. 意图路由 → `knowledge_rag`
3. 长期记忆检索相关文档片段
4. LLM 基于片段生成自然回答（不生硬贴原文）
5. 合规审查通过后返回前端

---

## 技术栈

| 层次 | 技术 | 说明 |
|------|------|------|
| 后端语言 | Go 1.22+ | 高并发、云原生友好 |
| HTTP | Gin | REST API |
| 编排模式 | Supervisor Multi-Agent | 中央调度 + 子 Agent |
| LLM | OpenAI 兼容接口 | 意图分类 / RAG 生成 / 闲聊 |
| 记忆 | 进程内工作记忆 + 会话短期记忆 | Redis 可扩展 |
| 知识库 | 关键词 / 中文切分检索 | 演示级，可替换向量库 |
| 前端 | Vue 3 + TypeScript + Vite | 聊天 UI |
| 配置 | godotenv | `.env` 注入密钥 |

---

## 项目结构

```
.
├── README.md
├── LICENSE
├── go-impl/                 # Go 多 Agent 后端
│   ├── main.go              # 启动入口
│   ├── .env.example         # 环境变量模板（不含真实密钥）
│   ├── agent/               # Supervisor / 意图 / RAG / 工单 / 闲聊 / 合规
│   ├── api/                 # Gin HTTP 接口
│   ├── llm/                 # OpenAI 兼容 Chat Completions 客户端
│   ├── memory/              # 工作记忆 / 短期记忆 / 长期知识库
│   ├── mcp/                 # MCP 工具协议（演示）
│   └── tracing/             # Agent 调用耗时与指标
└── frontend/                # Vue3 + TS 聊天前端
    ├── src/api/             # 后端请求封装
    ├── src/components/      # 聊天主界面
    └── vite.config.ts       # 开发代理到 :8090
```

---

## 快速开始

### 环境要求

- Go 1.22+
- Node.js 18+
- 兼容 OpenAI 协议的 LLM API Key（如硅基流动 SiliconFlow 等）

### 1. 配置后端密钥

```bash
cd go-impl
cp .env.example .env
```

编辑 `.env`：

```env
OPENAI_API_KEY=你的密钥
OPENAI_BASE_URL=https://api.siliconflow.cn/v1
OPENAI_MODEL=deepseek-ai/DeepSeek-V3
PORT=8090
```

> 未配置 Key 时，系统会自动回退到规则 / 模板模式，服务仍可启动，但回复会较生硬。

### 2. 启动 Go 后端

```bash
cd go-impl
go mod tidy
go run .
```

默认监听：`http://localhost:8090`

健康检查：

```bash
curl http://localhost:8090/health
```

### 3. 启动 Vue 前端

```bash
cd frontend
npm install
npm run dev
```

浏览器打开：`http://localhost:5173`  
Vite 已将 `/api`、`/health` 代理到后端 `8090`。

### 4. 接口联调（可选）

```bash
curl -X POST http://localhost:8090/api/chat \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"你好\",\"user_id\":\"demo_user\"}"
```

主要接口：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| POST | `/api/chat` | 多 Agent 对话 |
| GET | `/api/history/:sessionId` | 会话历史 |
| GET | `/api/tools` | MCP 工具列表 |
| GET | `/api/metrics` | Agent 调用指标 |

---

## 核心设计说明

### 1. Supervisor 编排

`SupervisorAgent` 统一负责：

1. 意图路由  
2. 分发到子 Agent  
3. 合规审查  
4. 汇总最终回复  

优点：链路清晰、便于追踪与兜底，避免 Agent 之间循环调用。

### 2. LLM + 规则双通道意图识别

- 优先使用大模型输出：`knowledge_rag` / `ticket_handler` / `chitchat`
- 模型异常时回退关键词规则，保证可用性

### 3. RAG 问答

- 先从知识库检索相关片段  
- 再由 LLM 基于片段生成自然中文回答  
- 约束模型「不编造知识库没有的细节」

### 4. 合规审查

规则引擎检测：

- 违规金融话术（如「稳赚不赔」「保证收益」）
- PII（手机号 / 身份证 / 银行卡号）

未通过时返回人工转接话术，而不是直接输出风险内容。

---

## 简历可写版本（示例）

> **智能客服多 Agent 系统（个人项目）**  
> 技术栈：Go / Gin / Vue3 / TypeScript / OpenAI 兼容 LLM  
> - 设计并实现 Supervisor 多 Agent 编排，完成意图路由、知识问答、工单与闲聊协同  
> - 接入 LLM 完成意图分类与 RAG 生成，规则引擎兜底保障可用性  
> - 增加合规审查与会话记忆，提供 Vue3 聊天前端与 Gin REST API 联调落地  

---

## 安全说明

- **不要**将真实 `OPENAI_API_KEY` 提交到 GitHub  
- 仅提交 `.env.example`；本地使用 `.env`（已在 `.gitignore` 中忽略）  
- 本仓库不包含任何真实密钥

---

## 致谢与声明

本项目在多 Agent 智能客服的整体思路上参考了开源项目 [smart-cs-multi-agent](https://github.com/bcefghj/smart-cs-multi-agent)（MIT License）的架构设计，并在此基础上聚焦 **Go 实现**，完成了：

- 大模型 API 真实接入（意图 / RAG / 闲聊）
- Vue3 + TypeScript 聊天前端
- 中文检索与工程化配置（`.env` / 代理联调）

如用于简历展示，请如实描述个人完成部分。

---

## License

MIT License
