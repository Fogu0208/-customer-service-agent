package agent

import (
	"github.com/smartcs/go-impl/llm"
	"github.com/smartcs/go-impl/tracing"
)

// ChitchatAgent 闲聊/寒暄 Agent，用大模型自然回复。
type ChitchatAgent struct {
	llm *llm.Client
}

func NewChitchatAgent(client *llm.Client) *ChitchatAgent {
	return &ChitchatAgent{llm: client}
}

func (a *ChitchatAgent) Process(state *State) *State {
	return tracing.TraceFunc("chitchat", "process", func() *State {
		if a.llm != nil && a.llm.Enabled() {
			system := `你是 Smart CS 智能客服。用简洁友好的中文回复打招呼或闲聊。
可以简要介绍你能帮忙查询理财产品、退款政策、开户流程，以及协助创建工单。不要编造具体产品数据。`
			if reply, err := a.llm.Chat(system, state.UserMessage); err == nil && reply != "" {
				state.SubResults["chitchat"] = reply
				return state
			}
		}

		state.SubResults["chitchat"] = "您好！我是 Smart CS 智能客服，可以帮您咨询理财产品、退款政策、开户流程，或协助创建工单。请问需要什么帮助？"
		return state
	})
}

func (a *ChitchatAgent) Name() string {
	return "chitchat"
}
