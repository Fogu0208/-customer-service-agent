package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/smartcs/go-impl/llm"
	"github.com/smartcs/go-impl/tracing"
)

// TicketHandlerAgent 工单处理Agent — 工单创建与流转。
type TicketHandlerAgent struct {
	mu      sync.RWMutex
	tickets map[string]*Ticket
	counter int
	llm     *llm.Client
}

type Ticket struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Summary   string `json:"summary"`
	Priority  string `json:"priority"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func NewTicketHandlerAgent(client *llm.Client) *TicketHandlerAgent {
	return &TicketHandlerAgent{
		tickets: make(map[string]*Ticket),
		llm:     client,
	}
}

func (a *TicketHandlerAgent) Process(state *State) *State {
	return tracing.TraceFunc("ticket_handler", "process", func() *State {
		ticket := a.createTicketWithPriority(state.UserID, state.UserMessage, "medium")

		base := fmt.Sprintf(
			"工单已创建成功！\n\n"+
				"工单号: %s\n"+
				"状态: 已创建\n"+
				"优先级: %s\n"+
				"创建时间: %s\n\n"+
				"我们将尽快处理您的请求，请保存好工单号以便后续查询。",
			ticket.ID, ticket.Priority, ticket.CreatedAt,
		)

		if a.llm != nil && a.llm.Enabled() {
			system := `你是客服助手。根据工单创建结果，用简洁友好的中文向用户确认，保留工单号等关键信息，不要编造额外承诺。`
			user := fmt.Sprintf("用户诉求：%s\n系统结果：%s", state.UserMessage, base)
			if reply, err := a.llm.ChatHistory(system, toLLMHistory(state.History), user); err == nil && reply != "" {
				state.SubResults["ticket_handler"] = reply
				return state
			}
		}

		state.SubResults["ticket_handler"] = base
		return state
	})
}

func (a *TicketHandlerAgent) createTicket(userID, summary string) *Ticket {
	return a.createTicketWithPriority(userID, summary, "medium")
}

func (a *TicketHandlerAgent) createTicketWithPriority(userID, summary, priority string) *Ticket {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.counter++
	now := time.Now()
	ticketID := fmt.Sprintf("TK-%s-%04d", now.Format("20060102"), a.counter)
	if priority == "" {
		priority = "medium"
	}

	ticket := &Ticket{
		ID:        ticketID,
		UserID:    userID,
		Summary:   summary,
		Priority:  priority,
		Status:    "created",
		CreatedAt: now.Format("2006-01-02 15:04:05"),
	}

	a.tickets[ticketID] = ticket
	return ticket
}

func (a *TicketHandlerAgent) Name() string {
	return "ticket_handler"
}
