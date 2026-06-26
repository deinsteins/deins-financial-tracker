package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetIntent_ToolCall(t *testing.T) {
	// Mock server that returns a tool call response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		var response ChatCompletionResponse
		response.Choices = make([]struct {
			Message struct {
				Role      string "json:\"role\""
				Content   string "json:\"content\""
				ToolCalls []struct {
					ID       string "json:\"id\""
					Type     string "json:\"type\""
					Function struct {
						Name      string "json:\"name\""
						Arguments string "json:\"arguments\""
					} "json:\"function\""
				} "json:\"tool_calls\""
			} "json:\"message\""
		}, 1)
		
		choice := &response.Choices[0]
		choice.Message.Role = "assistant"
		choice.Message.ToolCalls = []struct {
			ID       string "json:\"id\""
			Type     string "json:\"type\""
			Function struct {
				Name      string "json:\"name\""
				Arguments string "json:\"arguments\""
			} "json:\"function\""
		}{
			{
				ID:   "call_1",
				Type: "function",
				Function: struct {
					Name      string "json:\"name\""
					Arguments string "json:\"arguments\""
				}{
					Name:      "save_transaction",
					Arguments: `{"type": "expense", "amount": 25000, "category": "food", "description": "makan bakso"}`,
				},
			},
		}

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewHermesClient(ClientConfig{
		APIURL: server.URL,
		Model:  "test-model",
		APIKey: "test-key",
	})

	intent, err := client.GetIntent(context.Background(), nil, "makan bakso 25rb")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(intent.ToolCalls) == 0 {
		t.Fatal("expected at least 1 tool call, got 0")
	}

	tc := intent.ToolCalls[0]
	if tc.ToolName != "save_transaction" {
		t.Errorf("expected tool name save_transaction, got %q", tc.ToolName)
	}

	if tc.Params["type"] != "expense" {
		t.Errorf("expected type expense, got %v", tc.Params["type"])
	}

	if amount, ok := tc.Params["amount"].(float64); !ok || amount != 25000 {
		t.Errorf("expected amount 25000, got %v", tc.Params["amount"])
	}
}

func TestGetIntent_ConversationalResponse(t *testing.T) {
	// Mock server that returns a plain content message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		var response ChatCompletionResponse
		response.Choices = make([]struct {
			Message struct {
				Role      string "json:\"role\""
				Content   string "json:\"content\""
				ToolCalls []struct {
					ID       string "json:\"id\""
					Type     string "json:\"type\""
					Function struct {
						Name      string "json:\"name\""
						Arguments string "json:\"arguments\""
					} "json:\"function\""
				} "json:\"tool_calls\""
			} "json:\"message\""
		}, 1)
		
		choice := &response.Choices[0]
		choice.Message.Role = "assistant"
		choice.Message.Content = "Halo bro! Ada yang bisa gua bantu hari ini?"

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewHermesClient(ClientConfig{
		APIURL: server.URL,
		Model:  "test-model",
	})

	intent, err := client.GetIntent(context.Background(), nil, "halo")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(intent.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(intent.ToolCalls))
	}

	if intent.Response != "Halo bro! Ada yang bisa gua bantu hari ini?" {
		t.Errorf("expected response %q, got %q", "Halo bro! Ada yang bisa gua bantu hari ini?", intent.Response)
	}
}

func TestGetIntent_DynamicToolsPayload(t *testing.T) {
	var capturedRequest ChatCompletionRequest

	// Mock server that captures the request structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			_ = json.Unmarshal(bodyBytes, &capturedRequest)
		}

		var response ChatCompletionResponse
		response.Choices = make([]struct {
			Message struct {
				Role      string "json:\"role\""
				Content   string "json:\"content\""
				ToolCalls []struct {
					ID       string "json:\"id\""
					Type     string "json:\"type\""
					Function struct {
						Name      string "json:\"name\""
						Arguments string "json:\"arguments\""
					} "json:\"function\""
				} "json:\"tool_calls\""
			} "json:\"message\""
		}, 1)
		
		choice := &response.Choices[0]
		choice.Message.Role = "assistant"
		choice.Message.Content = "Done"

		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	registry := NewRegistry()
	mockTool := &GetTodaySummaryTool{
		Handler: func(ctx context.Context, telegramID int64) (interface{}, error) {
			return "today", nil
		},
	}
	registry.Register(mockTool)

	client := NewHermesClient(ClientConfig{
		APIURL:   server.URL,
		Model:    "test-model",
		Registry: registry,
	})

	_, err := client.GetIntent(context.Background(), nil, "cek rekap")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(capturedRequest.Tools) != 1 {
		t.Fatalf("expected 1 tool in payload, got %d", len(capturedRequest.Tools))
	}

	tool := capturedRequest.Tools[0]
	if tool.Function.Name != "get_today_summary" {
		t.Errorf("expected tool name get_today_summary, got %s", tool.Function.Name)
	}

	if tool.Function.Description != "Retrieve today's financial summary or general daily report." {
		t.Errorf("incorrect tool description: %s", tool.Function.Description)
	}
}
