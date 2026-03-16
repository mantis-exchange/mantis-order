package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type RiskClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRiskClient(baseURL string) *RiskClient {
	return &RiskClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 3 * time.Second},
	}
}

type OrderCheckRequest struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"`
	LastPrice float64 `json:"last_price"`
}

type CheckResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func (c *RiskClient) CheckOrder(ctx context.Context, req OrderCheckRequest) (*CheckResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal risk check request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/risk/check-order", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// If risk service is down, allow the order (fail-open)
		return &CheckResult{Allowed: true}, nil
	}
	defer resp.Body.Close()

	var result CheckResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &CheckResult{Allowed: true}, nil
	}
	return &result, nil
}
