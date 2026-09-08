package agent

import "github.com/smartcs/go-impl/llm"

// TokenSink 接收子 Agent 流式生成的增量文本。
type TokenSink func(delta string)

// NodeSink 在编排图进入某个节点时回调，用于把编排进度实时暴露给调用方。
type NodeSink func(node string)

// BlockedResponse 合规拦截时统一返回的话术。
const BlockedResponse = "抱歉，您的请求涉及敏感内容，已转交人工客服处理。工单编号已自动生成，请留意后续通知。"

// GuardedSink 是流式输出的合规闸门。
//
// 合规审查是编排图的出口节点，只能看到子 Agent 的完整输出；但流式响应会在
// 该节点执行之前就把 token 送给用户。GuardedSink 因此把审查前移到管道上：
// 每收到一个增量就对已累积文本重新做一次规则审查，并且始终保留 holdback
// 长度的尾巴不下发，使「保证」+「收益」这类跨 token 拼出的违规内容在首次
// 完整出现时仍然被拦在管道内。
//
// 所有方法都在编排所在的单个 goroutine 上同步调用，无需额外加锁。
type GuardedSink struct {
	checker  *ComplianceCheckerAgent
	out      func(string)
	holdback int

	buf        []rune
	flushed    int
	blocked    bool
	violations []string
}

func NewGuardedSink(checker *ComplianceCheckerAgent, out func(string)) *GuardedSink {
	return &GuardedSink{
		checker:  checker,
		out:      out,
		holdback: checker.Holdback(),
	}
}

// Delta 作为 TokenSink 接入 State，逐个增量做审查与下发。
func (g *GuardedSink) Delta(delta string) {
	if delta == "" || g.blocked {
		return
	}

	g.buf = append(g.buf, []rune(delta)...)

	if violations := g.checker.CheckText(string(g.buf)); len(violations) > 0 {
		g.blocked = true
		g.violations = violations
		return
	}

	if safe := len(g.buf) - g.holdback; safe > g.flushed {
		g.out(string(g.buf[g.flushed:safe]))
		g.flushed = safe
	}
}

// Close 结束流式输出：补一次完整审查，通过则把保留的尾部补发出去。
func (g *GuardedSink) Close() (passed bool, violations []string) {
	if g.blocked {
		return false, g.violations
	}

	if violations := g.checker.CheckText(string(g.buf)); len(violations) > 0 {
		g.blocked = true
		g.violations = violations
		return false, violations
	}

	if g.flushed < len(g.buf) {
		g.out(string(g.buf[g.flushed:]))
		g.flushed = len(g.buf)
	}
	return true, nil
}

// Sent 返回已经下发给调用方的文本，用于和最终响应比对是否需要整段纠正。
func (g *GuardedSink) Sent() string {
	return string(g.buf[:g.flushed])
}

// chat 依据当前请求是否为流式，选择流式或一次性的模型调用。
func chat(client *llm.Client, state *State, system, user string) (string, error) {
	if state.Streaming() {
		return client.ChatHistoryStream(state.Ctx(), system, toLLMHistory(state.History), user, state.Emit)
	}
	return client.ChatHistory(state.Ctx(), system, toLLMHistory(state.History), user)
}
