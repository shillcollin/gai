package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tools"
)

type firecrawlClient struct {
	apiKey     string
	httpClient *http.Client
}

func newFirecrawlClient(client *http.Client, apiKey string) *firecrawlClient {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	apiKey = strings.TrimSpace(apiKey)
	return &firecrawlClient{apiKey: apiKey, httpClient: client}
}

func (fc *firecrawlClient) enabled() bool {
	return fc != nil && fc.apiKey != ""
}

func (fc *firecrawlClient) searchTool() core.ToolHandle {
	if !fc.enabled() {
		return nil
	}
	type searchInput struct {
		Query          string   `json:"query"`
		Limit          int      `json:"limit,omitempty"`
		Formats        []string `json:"formats,omitempty"`
		IncludeDomains []string `json:"include_domains,omitempty"`
	}
	type searchResult struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Snippet   string `json:"snippet,omitempty"`
		Markdown  string `json:"markdown,omitempty"`
		Published string `json:"published,omitempty"`
	}
	type searchOutput struct {
		Results []searchResult `json:"results"`
	}

	tool := tools.New[searchInput, searchOutput](
		"web_search",
		"Search the public web using Firecrawl and return high-signal sources.",
		func(ctx context.Context, in searchInput, meta core.ToolMeta) (searchOutput, error) {
			if strings.TrimSpace(in.Query) == "" {
				return searchOutput{}, errors.New("query is required")
			}
			reqBody := map[string]any{
				"query": in.Query,
			}
			if in.Limit > 0 {
				reqBody["limit"] = in.Limit
			}
			if len(in.Formats) > 0 {
				reqBody["scrapeOptions"] = map[string]any{"formats": in.Formats}
			}
			if len(in.IncludeDomains) > 0 {
				reqBody["includeDomains"] = in.IncludeDomains
			}

			var resp struct {
				Success bool `json:"success"`
				Data    []struct {
					Title     string `json:"title"`
					URL       string `json:"url"`
					Snippet   string `json:"snippet"`
					Markdown  string `json:"markdown"`
					Published string `json:"publishedDate"`
				} `json:"data"`
				Error string `json:"error"`
			}

			if err := fc.do(ctx, http.MethodPost, "https://api.firecrawl.dev/v1/search", reqBody, &resp); err != nil {
				return searchOutput{}, err
			}
			if !resp.Success && resp.Error != "" {
				return searchOutput{}, fmt.Errorf("firecrawl search error: %s", resp.Error)
			}
			out := searchOutput{Results: make([]searchResult, 0, len(resp.Data))}
			for _, item := range resp.Data {
				out.Results = append(out.Results, searchResult{
					Title:     item.Title,
					URL:       item.URL,
					Snippet:   item.Snippet,
					Markdown:  item.Markdown,
					Published: item.Published,
				})
			}
			return out, nil
		},
	)
	return tools.NewCoreAdapter(tool)
}

func (fc *firecrawlClient) extractTool() core.ToolHandle {
	if !fc.enabled() {
		return nil
	}
	type extractInput struct {
		URLs            []string       `json:"urls"`
		Prompt          string         `json:"prompt,omitempty"`
		Schema          map[string]any `json:"schema,omitempty"`
		EnableWebSearch bool           `json:"enable_web_search,omitempty"`
		MaxCrawlLinks   int            `json:"max_crawl_links,omitempty"`
	}
	type extractOutput struct {
		Results []map[string]any `json:"results"`
	}

	tool := tools.New[extractInput, extractOutput](
		"url_extract",
		"Extract structured insights from URLs using Firecrawl's extractor.",
		func(ctx context.Context, in extractInput, meta core.ToolMeta) (extractOutput, error) {
			if len(in.URLs) == 0 {
				return extractOutput{}, errors.New("at least one url is required")
			}
			payload := map[string]any{
				"urls": in.URLs,
				"wait": true,
			}
			if strings.TrimSpace(in.Prompt) != "" {
				payload["prompt"] = in.Prompt
			}
			if in.Schema != nil {
				payload["schema"] = in.Schema
			}
			if in.EnableWebSearch {
				payload["enableWebSearch"] = true
			}
			if in.MaxCrawlLinks > 0 {
				payload["maxCrawlLinks"] = in.MaxCrawlLinks
			}

			var resp struct {
				Success     bool             `json:"success"`
				Data        []map[string]any `json:"data"`
				Results     []map[string]any `json:"results"`
				Extractions []map[string]any `json:"extractions"`
				Error       string           `json:"error"`
			}
			if err := fc.do(ctx, http.MethodPost, "https://api.firecrawl.dev/v2/extract", payload, &resp); err != nil {
				return extractOutput{}, err
			}
			if !resp.Success && resp.Error != "" {
				return extractOutput{}, fmt.Errorf("firecrawl extract error: %s", resp.Error)
			}

			results := make([]map[string]any, 0)
			switch {
			case len(resp.Data) > 0:
				results = resp.Data
			case len(resp.Results) > 0:
				results = resp.Results
			case len(resp.Extractions) > 0:
				results = resp.Extractions
			}

			return extractOutput{Results: results}, nil
		},
	)
	return tools.NewCoreAdapter(tool)
}

func (fc *firecrawlClient) do(ctx context.Context, method, rawURL string, payload any, v any) error {
	if !fc.enabled() {
		return errors.New("firecrawl api key missing")
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+fc.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := fc.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("firecrawl %s %s: %s", method, rawURL, strings.TrimSpace(string(data)))
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
