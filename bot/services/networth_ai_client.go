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

type NetWorthParseResponse struct {
	Intent string  `json:"intent"`
	Type   *string `json:"type"`
	Name   *string `json:"name"`
	Amount *int64  `json:"amount"`
	Notes  *string `json:"notes"`
	Reason *string `json:"reason"`
}

type NetWorthAIClient interface {
	ParseNetWorth(text string) (*NetWorthParseResponse, error)
}

type netWorthAIClient struct {
	serviceURL string
	httpClient *http.Client
}

func NewNetWorthAIClient(serviceURL string) NetWorthAIClient {
	return &netWorthAIClient{
		serviceURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *netWorthAIClient) ParseNetWorth(text string) (*NetWorthParseResponse, error) {
	apiURL := fmt.Sprintf("%s/parse-networth", c.serviceURL)

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

	var parseResp NetWorthParseResponse
	if err := json.Unmarshal(body, &parseResp); err != nil {
		return nil, fmt.Errorf("received malformed response from AI Service: %w", err)
	}

	if parseResp.Intent == "" {
		return nil, fmt.Errorf("received incomplete response fields from AI Service")
	}

	return &parseResp, nil
}
