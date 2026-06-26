package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const SystemPrompt = `You are a helpful, smart personal finance assistant named Hermes.
Your main job is to orchestrate user actions by selecting the most appropriate tool based on user queries.
Analyze the user's input, extract necessary details, and call the relevant function/tool.

Here are the guidelines for decision-making:
1. If the user wants to record or save a transaction (e.g., spending, purchasing, earning, salary, or transfer received), call 'save_transaction'.
   - Determine if it's an 'expense' or 'income'.
   - Extract the amount of money. If the user uses casual Indonesian currency terms like:
     * 'rb' / 'ribu' -> multiply by 1,000 (e.g., '25rb' -> 25000, '300 ribu' -> 300000)
     * 'jt' / 'juta' -> multiply by 1,000,000 (e.g., '1.5jt' -> 1500000, '2 juta' -> 2000000)
   - Assign the transaction to a normalized category: 'food' (makan/kopi/cafe), 'transport' (bensin/ojek/mrt/kereta), 'utilities' (wifi/listrik/air/pulsa), 'entertainment' (nonton/game/holiday), 'salary' (gaji/gajian), or 'other'.
   - Clean up and sanitize the description (e.g., remove amount and slang numbers, keep the text description like 'beli kopi' or 'gaji bulanan').
2. If the user wants to see today's summary, today's transactions, or what they spent/received today, call 'get_today_summary'.
3. If the user wants a monthly summary, monthly report, or report for this/last month, call 'get_month_summary'.
4. If the user wants to get a list of recent transactions, call 'get_transactions'.
5. If the user asks for financial analysis, tips, budget evaluation, or general AI budget audit, call 'analyze_spending'.
6. If the user wants to set, change, or update their total monthly spending budget limit (e.g. 'set budget bulanan gua 5 juta', 'atur limit sebulan 2jt'), call 'set_monthly_budget'.
   - Extract the amount (integer), normalizing slang like 'jt' / 'rb'.
7. If the user wants to set, change, or update their spending budget limit for a specific category (e.g. 'budget jajan sebulan 500rb', 'set limit bensin 200rb', 'limit kopi 300 ribu'), call 'set_category_budget'.
   - Assign to a normalized category (food, transport, utilities, entertainment, salary, or other).
   - Extract the amount (integer), normalizing slang like 'jt' / 'rb'.

If the query is a simple greeting or general talk that does not match any of these tasks, do not call any tool. Just reply with a helpful conversational message in casual Indonesian (using 'lu', 'gua', etc.).
Additionally, if the user asks follow-up questions about the transactions they just logged or the messages in the conversation history (e.g. 'berapa total tadi?', 'yang paling besar apa?'), answer them conversationally using the information in the chat history instead of calling a tool.`

type ToolCall struct {
	ToolName string                 `json:"tool_name"`
	Params   map[string]interface{} `json:"params"`
	ArgsRaw  string                 `json:"args_raw"`
}

type ToolIntent struct {
	ToolCalls []ToolCall             `json:"tool_calls,omitempty"`
	Response  string                 `json:"response,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMFunction struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  Parameters `json:"parameters"`
}

type LLMTool struct {
	Type     string      `json:"type"`
	Function LLMFunction `json:"function"`
}

type Parameters struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

type ChatCompletionRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []LLMTool `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

type ClientConfig struct {
	APIURL   string
	Model    string
	APIKey   string
	Registry *Registry
}

type HermesClient interface {
	GetIntent(ctx context.Context, history []Message, userInput string) (*ToolIntent, error)
}

type hermesClient struct {
	apiURL     string
	model      string
	apiKey     string
	registry   *Registry
	httpClient *http.Client
}

func NewHermesClient(cfg ClientConfig) HermesClient {
	apiURL := cfg.APIURL
	model := cfg.Model
	apiKey := cfg.APIKey

	// Fallback logic
	if apiURL == "" {
		if cfg.APIKey != "" {
			// Using Gemini API with OpenAI compatibility
			apiURL = "https://generativelanguage.googleapis.com/v1beta/openai"
			if model == "" {
				model = "gemini-2.5-flash"
			}
		} else {
			// Local Ollama default
			apiURL = "http://localhost:11434/v1"
			if model == "" {
				model = "hermes3"
			}
		}
	} else if model == "" {
		model = "hermes3"
	}

	return &hermesClient{
		apiURL:     apiURL,
		model:      model,
		apiKey:     apiKey,
		registry:   cfg.Registry,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *hermesClient) GetIntent(ctx context.Context, history []Message, userInput string) (*ToolIntent, error) {
	// Define the tools dynamically from the registry
	var tools []LLMTool
	if c.registry != nil {
		for _, t := range c.registry.List() {
			tools = append(tools, LLMTool{
				Type: "function",
				Function: LLMFunction{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  t.Parameters(),
				},
			})
		}
	}

	var messages []Message
	messages = append(messages, Message{Role: "system", Content: SystemPrompt})
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userInput})

	reqPayload := ChatCompletionRequest{
		Model: c.model,
		Messages: messages,
		Tools:      tools,
		ToolChoice: "auto",
	}

	url := fmt.Sprintf("%s/chat/completions", c.apiURL)
	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	var resp *http.Response
	maxRetries := 3
	backoff := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		// Re-create body reader since Do consumes it
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		resp, err = c.httpClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				break
			}
			
			// Retry only if it's a 503 or 429, and we have retries left
			if (resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests) && i < maxRetries-1 {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("LLM API returned temporary status %d (attempt %d/%d). Retrying in %v: %s", resp.StatusCode, i+1, maxRetries, backoff, string(respBody))
				
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoff):
					backoff *= 2
				}
				continue
			}
			
			// Non-retryable status code or last attempt
			break
		} else {
			log.Printf("LLM API connection error (attempt %d/%d): %v", i+1, maxRetries, err)
			if i < maxRetries-1 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoff):
					backoff *= 2
				}
				continue
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to call LLM API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LLM response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal LLM response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in LLM response")
	}

	choice := chatResp.Choices[0]
	if len(choice.Message.ToolCalls) > 0 {
		var calls []ToolCall
		for _, tc := range choice.Message.ToolCalls {
			var params map[string]interface{}
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
					return nil, fmt.Errorf("failed to parse tool arguments for %s: %w", tc.Function.Name, err)
				}
			}
			calls = append(calls, ToolCall{
				ToolName: tc.Function.Name,
				Params:   params,
				ArgsRaw:  tc.Function.Arguments,
			})
		}
		return &ToolIntent{
			ToolCalls: calls,
		}, nil
	}

	// If no tool was called, return the conversational text response
	return &ToolIntent{
		Response: choice.Message.Content,
	}, nil
}
