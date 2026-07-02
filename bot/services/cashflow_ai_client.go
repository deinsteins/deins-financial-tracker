package services

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

// CashflowInsightRequest mirrors the FastAPI AnalyzeCashflowRequest schema.
type CashflowInsightRequest struct {
	AvailableBalance    int64    `json:"available_balance"`
	DailyBurnRate       int64    `json:"daily_burn_rate"`
	ProjectedExpense    int64    `json:"projected_expense"`
	UpcomingObligations int64    `json:"upcoming_obligations"`
	ProjectedBalance    int64    `json:"projected_balance"`
	RiskLevel           string   `json:"risk_level"`
	TargetDate          string   `json:"target_date"`
	TopCategories       []string `json:"top_categories"`
}

// CashflowInsightResponse mirrors the FastAPI CashflowInsightResponse schema.
type CashflowInsightResponse struct {
	Summary         string   `json:"summary"`
	Recommendations []string `json:"recommendations"`
}

// CashflowAIClient calls the Python AI service to generate cashflow insight.
type CashflowAIClient interface {
	AnalyzeCashflow(req CashflowInsightRequest) (*CashflowInsightResponse, error)
}

type cashflowAIClient struct {
	serviceURL string
	httpClient *http.Client
}

// NewCashflowAIClient creates a CashflowAIClient with a 25-second timeout
// (Gemini can be slow; this keeps us within Telegram's implicit response window
// while still being able to fall back gracefully).
func NewCashflowAIClient(serviceURL string) CashflowAIClient {
	return &cashflowAIClient{
		serviceURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *cashflowAIClient) AnalyzeCashflow(req CashflowInsightRequest) (*CashflowInsightResponse, error) {
	if c.serviceURL == "" {
		return nil, fmt.Errorf("AI service URL not configured")
	}

	apiURL := fmt.Sprintf("%s/analyze-cashflow", c.serviceURL)

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Separate context timeout — tighter than httpClient.Timeout so we can log
	// an informative error rather than a raw context.DeadlineExceeded.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("AI service connection error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read AI service response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.Unmarshal(body, &errData)
		if detail, ok := errData["detail"]; ok {
			return nil, fmt.Errorf("AI service error: %v", detail)
		}
		return nil, fmt.Errorf("AI service failed with status %d", resp.StatusCode)
	}

	var insight CashflowInsightResponse
	if err := json.Unmarshal(body, &insight); err != nil {
		return nil, fmt.Errorf("malformed AI service response: %w", err)
	}

	if insight.Summary == "" {
		return nil, fmt.Errorf("AI service returned empty summary")
	}

	log.Printf("[CashflowAIClient] AI insight received: summary len=%d, recs=%d", len(insight.Summary), len(insight.Recommendations))
	return &insight, nil
}
