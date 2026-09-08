package agent

import (
	"context"

	"github.com/smartcs/go-impl/memory"
)

// State 是在Supervisor编排流程中流转的全局上下文。
// 所有子Agent共享读写此State。
type State struct {
	UserID           string                 `json:"user_id"`
	SessionID        string                 `json:"session_id"`
	UserMessage      string                 `json:"user_message"`
	History          []memory.Message       `json:"history"` // 当前轮之前的多轮上下文
	Intent           string                 `json:"intent"`
	SubResults       map[string]interface{} `json:"sub_results"`
	ToolsUsed        []string               `json:"tools_used,omitempty"`
	CompliancePassed bool                   `json:"compliance_passed"`
	FinalResponse    string                 `json:"final_response"`
	CurrentAgent     string                 `json:"current_agent"`
	RetryCount       int                    `json:"retry_count"`

	// 以下字段不参与序列化：由编排图与 HTTP 层注入的运行期钩子。
	ctx    context.Context
	tokens TokenSink
	nodes  NodeSink
}

func NewState(userID, sessionID, message string, history []memory.Message) *State {
	copied := make([]memory.Message, len(history))
	copy(copied, history)
	return &State{
		UserID:           userID,
		SessionID:        sessionID,
		UserMessage:      message,
		History:          copied,
		SubResults:       make(map[string]interface{}),
		CompliancePassed: true,
	}
}

// SetTokenSink 开启流式输出：子 Agent 生成的文本增量会实时回调 sink。
func (s *State) SetTokenSink(sink TokenSink) {
	s.tokens = sink
}

// SetNodeSink 订阅编排进度：每进入一个图节点回调一次。
func (s *State) SetNodeSink(sink NodeSink) {
	s.nodes = sink
}

// Streaming 表示当前请求是否需要流式输出。
func (s *State) Streaming() bool {
	return s != nil && s.tokens != nil
}

// Emit 下发一段文本增量。非流式请求下为空操作。
func (s *State) Emit(delta string) {
	if s.tokens != nil {
		s.tokens(delta)
	}
}

// Ctx 返回当前请求的上下文，由编排图在进入节点时注入。
// 客户端断连后由它把取消信号传导到上游模型调用。
func (s *State) Ctx() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// enter 在节点开始执行时记录上下文与当前节点，并通知订阅方。
func (s *State) enter(ctx context.Context, node string) {
	s.ctx = ctx
	s.CurrentAgent = node
	if s.nodes != nil {
		s.nodes(node)
	}
}

// Agent 接口定义所有子Agent的统一契约
type Agent interface {
	Process(state *State) *State
	Name() string
}
