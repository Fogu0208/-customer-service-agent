package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/smartcs/go-impl/agent"
	"github.com/smartcs/go-impl/memory"
	"github.com/smartcs/go-impl/mcp"
	"github.com/smartcs/go-impl/tracing"

	"github.com/gin-gonic/gin"
)

// Server HTTP API服务（基于Gin框架）
type Server struct {
	supervisor   *agent.SupervisorAgent
	shortTermMem *memory.ShortTermMemory
	longTermMem  *memory.LongTermMemory
	mcpServer    *mcp.MCPToolServer
	engine       *gin.Engine
}

type ChatRequest struct {
	Message   string `json:"message" binding:"required"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

func NewServer(
	supervisor *agent.SupervisorAgent,
	stm *memory.ShortTermMemory,
	ltm *memory.LongTermMemory,
	mcpServer *mcp.MCPToolServer,
) *Server {
	s := &Server{
		supervisor:   supervisor,
		shortTermMem: stm,
		longTermMem:  ltm,
		mcpServer:    mcpServer,
	}

	gin.SetMode(gin.ReleaseMode)
	s.engine = gin.Default()
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	api := s.engine.Group("/api")
	{
		api.POST("/chat", s.handleChat)
		api.POST("/chat/stream", s.handleChatStream)
		api.GET("/history/:sessionId", s.handleHistory)
		api.GET("/tools", s.handleListTools)
		api.GET("/metrics", s.handleMetrics)
	}
	s.engine.GET("/health", s.handleHealth)
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	state := s.newState(&req)
	result := s.supervisor.Orchestrate(c.Request.Context(), state)

	s.shortTermMem.AddMessage(req.SessionID, "assistant", result.FinalResponse)

	c.JSON(http.StatusOK, gin.H{
		"response":          result.FinalResponse,
		"session_id":        req.SessionID,
		"intent":            result.Intent,
		"tools_used":        result.ToolsUsed,
		"compliance_passed": result.CompliancePassed,
	})
}

// handleChatStream 用 SSE 推送同一条编排链路的执行过程：
//
//	meta    会话建立，返回 session_id
//	node    进入某个图节点，可据此展示实际走过的编排路径
//	delta   通过合规闸门的文本增量
//	replace 流式内容与最终响应不一致时（模板兜底、合规拦截）的整段纠正
//	done    最终元信息，含首字延迟
//
// 约定：delta 只是首字延迟的优化，replace 与 done 才是权威结果。
func (s *Server) handleChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	// 关闭反向代理的响应缓冲，否则增量会被攒成一整包再下发
	header.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	send := func(event string, payload gin.H) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}

	state := s.newState(&req)
	send("meta", gin.H{"session_id": req.SessionID})

	start := time.Now()
	var (
		firstTokenMs  int64
		gotFirstToken bool
	)

	guard := s.supervisor.NewOutputGuard(func(delta string) {
		if !gotFirstToken {
			gotFirstToken = true
			firstTokenMs = time.Since(start).Milliseconds()
		}
		send("delta", gin.H{"text": delta})
	})

	state.SetTokenSink(guard.Delta)
	state.SetNodeSink(func(node string) {
		send("node", gin.H{"node": node})
	})

	result := s.supervisor.Orchestrate(c.Request.Context(), state)

	passed, violations := guard.Close()
	final := result.FinalResponse
	if !passed {
		// 闸门在合规节点之前就已拦下内容，最终响应必须换成安全话术
		final = agent.BlockedResponse
		result.CompliancePassed = false
	}
	if strings.TrimSpace(guard.Sent()) != strings.TrimSpace(final) {
		send("replace", gin.H{"text": final})
	}

	s.shortTermMem.AddMessage(req.SessionID, "assistant", final)

	var firstToken any
	if gotFirstToken {
		firstToken = firstTokenMs
		tracing.RecordMetric("stream_first_token", firstTokenMs, true)
	}

	send("done", gin.H{
		"session_id":        req.SessionID,
		"intent":            result.Intent,
		"tools_used":        result.ToolsUsed,
		"compliance_passed": result.CompliancePassed,
		"violations":        violations,
		"first_token_ms":    firstToken,
		"total_ms":          time.Since(start).Milliseconds(),
	})
}

// newState 补齐请求默认值，并在写入本轮用户消息之前取出历史，
// 保证 Agent 拿到的是“当前轮之前”的上下文。
func (s *Server) newState(req *ChatRequest) *agent.State {
	if req.UserID == "" {
		req.UserID = "anonymous"
	}
	if req.SessionID == "" {
		req.SessionID = generateSessionID()
	}

	priorHistory := s.shortTermMem.GetHistory(req.SessionID)
	s.shortTermMem.AddMessage(req.SessionID, "user", req.Message)

	return agent.NewState(req.UserID, req.SessionID, req.Message, priorHistory)
}

func (s *Server) handleHistory(c *gin.Context) {
	sessionID := c.Param("sessionId")
	history := s.shortTermMem.GetHistory(sessionID)
	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"messages":   history,
	})
}

func (s *Server) handleListTools(c *gin.Context) {
	tools := s.mcpServer.ListTools()
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

func (s *Server) handleMetrics(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"agent_metrics": tracing.GetMetrics(),
	})
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy", "version": "1.1.0"})
}

func (s *Server) Run(addr string) error {
	return s.engine.Run(addr)
}

func generateSessionID() string {
	return "sess-" + randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下退化为时间相关值，避免阻塞请求
		return hex.EncodeToString([]byte("fallback0"))[:n]
	}
	return hex.EncodeToString(b)[:n]
}
