package agent

import (
	"fmt"
	"strings"

	"github.com/smartcs/go-impl/llm"
	"github.com/smartcs/go-impl/tracing"
)

// IntentRouterAgent 意图路由Agent。
// 优先用大模型分类，失败时回退到关键词规则。
type IntentRouterAgent struct {
	llm *llm.Client
}

func NewIntentRouterAgent(client *llm.Client) *IntentRouterAgent {
	return &IntentRouterAgent{llm: client}
}

var intentKeywords = map[string][]string{
	"ticket_handler": {
		"退款", "退货", "理赔", "投诉",
		"办理", "工单", "申诉", "注销", "申请退款",
	},
	"tool_agent": {
		"订单", "物流", "快递", "查单", "订单号", "ORD-",
	},
	"chitchat": {
		"你好", "您好", "在吗", "嗨", "hello", "hi",
		"谢谢", "感谢", "再见", "拜拜",
	},
}

func (a *IntentRouterAgent) Process(state *State) *State {
	return tracing.TraceFunc("intent_router", "process", func() *State {
		intent := a.classifyByRules(state.UserMessage)

		if a.llm != nil && a.llm.Enabled() {
			if llmIntent, err := a.classifyByLLM(state); err == nil && llmIntent != "" {
				intent = llmIntent
			}
		}

		state.Intent = intent
		return state
	})
}

func (a *IntentRouterAgent) classifyByLLM(state *State) (string, error) {
	system := `你是智能客服意图分类器。只输出以下之一，不要解释：
knowledge_rag
ticket_handler
tool_agent
chitchat

规则：
- knowledge_rag：产品、政策、流程、收益、开户材料等知识问答
- ticket_handler：退款/投诉/理赔/申诉等需要创建工单的请求
- tool_agent：查询订单状态、物流、需要调用外部工具的操作
- chitchat：打招呼、寒暄、感谢、无关闲聊`

	raw, err := a.llm.ChatHistory(system, toLLMHistory(state.History), state.UserMessage)
	if err != nil {
		return "", err
	}

	text := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(text, "ticket_handler"):
		return "ticket_handler", nil
	case strings.Contains(text, "tool_agent"):
		return "tool_agent", nil
	case strings.Contains(text, "chitchat"):
		return "chitchat", nil
	case strings.Contains(text, "knowledge_rag"):
		return "knowledge_rag", nil
	default:
		return "", fmt.Errorf("无法解析意图: %s", raw)
	}
}

func (a *IntentRouterAgent) classifyByRules(message string) string {
	msg := strings.ToLower(message)
	intent := "knowledge_rag"
	maxScore := 0

	for target, keywords := range intentKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(msg, strings.ToLower(kw)) {
				score++
			}
		}
		if score > maxScore {
			maxScore = score
			intent = target
		}
	}
	return intent
}

func (a *IntentRouterAgent) Name() string {
	return "intent_router"
}
