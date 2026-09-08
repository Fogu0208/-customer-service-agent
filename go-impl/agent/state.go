package agent

import "github.com/smartcs/go-impl/memory"

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

// Agent 接口定义所有子Agent的统一契约
type Agent interface {
	Process(state *State) *State
	Name() string
}
