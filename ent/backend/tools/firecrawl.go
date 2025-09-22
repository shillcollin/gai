package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	gaiTools "github.com/shillcollin/gai/tools"
)

const (
	defaultFirecrawlBase = "https://api.firecrawl.dev/v2"
	headerAuthBearer     = "Bearer %s"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// WebSearchInput mirrors the Firecrawl search request payload.
type WebSearchInput struct {
	Query      string   `json:"query"`
	Limit      int      `json:"limit,omitempty"`
	Scrape     bool     `json:"scrape,omitempty"`
	Formats    []string `json:"formats,omitempty"`
	Sources    []string `json:"sources,omitempty"`
	Categories []string `json:"categories,omitempty"`
	TBS        string   `json:"tbs,omitempty"`
}

// WebSearchOutput captures the Firecrawl response data.
type WebSearchOutput map[string]any

// URLExtactInput represents the Firecrawl scrape request body.
type URLExtractInput struct {
	URL     string           `json:"url"`
	Formats []string         `json:"formats,omitempty"`
	Proxy   string           `json:"proxy,omitempty"`
	Actions []map[string]any `json:"actions,omitempty"`
	Timeout int              `json:"timeout,omitempty"`
}

// URLExtractOutput mirrors Firecrawl scrape responses.
type URLExtractOutput map[string]any

// NewWebSearchTool constructs the Firecrawl web_search tool handle.
func NewWebSearchTool() core.ToolHandle {
	tool := gaiTools.New[WebSearchInput, WebSearchOutput](
		"web_search",
		"Search the web via Firecrawl and optionally scrape top results.",
		func(ctx context.Context, in WebSearchInput, meta core.ToolMeta) (WebSearchOutput, error) {
			apiKey := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY"))
			if apiKey == "" {
				return nil, fmt.Errorf("FIRECRAWL_API_KEY not set")
			}

			base := strings.TrimSpace(os.Getenv("FIRECRAWL_BASE"))
			if base == "" {
				base = defaultFirecrawlBase
			}

			payload := map[string]any{
				"query": in.Query,
				"limit": valueOrDefault(in.Limit, 3),
			}
			if len(in.Sources) > 0 {
				payload["sources"] = in.Sources
			}
			if len(in.Categories) > 0 {
				payload["categories"] = in.Categories
			}
			if in.TBS != "" {
				payload["tbs"] = in.TBS
			}
			if in.Scrape {
				scrape := map[string]any{}
				if len(in.Formats) > 0 {
					scrape["formats"] = in.Formats
				} else {
					scrape["formats"] = []string{"markdown"}
				}
				payload["scrapeOptions"] = scrape
			}

			raw, err := firecrawlPost(ctx, base+"/search", apiKey, payload)
			if err != nil {
				return nil, err
			}
			return WebSearchOutput(normalizeDataField(raw)), nil
		},
	)

	return gaiTools.NewCoreAdapter(tool)
}

// NewURLExtractTool constructs the Firecrawl url_extract tool handle.
func NewURLExtractTool() core.ToolHandle {
	tool := gaiTools.New[URLExtractInput, URLExtractOutput](
		"url_extract",
		"Scrape a single URL with Firecrawl and return markdown/html/screenshots metadata.",
		func(ctx context.Context, in URLExtractInput, meta core.ToolMeta) (URLExtractOutput, error) {
			apiKey := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY"))
			if apiKey == "" {
				return nil, fmt.Errorf("FIRECRAWL_API_KEY not set")
			}

			base := strings.TrimSpace(os.Getenv("FIRECRAWL_BASE"))
			if base == "" {
				base = defaultFirecrawlBase
			}

			payload := map[string]any{
				"url":     in.URL,
				"formats": fallbackFormats(in.Formats),
			}
			if in.Proxy != "" {
				payload["proxy"] = in.Proxy
			}
			if len(in.Actions) > 0 {
				payload["actions"] = in.Actions
			}
			if in.Timeout > 0 {
				payload["timeout"] = in.Timeout
			}

			raw, err := firecrawlPost(ctx, base+"/scrape", apiKey, payload)
			if err != nil {
				return nil, err
			}
			return URLExtractOutput(normalizeDataField(raw)), nil
		},
	)

	return gaiTools.NewCoreAdapter(tool)
}

func firecrawlPost(ctx context.Context, url, apiKey string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal firecrawl payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build firecrawl request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf(headerAuthBearer, apiKey))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("firecrawl request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("firecrawl error %s: %s", resp.Status, data)
	}

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode firecrawl response: %w", err)
	}
	return decoded, nil
}

func normalizeDataField(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	if data, ok := raw["data"].(map[string]any); ok {
		return data
	}
	return raw
}

func valueOrDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func fallbackFormats(formats []string) []string {
	if len(formats) == 0 {
		return []string{"markdown"}
	}
	return formats
}
