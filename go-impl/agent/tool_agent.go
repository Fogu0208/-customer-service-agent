package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/smartcs/go-impl/llm"
	"github.com/smartcs/go-impl/memory"
	"github.com/smartcs/go-impl/tracing"
)

// ToolAgent 基于 Function Calling 的工具调用 Agent。
// 由模型自主决定调用 order_query / ticket_create / knowledge_search。
type ToolAgent struct {
	llm            *llm.Client
	longTermMemory *memory.LongTermMemory
	tickets        *TicketHandlerAgent
	orders         map[string]map[string]any
	mu             sync.RWMutex
}

func NewToolAgent(client *llm.Client, ltm *memory.LongTermMemory, tickets *TicketHandlerAgent) *ToolAgent {
	a := &ToolAgent{
		llm:            client,
		longTermMemory: ltm,
		tickets:        tickets,
		orders:         make(map[string]map[string]any),
	}
	a.seedOrders()
	return a
}

func (a *ToolAgent) seedOrders() {
	a.orders["ORD-2024-001"] = map[string]any{
		"order_id": "ORD-2024-001",
		"status":   "配送中",
		"product":  "理财产品A",
		"amount":   "10000.00",
		"eta":      "预计2个工作日内签收",
	}
	a.orders["ORD-2024-002"] = map[string]any{
		"order_id": "ORD-2024-002",
		"status":   "已完成",
		"product":  "理财产品A",
		"amount":   "5000.00",
		"eta":      "已于昨日签收",
	}
}

func (a *ToolAgent) Process(state *State) *State {
	return tracing.TraceFunc("tool_agent", "process", func() *State {
		if a.llm == nil || !a.llm.Enabled() {
			state.SubResults["tool_agent"] = "当前未启用大模型，无法执行工具调用。请配置 OPENAI_API_KEY 后重试，或直接说明订单号/工单需求。"
			return state
		}

		system := `你是 Smart CS 智能客服，可以使用工具完成订单查询、工单创建与知识检索。
规则：
1. 需要外部信息时优先调用工具，不要编造订单/工单数据
2. 用户未提供订单号时，先询问订单号（示例 ORD-2024-001）
3. 用简洁中文回复最终结果
4. 不要承诺保本、稳赚等违规表述`

		reply, used, err := a.llm.ChatWithTools(
			system,
			toLLMHistory(state.History),
			state.UserMessage,
			a.toolDefs(),
			a.execute(state),
			4,
		)
		if err != nil || reply == "" {
			msg := "工具调用暂时失败，请稍后重试或联系人工客服。"
			if err != nil {
				msg = msg + "（" + err.Error() + "）"
			}
			state.SubResults["tool_agent"] = msg
			return state
		}

		state.ToolsUsed = used
		state.SubResults["tool_agent"] = reply
		return state
	})
}

func (a *ToolAgent) toolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Type: "function",
			Function: llm.ToolFunctionSchema{
				Name:        "order_query",
				Description: "根据订单号查询订单状态、商品与物流信息",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"order_id": map[string]any{
							"type":        "string",
							"description": "订单号，例如 ORD-2024-001",
						},
					},
					"required": []string{"order_id"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.ToolFunctionSchema{
				Name:        "ticket_create",
				Description: "为用户创建客服工单",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"summary": map[string]any{
							"type":        "string",
							"description": "工单摘要",
						},
						"priority": map[string]any{
							"type":        "string",
							"description": "优先级：low/medium/high",
						},
					},
					"required": []string{"summary"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.ToolFunctionSchema{
				Name:        "knowledge_search",
				Description: "检索产品、退款、开户等知识库内容",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "检索关键词或问题",
						},
					},
					"required": []string{"query"},
				},
			},
		},
	}
}

func (a *ToolAgent) execute(state *State) llm.ToolExecutor {
	return func(name, argsJSON string) (string, error) {
		var args map[string]any
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}

		switch name {
		case "order_query":
			orderID, _ := args["order_id"].(string)
			orderID = strings.TrimSpace(orderID)
			a.mu.RLock()
			order, ok := a.orders[orderID]
			a.mu.RUnlock()
			if !ok {
				return mustJSON(map[string]any{
					"found":    false,
					"order_id": orderID,
					"message":  "未找到该订单，请确认订单号",
				}), nil
			}
			return mustJSON(map[string]any{"found": true, "order": order}), nil

		case "ticket_create":
			summary, _ := args["summary"].(string)
			priority, _ := args["priority"].(string)
			if strings.TrimSpace(summary) == "" {
				summary = state.UserMessage
			}
			if priority == "" {
				priority = "medium"
			}
			ticket := a.tickets.createTicketWithPriority(state.UserID, summary, priority)
			return mustJSON(map[string]any{
				"ticket_id":  ticket.ID,
				"status":     ticket.Status,
				"priority":   ticket.Priority,
				"created_at": ticket.CreatedAt,
			}), nil

		case "knowledge_search":
			query, _ := args["query"].(string)
			docs := a.longTermMemory.Search(query, 3)
			items := make([]map[string]string, 0, len(docs))
			for _, d := range docs {
				items = append(items, map[string]string{
					"source":  d.Source,
					"content": d.Content,
				})
			}
			return mustJSON(map[string]any{"results": items}), nil
		}

		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}

func (a *ToolAgent) Name() string {
	return "tool_agent"
}
