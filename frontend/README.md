# Smart CS Frontend

Vue 3 + TypeScript 聊天界面，对接 `go-impl` 智能客服 API。

## 开发

先启动 Go 后端（默认 `8090`），再启动前端：

```bash
# 终端 1
cd go-impl
go run .

# 终端 2
cd frontend
npm install
npm run dev
```

浏览器打开 http://localhost:5173

Vite 已将 `/api` 与 `/health` 代理到 `http://localhost:8090`。
