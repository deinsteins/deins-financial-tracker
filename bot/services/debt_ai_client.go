package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DebtParseResponse mirrors the JSON shape returned by the ai-service's
// POST /parse-debt endpoint (see ai-service/parser_service.py ParsedDebt).
type DebtParseResponse struct {
	Intent      string  `json:"intent"`
	Direction   *string `json:"direction"`
	PersonName  *string `json:"person_name"`
	Amount      *int64  `json:"amount"`
	Description *string `json:"description"`
	DueDate     *string `json:"due_date"`
	Reason      *string `json:"reason"`
}

type DebtAIClient interface {
	ParseDebt(text string) (*DebtParseResponse, error)
}

type debtAIClient struct {
	serviceURL string
	httpClient *http.Client
}

func NewDebtAIClient(serviceURL string) DebtAIClient {
	return &debtAIClient{
		serviceURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *debtAIClient) ParseDebt(text string) (*DebtParseResponse, error) {
	apiURL := fmt.Sprintf("%s/parse-debt", c.serviceURL)

	reqBody, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AI Service connection error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.Unmarshal(body, &errData)
		if detail, ok := errData["detail"]; ok {
			return nil, fmt.Errorf("AI Service returned error: %v", detail)
		}
		return nil, fmt.Errorf("AI Service failed with status %d", resp.StatusCode)
	}

	var parseResp DebtParseResponse
	if err := json.Unmarshal(body, &parseResp); err != nil {
		return nil, fmt.Errorf("received malformed response from AI Service: %w", err)
	}

	if parseResp.Intent == "" {
		return nil, fmt.Errorf("received incomplete response fields from AI Service")
	}

	return &parseResp, nil
}
