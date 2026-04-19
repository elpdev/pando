package relayapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const authHeader = "X-Pando-Relay-Token"

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(relayURL, token string) (*Client, error) {
	baseURL, err := RelayHTTPBaseURL(relayURL)
	if err != nil {
		return nil, err
	}
	return &Client{baseURL: baseURL, token: token, httpClient: &http.Client{Timeout: 15 * time.Second}}, nil
}

func RelayHTTPBaseURL(relayURL string) (string, error) {
	parsed, err := url.Parse(relayURL)
	if err != nil {
		return "", fmt.Errorf("parse relay URL: %w", err)
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported relay URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/ws")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *Client) PublishDirectoryEntry(entry SignedDirectoryEntry) (*SignedDirectoryEntry, error) {
	var response SignedDirectoryEntry
	if err := c.doJSON(http.MethodPut, "/directory/mailboxes/"+url.PathEscape(entry.Entry.Mailbox), entry, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) LookupDirectoryEntry(mailbox string) (*SignedDirectoryEntry, error) {
	var response SignedDirectoryEntry
	if err := c.doJSON(http.MethodGet, "/directory/mailboxes/"+url.PathEscape(mailbox), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) LookupDirectoryEntryByDeviceMailbox(mailbox string) (*SignedDirectoryEntry, error) {
	var response SignedDirectoryEntry
	if err := c.doJSON(http.MethodGet, "/directory/devices/"+url.PathEscape(mailbox), nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) ListDiscoverableEntries() ([]SignedDirectoryEntry, error) {
	var response ListDirectoryResponse
	if err := c.doJSON(http.MethodGet, "/directory/discoverable", nil, &response); err != nil {
		return nil, err
	}
	return response.Entries, nil
}

func (c *Client) PutRendezvousPayload(id string, payload RendezvousPayload) error {
	return c.doJSON(http.MethodPut, "/rendezvous/"+url.PathEscape(id), PutRendezvousRequest{Payload: payload}, nil)
}

func (c *Client) GetRendezvousPayloads(id string) ([]RendezvousPayload, error) {
	var response GetRendezvousResponse
	if err := c.doJSON(http.MethodGet, "/rendezvous/"+url.PathEscape(id), nil, &response); err != nil {
		return nil, err
	}
	return response.Payloads, nil
}

func (c *Client) DeleteRendezvous(id string) error {
	return c.doJSON(http.MethodDelete, "/rendezvous/"+url.PathEscape(id), nil, nil)
}

func (c *Client) doJSON(method, path string, requestBody any, responseBody any) error {
	var bodyReader *bytes.Reader
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set(authHeader, c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("relay request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Message != "" {
			return fmt.Errorf("relay request failed: %s", payload.Message)
		}
		return fmt.Errorf("relay request failed: %s", resp.Status)
	}
	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil

}
