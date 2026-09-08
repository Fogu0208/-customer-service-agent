package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"

	"github.com/smartcs/go-impl/memory"
	"github.com/smartcs/go-impl/tracing"
)

// 编排图中的节点名。与 IntentRouterAgent 输出的意图标签保持一致，
// 分支条件因此可以直接把意图当作目标节点名使用。
const (
	NodeIntentRouter = "intent_router"
	NodeKnowledgeRAG = "knowledge_rag"
	NodeTicket       = "ticket_handler"
	NodeTool         = "tool_agent"
	NodeChitchat     = "chitchat"
	NodeCompliance   = "compliance"
	NodeSynthesize   = "synthesize"
)

// SupervisorAgent 是中央编排协调者。
// 编排链路由 Eino Graph 描述：意图路由 → 条件分支到子 Agent → 合规审查 → 结果汇总。
type SupervisorAgent struct {
	intentRouter    *IntentRouterAgent
	knowledgeAgent  *KnowledgeRAGAgent
	ticketAgent     *TicketHandlerAgent
	toolAgent       *ToolAgent
	chitchatAgent   *ChitchatAgent
	complianceAgent *ComplianceCheckerAgent
	workingMemory   *memory.WorkingMemory

	runnable compose.Runnable[*State, *State]
}

func NewSupervisorAgent(
	intentRouter *IntentRouterAgent,
	knowledgeAgent *KnowledgeRAGAgent,
	ticketAgent *TicketHandlerAgent,
	toolAgent *ToolAgent,
	chitchatAgent *ChitchatAgent,
	complianceAgent *ComplianceCheckerAgent,
	workingMem *memory.WorkingMemory,
) (*SupervisorAgent, error) {
	s := &SupervisorAgent{
		intentRouter:    intentRouter,
		knowledgeAgent:  knowledgeAgent,
		ticketAgent:     ticketAgent,
		toolAgent:       toolAgent,
		chitchatAgent:   chitchatAgent,
		complianceAgent: complianceAgent,
		workingMemory:   workingMem,
	}

	runnable, err := s.buildGraph(context.Background())
	if err != nil {
		return nil, fmt.Errorf("编排图编译失败: %w", err)
	}
	s.runnable = runnable

	return s, nil
}

// buildGraph 构建并编译 Eino 编排图。
//
//	START → intent_router ─┬→ knowledge_rag ─┐
//	                       ├→ ticket_handler ┤
//	                       ├→ tool_agent     ├→ compliance → synthesize → END
//	                       └→ chitchat ──────┘
func (s *SupervisorAgent) buildGraph(ctx context.Context) (compose.Runnable[*State, *State], error) {
	g := compose.NewGraph[*State, *State]()

	nodes := map[string]func(*State) *State{
		NodeIntentRouter: s.routeIntent,
		NodeKnowledgeRAG: s.knowledgeAgent.Process,
		NodeTicket:       s.ticketAgent.Process,
		NodeTool:         s.toolAgent.Process,
		NodeChitchat:     s.chitchatAgent.Process,
		NodeCompliance:   s.complianceAgent.Process,
		NodeSynthesize:   s.synthesize,
	}

	for name, process := range nodes {
		if err := g.AddLambdaNode(name, asLambda(name, process)); err != nil {
			return nil, err
		}
	}

	// 意图路由后的条件分支：由 State.Intent 决定进入哪个子 Agent。
	subAgents := map[string]bool{
		NodeKnowledgeRAG: true,
		NodeTicket:       true,
		NodeTool:         true,
		NodeChitchat:     true,
	}
	branch := compose.NewGraphBranch(
		func(_ context.Context, state *State) (string, error) {
			if subAgents[state.Intent] {
				return state.Intent, nil
			}
			return NodeChitchat, nil
		},
		subAgents,
	)
	if err := g.AddBranch(NodeIntentRouter, branch); err != nil {
		return nil, err
	}

	if err := g.AddEdge(compose.START, NodeIntentRouter); err != nil {
		return nil, err
	}
	for name := range subAgents {
		if err := g.AddEdge(name, NodeCompliance); err != nil {
			return nil, err
		}
	}
	if err := g.AddEdge(NodeCompliance, NodeSynthesize); err != nil {
		return nil, err
	}
	if err := g.AddEdge(NodeSynthesize, compose.END); err != nil {
		return nil, err
	}

	return g.Compile(ctx)
}

// asLambda 把子 Agent 的 Process 方法适配成 Eino 的 Lambda 节点。
// 图在调用节点时提供的 ctx 会被写回 State，供子 Agent 传给上游模型调用。
func asLambda(name string, process func(*State) *State) *compose.Lambda {
	return compose.InvokableLambda(func(ctx context.Context, state *State) (*State, error) {
		state.enter(ctx, name)
		return process(state), nil
	})
}

// NewOutputGuard 复用图中的合规检查器，为流式响应构造输出闸门。
func (s *SupervisorAgent) NewOutputGuard(out func(string)) *GuardedSink {
	return NewGuardedSink(s.complianceAgent, out)
}

// Orchestrate 执行完整的 Supervisor 编排流程。
func (s *SupervisorAgent) Orchestrate(ctx context.Context, state *State) *State {
	start := time.Now()

	result, err := s.runnable.Invoke(ctx, state)
	if err != nil {
		tracing.RecordMetric("supervisor", time.Since(start).Milliseconds(), false)
		state.FinalResponse = "抱歉，暂时无法处理您的请求，请稍后重试。"
		return state
	}

	tracing.RecordMetric("supervisor", time.Since(start).Milliseconds(), true)
	return result
}

func (s *SupervisorAgent) routeIntent(state *State) *State {
	state = s.intentRouter.Process(state)

	s.workingMemory.Update(state.SessionID, map[string]interface{}{
		"intent":    state.Intent,
		"timestamp": time.Now().Format(time.RFC3339),
	})

	return state
}

func (s *SupervisorAgent) synthesize(state *State) *State {
	if !state.CompliancePassed {
		state.FinalResponse = BlockedResponse
		return state
	}

	var parts []string
	for key, val := range state.SubResults {
		if key == "compliance" {
			continue
		}
		if str, ok := val.(string); ok && str != "" {
			parts = append(parts, str)
		}
	}

	if len(parts) > 0 {
		state.FinalResponse = strings.Join(parts, "\n\n")
	} else {
		state.FinalResponse = "抱歉，暂时无法处理您的请求，请稍后重试。"
	}

	return state
}
