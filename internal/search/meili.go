package search

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Hit struct {
	Route   string
	Title   string
	Snippet string
}

type Document struct {
	ID          string `json:"id"`
	Route       string `json:"route"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

type Client struct {
	baseURL string
	apiKey  string
	index   string
	http    *http.Client
}

func NewClient(baseURL, apiKey, index string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		index:   strings.TrimSpace(index),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func DocumentID(route string) string {
	sum := sha256.Sum256([]byte(route))
	return hex.EncodeToString(sum[:])[:32]
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	body := map[string]any{
		"q":                    query,
		"limit":                limit,
		"attributesToRetrieve": []string{"route", "title", "description"},
		"attributesToCrop":     []string{"body"},
		"cropLength":           80,
	}
	var response struct {
		Hits []struct {
			Route       string `json:"route"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Formatted   struct {
				Body string `json:"body"`
			} `json:"_formatted"`
		} `json:"hits"`
	}
	if err := c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(c.index)+"/search", body, &response); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(response.Hits))
	for _, item := range response.Hits {
		snippet := strings.TrimSpace(item.Formatted.Body)
		if snippet == "" {
			snippet = strings.TrimSpace(item.Description)
		}
		hits = append(hits, Hit{Route: item.Route, Title: item.Title, Snippet: snippet})
	}
	return hits, nil
}

func (c *Client) Index(ctx context.Context, documents []Document) error {
	if len(documents) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(c.index)+"/documents?primaryKey=id", documents, nil)
}

func (c *Client) Delete(ctx context.Context, routes []string) error {
	if len(routes) == 0 {
		return nil
	}
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, DocumentID(route))
	}
	return c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(c.index)+"/documents/delete-batch", ids, nil)
}

func (c *Client) Clear(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/indexes/"+url.PathEscape(c.index)+"/documents", nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, payload any, out any) error {
	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("meilisearch status %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(out); err != nil {
		return err
	}
	return nil
}

func ValidIndexName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func ValidBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

var ErrNotConfigured = errors.New("search backend is not configured")
