package agent

import (
	"github.com/smartcs/go-impl/llm"
	"github.com/smartcs/go-impl/memory"
)

func toLLMHistory(history []memory.Message) []llm.Message {
	out := make([]llm.Message, 0, len(history))
	for _, m := range history {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Content == "" {
			continue
		}
		out = append(out, llm.Message{Role: m.Role, Content: m.Content})
	}
	return out
}
