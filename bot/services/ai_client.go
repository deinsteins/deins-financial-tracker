package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"finance-bot/bot/models"
)

type AIParseResponse struct {
	Type        string `json:"type"`
	Category    string `json:"category"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
}

type AITransactionItem struct {
	Type            string `json:"type"`
	Category        string `json:"category"`
	Amount          int64  `json:"amount"`
	Description     string `json:"description"`
	TransactionDate string `json:"transaction_date,omitempty"`
}

type AIAnalyzeRequest struct {
	Transactions []AITransactionItem `json:"transactions"`
}

type AIAnalyzeResponse struct {
	Summary               string   `json:"summary"`
	Insights              []string `json:"insights"`
	Anomalies             []string `json:"anomalies"`
	WastefulSpending      []string `json:"wasteful_spending"`
	HighestSpendingDay    string   `json:"highest_spending_day"`
	Trends                []string `json:"trends"`
	SavingRecommendations []string `json:"saving_recommendations"`
	FinancialScore        int      `json:"financial_score"`
}

type AIClient interface {
	ParseTransaction(text string) (*AIParseResponse, error)
	AnalyzeTransactions(txs []*models.Transaction) (*AIAnalyzeResponse, error)
}

type aiClient struct {
	serviceURL string
	httpClient *http.Client
}

func NewAIClient(serviceURL string) AIClient {
	return &aiClient{
		serviceURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // Give analysis up to 10 seconds (LLMs take longer)
		},
	}
}

func (c *aiClient) ParseTransaction(text string) (*AIParseResponse, error) {
	apiURL := fmt.Sprintf("%s/parse", c.serviceURL)

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

	var parseResp AIParseResponse
	if err := json.Unmarshal(body, &parseResp); err != nil {
		return nil, fmt.Errorf("received malformed response from AI Service: %w", err)
	}

	if parseResp.Type == "" || parseResp.Category == "" {
		return nil, fmt.Errorf("received incomplete response fields from AI Service")
	}

	return &parseResp, nil
}

func (c *aiClient) AnalyzeTransactions(txs []*models.Transaction) (*AIAnalyzeResponse, error) {
	apiURL := fmt.Sprintf("%s/analyze", c.serviceURL)

	// Map models.Transaction to AITransactionItem
	items := make([]AITransactionItem, len(txs))
	for i, tx := range txs {
		items[i] = AITransactionItem{
			Type:            tx.Type,
			Category:        tx.Category,
			Amount:          tx.Amount,
			Description:     tx.Description,
			TransactionDate: tx.TransactionDate.Format(time.RFC3339),
		}
	}

	reqBody, err := json.Marshal(AIAnalyzeRequest{Transactions: items})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 10-second timeout for analysis request execution
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	var analyzeResp AIAnalyzeResponse
	if err := json.Unmarshal(body, &analyzeResp); err != nil {
		return nil, fmt.Errorf("received malformed response from AI Service: %w", err)
	}

	return &analyzeResp, nil
}
