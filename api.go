package faucetpow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const DefaultURL = "https://robinet.eldamar.icu"

type Info struct {
	PayoutBTC     float64 `json:"payout_btc"`
	PoWDifficulty uint32  `json:"pow_difficulty"`
	Network       string  `json:"network"`
	BalanceBTC    float64 `json:"balance_btc"`
}

type Challenge struct {
	Challenge        string `json:"challenge"`
	Difficulty       uint32 `json:"difficulty"`
	ExpiresInSeconds uint32 `json:"expires_in_seconds"`
}

type FaucetResponse struct {
	AmountBTC float64 `json:"amount_btc"`
	TxID      string  `json:"txid"`
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: http.DefaultClient,
	}
}

func (c *Client) Info(ctx context.Context) (Info, error) {
	var out Info
	err := c.getJSON(ctx, "/api/info", &out)
	return out, err
}

func (c *Client) Challenge(ctx context.Context) (Challenge, error) {
	var out Challenge
	err := c.getJSON(ctx, "/api/challenge", &out)
	return out, err
}

func (c *Client) Submit(ctx context.Context, address, challenge, nonce string) (FaucetResponse, error) {
	body := map[string]string{
		"address":   address,
		"challenge": challenge,
		"nonce":     nonce,
	}
	var out FaucetResponse
	err := c.postJSON(ctx, "/api/faucet", body, &out)
	return out, err
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func decodeResponse(resp *http.Response, out any) error {
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var errBody struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errBody); err == nil && errBody.Error != "" {
			return fmt.Errorf("server rejected request: %s", errBody.Error)
		}
		return fmt.Errorf("server rejected request: HTTP %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
