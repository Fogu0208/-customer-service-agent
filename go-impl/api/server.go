package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

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

	if req.UserID == "" {
		req.UserID = "anonymous"
	}
	if req.SessionID == "" {
		req.SessionID = generateSessionID()
	}

	// 先取历史，再写入本轮 user，保证 Agent 拿到的是“当前轮之前”的上下文
	priorHistory := s.shortTermMem.GetHistory(req.SessionID)
	s.shortTermMem.AddMessage(req.SessionID, "user", req.Message)

	state := agent.NewState(req.UserID, req.SessionID, req.Message, priorHistory)
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
