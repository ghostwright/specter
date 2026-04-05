package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const baseURL = "https://api.cloudflare.com/client/v4"

// NotFoundError is returned when a resource does not exist.
type NotFoundError struct {
	Resource string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.Resource)
}

// IsNotFound checks if an error is a NotFoundError.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nfe *NotFoundError
	return errors.As(err, &nfe)
}

type Client struct {
	token  string
	zoneID string
	http   *http.Client
}

func NewClient(token, zoneID string) *Client {
	return &Client{
		token:  token,
		zoneID: zoneID,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) ValidateToken(ctx context.Context) error {
	url := fmt.Sprintf("%s/zones/%s", baseURL, c.zoneID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Cloudflare API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Cloudflare API returned %d: %s. Check your token and zone ID", resp.StatusCode, body)
	}
	return nil
}

type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type cfResponse struct {
	Success bool      `json:"success"`
	Result  DNSRecord `json:"result"`
	Errors  []cfError `json:"errors"`
}

type cfListResponse struct {
	Success bool        `json:"success"`
	Result  []DNSRecord `json:"result"`
	Errors  []cfError   `json:"errors"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) CreateDNSRecord(ctx context.Context, name, ip string) (*DNSRecord, error) {
	payload := map[string]interface{}{
		"type":    "A",
		"name":    name,
		"content": ip,
		"ttl":     300,
		"proxied": false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records", baseURL, c.zoneID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not create DNS record: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Cloudflare returns 200 for creation, not 201 (G-15)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Cloudflare returned %d: %s", resp.StatusCode, respBody)
	}

	var result cfResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("DNS record creation failed: %v", result.Errors)
	}

	return &result.Result, nil
}

func (c *Client) DeleteDNSRecord(ctx context.Context, recordID string) error {
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", baseURL, c.zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not delete DNS record: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return &NotFoundError{Resource: "DNS record"}
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Cloudflare returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) FindDNSRecord(ctx context.Context, name string) (*DNSRecord, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?type=A&name=%s", baseURL, c.zoneID, name)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not query DNS records: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result cfListResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not parse response: %w", err)
	}

	if len(result.Result) == 0 {
		return nil, nil
	}
	return &result.Result[0], nil
}
