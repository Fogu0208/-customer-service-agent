package agent

import (
	"regexp"
	"strings"

	"github.com/smartcs/go-impl/tracing"
)

// ComplianceCheckerAgent 合规审查Agent — 两阶段审查。
// Phase 1: 规则引擎快速检查（敏感词 + PII检测）
// Phase 2: 深度审查（生产环境调用LLM）
type ComplianceCheckerAgent struct {
	forbiddenTerms []string
	piiRules       []piiRule
	holdback       int
}

// piiRule 一条 PII 检测规则。minLen 为该正则最短可能的匹配长度，
// 流式输出据此计算需要暂缓发送的尾部窗口。
type piiRule struct {
	label   string
	pattern *regexp.Regexp
	minLen  int
}

func NewComplianceCheckerAgent() *ComplianceCheckerAgent {
	a := &ComplianceCheckerAgent{
		forbiddenTerms: []string{
			"保证收益", "稳赚不赔", "零风险", "保本保息",
			"最高收益", "预期收益率", "承诺回报",
			"内部消息", "内幕", "暗箱操作",
		},
		piiRules: []piiRule{
			{label: "手机号", pattern: regexp.MustCompile(`1[3-9]\d{9}`), minLen: 11},
			{label: "身份证号", pattern: regexp.MustCompile(`\d{17}[\dXx]`), minLen: 18},
			{label: "银行卡号", pattern: regexp.MustCompile(`\d{16,19}`), minLen: 16},
		},
	}
	a.holdback = a.computeHoldback()
	return a
}

func (a *ComplianceCheckerAgent) Process(state *State) *State {
	return tracing.TraceFunc("compliance_checker", "process", func() *State {
		var contentBuilder strings.Builder
		for key, val := range state.SubResults {
			if key == "compliance" {
				continue
			}
			if str, ok := val.(string); ok {
				contentBuilder.WriteString(str)
				contentBuilder.WriteString("\n")
			}
		}

		content := contentBuilder.String()
		if strings.TrimSpace(content) == "" {
			state.CompliancePassed = true
			return state
		}

		violations := a.CheckText(content)

		passed := len(violations) == 0
		riskLevel := "low"
		if !passed {
			hasPII := false
			for _, v := range violations {
				if strings.Contains(v, "PII") {
					hasPII = true
					break
				}
			}
			if hasPII {
				riskLevel = "high"
			} else {
				riskLevel = "medium"
			}
		}

		state.CompliancePassed = passed
		state.SubResults["compliance"] = map[string]interface{}{
			"passed":     passed,
			"risk_level": riskLevel,
			"violations": violations,
		}

		return state
	})
}

// CheckText 对文本执行规则审查，返回命中的违规项。
func (a *ComplianceCheckerAgent) CheckText(content string) []string {
	var violations []string

	for _, term := range a.forbiddenTerms {
		if strings.Contains(content, term) {
			violations = append(violations, "包含违规金融用语: '"+term+"'")
		}
	}

	for _, rule := range a.piiRules {
		if rule.pattern.MatchString(content) {
			violations = append(violations, "检测到PII信息泄露: "+rule.label)
		}
	}

	return violations
}

// Holdback 返回流式输出需要暂缓发送的尾部字符数。
// 取值为最长规则长度减一：只有累积文本再多一个字符时，
// 跨 token 拼出的违规词才可能首次被完整匹配到。
func (a *ComplianceCheckerAgent) Holdback() int {
	return a.holdback
}

func (a *ComplianceCheckerAgent) computeHoldback() int {
	longest := 0
	for _, term := range a.forbiddenTerms {
		if n := len([]rune(term)); n > longest {
			longest = n
		}
	}
	for _, rule := range a.piiRules {
		if rule.minLen > longest {
			longest = rule.minLen
		}
	}
	if longest <= 1 {
		return 0
	}
	return longest - 1
}

func (a *ComplianceCheckerAgent) Name() string {
	return "compliance_checker"
}
