package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AccountClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewAccountClient(baseURL string) *AccountClient {
	return &AccountClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type balanceReq struct {
	UserID string `json:"user_id"`
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

func (c *AccountClient) FreezeBalance(ctx context.Context, userID, asset, amount string) error {
	return c.doBalanceOp(ctx, "/internal/v1/balance/freeze", userID, asset, amount)
}

func (c *AccountClient) UnfreezeBalance(ctx context.Context, userID, asset, amount string) error {
	return c.doBalanceOp(ctx, "/internal/v1/balance/unfreeze", userID, asset, amount)
}

func (c *AccountClient) CreditBalance(ctx context.Context, userID, asset, amount string) error {
	return c.doBalanceOp(ctx, "/internal/v1/balance/credit", userID, asset, amount)
}

func (c *AccountClient) DeductFrozenBalance(ctx context.Context, userID, asset, amount string) error {
	return c.doBalanceOp(ctx, "/internal/v1/balance/deduct-frozen", userID, asset, amount)
}

func (c *AccountClient) doBalanceOp(ctx context.Context, path, userID, asset, amount string) error {
	body, _ := json.Marshal(balanceReq{
		UserID: userID,
		Asset:  asset,
		Amount: amount,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("account client: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("account client: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("account service error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
