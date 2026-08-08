package sendly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	Token, From, BaseURL string
	HTTPClient           *http.Client
}

func (c *Client) Send(ctx context.Context, to, body string) (string, error) {
	if c.Token == "" || c.From == "" {
		return "", fmt.Errorf("Sendly is not configured")
	}
	payload, _ := json.Marshal(map[string]string{"from": c.From, "to": to, "body": body})
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.sendly.link"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/sms", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send SMS: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return "", fmt.Errorf("Sendly returned HTTP %d", response.StatusCode)
	}
	var result struct {
		MessageID string `json:"message_id"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil || result.MessageID == "" {
		return "", fmt.Errorf("invalid Sendly response")
	}
	return result.MessageID, nil
}
