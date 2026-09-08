package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client 调用 OpenAI 兼容的 Chat Completions API。
type Client struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// Message 对话消息（支持多轮与 tool 协议）。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef OpenAI function/tool 定义。
type ToolDef struct {
	Type     string             `json:"type"`
	Function ToolFunctionSchema `json:"function"`
}

type ToolFunctionSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolExecutor 根据工具名与 JSON 参数返回结果文本。
type ToolExecutor func(name, argsJSON string) (string, error)

// Sink 接收流式响应中的增量文本。传 nil 表示调用方不需要流式输出。
type Sink func(delta string)

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	Tools       []ToolDef `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func NewFromEnv() (*Client, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY 未配置")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://api.siliconflow.cn/v1"
	}

	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "deepseek-ai/DeepSeek-V3"
	}

	return &Client{
		apiKey:  key,
		baseURL: baseURL,
		model:   model,
		http: &http.Client{
			Timeout: 90 * time.Second,
		},
	}, nil
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

// Chat 单轮便捷方法。
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	return c.ChatHistory(ctx, system, nil, user)
}

// ChatHistory 带多轮上下文的对话。
func (c *Client) ChatHistory(ctx context.Context, system string, history []Message, user string) (string, error) {
	return c.ChatHistoryStream(ctx, system, history, user, nil)
}

// ChatHistoryStream 与 ChatHistory 相同，但生成的文本会实时经 onDelta 回调输出。
// onDelta 为 nil 时退回一次性返回。
func (c *Client) ChatHistoryStream(
	ctx context.Context,
	system string,
	history []Message,
	user string,
	onDelta Sink,
) (string, error) {
	msgs := buildMessages(system, history, user)
	resp, err := c.dispatch(ctx, msgs, nil, onDelta)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// ChatWithTools 带 Function Calling 的多轮工具调用循环。
func (c *Client) ChatWithTools(
	ctx context.Context,
	system string,
	history []Message,
	user string,
	tools []ToolDef,
	exec ToolExecutor,
	maxRounds int,
) (final string, usedTools []string, err error) {
	return c.ChatWithToolsStream(ctx, system, history, user, tools, exec, maxRounds, nil)
}

// ChatWithToolsStream 与 ChatWithTools 相同，但每一轮的文本增量都经 onDelta 输出。
// 携带 tool_calls 的那一轮通常没有文本增量，因此实际流出的是工具执行后的总结回复。
func (c *Client) ChatWithToolsStream(
	ctx context.Context,
	system string,
	history []Message,
	user string,
	tools []ToolDef,
	exec ToolExecutor,
	maxRounds int,
	onDelta Sink,
) (final string, usedTools []string, err error) {
	if maxRounds <= 0 {
		maxRounds = 3
	}
	msgs := buildMessages(system, history, user)
	used := make([]string, 0)

	for round := 0; round < maxRounds; round++ {
		resp, callErr := c.dispatch(ctx, msgs, tools, onDelta)
		if callErr != nil {
			return "", used, callErr
		}

		if len(resp.ToolCalls) == 0 {
			return strings.TrimSpace(resp.Content), used, nil
		}

		assistantMsg := Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		msgs = append(msgs, assistantMsg)

		for _, tc := range resp.ToolCalls {
			used = append(used, tc.Function.Name)
			result, execErr := exec(tc.Function.Name, tc.Function.Arguments)
			if execErr != nil {
				result = fmt.Sprintf(`{"error":%q}`, execErr.Error())
			}
			msgs = append(msgs, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}

	// 最后一轮强制不再带 tools，拿到自然语言总结
	resp, err := c.dispatch(ctx, msgs, nil, onDelta)
	if err != nil {
		return "", used, err
	}
	return strings.TrimSpace(resp.Content), used, nil
}

func buildMessages(system string, history []Message, user string) []Message {
	msgs := make([]Message, 0, len(history)+2)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, Message{Role: "system", Content: system})
	}
	for _, h := range history {
		role := h.Role
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(h.Content) == "" {
			continue
		}
		msgs = append(msgs, Message{Role: role, Content: h.Content})
	}
	msgs = append(msgs, Message{Role: "user", Content: user})
	return msgs
}

// dispatch 按调用方是否需要流式输出，选择 SSE 或一次性请求。
func (c *Client) dispatch(ctx context.Context, messages []Message, tools []ToolDef, onDelta Sink) (*Message, error) {
	if onDelta == nil {
		return c.complete(ctx, messages, tools)
	}
	return c.completeStream(ctx, messages, tools, onDelta)
}

func (c *Client) newRequest(ctx context.Context, messages []Message, tools []ToolDef, stream bool) (*http.Request, error) {
	payload := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.4,
		Stream:      stream,
	}
	if len(tools) > 0 {
		payload.Tools = tools
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return req, nil
}

func (c *Client) complete(ctx context.Context, messages []Message, tools []ToolDef) (*Message, error) {
	if c == nil {
		return nil, fmt.Errorf("llm client 未初始化")
	}

	req, err := c.newRequest(ctx, messages, tools, false)
	if err != nil {
		return nil, err
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析模型响应失败: %w; body=%s", err, truncate(string(raw), 300))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("模型接口错误: %s", parsed.Error.Message)
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("模型接口 HTTP %d: %s", res.StatusCode, truncate(string(raw), 300))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("模型未返回内容")
	}

	msg := parsed.Choices[0].Message
	return &msg, nil
}

// completeStream 以 SSE 方式读取响应：文本增量即时经 onDelta 输出，
// 同时把分片到达的 tool_calls 拼回完整调用，最终仍返回与 complete 等价的 Message。
func (c *Client) completeStream(ctx context.Context, messages []Message, tools []ToolDef, onDelta Sink) (*Message, error) {
	if c == nil {
		return nil, fmt.Errorf("llm client 未初始化")
	}

	req, err := c.newRequest(ctx, messages, tools, true)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		raw, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("模型接口 HTTP %d: %s", res.StatusCode, truncate(string(raw), 300))
	}

	var (
		content strings.Builder
		calls   []ToolCall
	)

	reader := bufio.NewReaderSize(res.Body, 64*1024)
	for {
		line, readErr := reader.ReadString('\n')

		if data, ok := sseData(line); ok {
			if data == "[DONE]" {
				break
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// 心跳或非 JSON 的注释行，跳过即可
				continue
			}
			if chunk.Error != nil && chunk.Error.Message != "" {
				return nil, fmt.Errorf("模型接口错误: %s", chunk.Error.Message)
			}
			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				if delta.Content != "" {
					content.WriteString(delta.Content)
					onDelta(delta.Content)
				}
				calls = mergeToolCallDeltas(calls, delta.ToolCalls)
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}

	if content.Len() == 0 && len(calls) == 0 {
		return nil, fmt.Errorf("模型未返回内容")
	}

	return &Message{Role: "assistant", Content: content.String(), ToolCalls: calls}, nil
}

// sseData 取出一行 SSE 中的 data 字段，非 data 行返回 false。
func sseData(line string) (string, bool) {
	trimmed := strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(trimmed, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")), true
}

// mergeToolCallDeltas 按 index 归并分片到达的 tool_calls：
// id 与函数名一般只出现在首个分片，arguments 则被切成多段陆续送达。
func mergeToolCallDeltas(calls []ToolCall, deltas []toolCallDelta) []ToolCall {
	for _, d := range deltas {
		if d.Index < 0 {
			continue
		}
		for len(calls) <= d.Index {
			calls = append(calls, ToolCall{Type: "function"})
		}

		call := &calls[d.Index]
		if d.ID != "" {
			call.ID = d.ID
		}
		if d.Type != "" {
			call.Type = d.Type
		}
		if d.Function.Name != "" {
			call.Function.Name = d.Function.Name
		}
		call.Function.Arguments += d.Function.Arguments
	}
	return calls
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
