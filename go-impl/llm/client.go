package llm

import (
	"bytes"
	"encoding/json"
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

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	Tools       []ToolDef `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
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
func (c *Client) Chat(system, user string) (string, error) {
	return c.ChatHistory(system, nil, user)
}

// ChatHistory 带多轮上下文的对话。
func (c *Client) ChatHistory(system string, history []Message, user string) (string, error) {
	msgs := buildMessages(system, history, user)
	resp, err := c.complete(msgs, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// ChatWithTools 带 Function Calling 的多轮工具调用循环。
func (c *Client) ChatWithTools(
	system string,
	history []Message,
	user string,
	tools []ToolDef,
	exec ToolExecutor,
	maxRounds int,
) (final string, usedTools []string, err error) {
	if maxRounds <= 0 {
		maxRounds = 3
	}
	msgs := buildMessages(system, history, user)
	used := make([]string, 0)

	for round := 0; round < maxRounds; round++ {
		resp, callErr := c.complete(msgs, tools)
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
	resp, err := c.complete(msgs, nil)
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

func (c *Client) complete(messages []Message, tools []ToolDef) (*Message, error) {
	if c == nil {
		return nil, fmt.Errorf("llm client 未初始化")
	}

	payload := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.4,
	}
	if len(tools) > 0 {
		payload.Tools = tools
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
