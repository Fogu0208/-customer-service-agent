package agent

import (
	"fmt"
	"strings"

	"github.com/smartcs/go-impl/llm"
	"github.com/smartcs/go-impl/memory"
	"github.com/smartcs/go-impl/tracing"
)

// KnowledgeRAGAgent 知识检索Agent — RAG：检索 + 大模型生成。
type KnowledgeRAGAgent struct {
	longTermMemory *memory.LongTermMemory
	llm            *llm.Client
}

func NewKnowledgeRAGAgent(ltm *memory.LongTermMemory, client *llm.Client) *KnowledgeRAGAgent {
	return &KnowledgeRAGAgent{longTermMemory: ltm, llm: client}
}

func (a *KnowledgeRAGAgent) Process(state *State) *State {
	return tracing.TraceFunc("knowledge_rag", "process", func() *State {
		query := state.UserMessage
		docs := a.longTermMemory.Search(query, 3)

		if a.llm != nil && a.llm.Enabled() {
			answer, err := a.generateWithLLM(query, docs)
			if err == nil && answer != "" {
				state.SubResults["knowledge_rag"] = answer
				return state
			}
		}

		state.SubResults["knowledge_rag"] = a.fallbackAnswer(docs)
		return state
	})
}

func (a *KnowledgeRAGAgent) generateWithLLM(query string, docs []memory.Document) (string, error) {
	var ctx strings.Builder
	if len(docs) == 0 {
		ctx.WriteString("（知识库未检索到直接匹配文档）")
	} else {
		for i, doc := range docs {
			ctx.WriteString(fmt.Sprintf("[%d] 来源:%s\n%s\n\n", i+1, doc.Source, doc.Content))
		}
	}

	system := `你是金融/电商场景的智能客服助手。请基于给定知识库片段用中文简洁回答用户。
要求：
1. 语气自然友好，像真人客服，不要生硬念文档
2. 只能依据知识库内容作答；知识不足时明确说明并建议联系人工
3. 不要承诺保本、稳赚等违规表述
4. 不要编造知识库没有的产品细节`

	user := fmt.Sprintf("知识库片段：\n%s\n用户问题：%s", ctx.String(), query)
	return a.llm.Chat(system, user)
}

func (a *KnowledgeRAGAgent) fallbackAnswer(docs []memory.Document) string {
	if len(docs) == 0 {
		return "抱歉，知识库中暂未找到与您问题相关的信息。建议您联系人工客服获取帮助。"
	}

	var docParts []string
	for _, doc := range docs {
		docParts = append(docParts, fmt.Sprintf("【%s】%s", doc.Source, doc.Content))
	}
	return fmt.Sprintf(
		"根据知识库检索结果，为您回答如下：\n\n%s\n\n以上信息仅供参考，具体以合同条款为准。如需进一步帮助，请联系人工客服。",
		strings.Join(docParts, "\n\n"),
	)
}

func (a *KnowledgeRAGAgent) Name() string {
	return "knowledge_rag"
}
