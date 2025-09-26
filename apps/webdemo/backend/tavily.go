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

type tavilyClient struct {
	apiKey     string
	httpClient *http.Client
}

func newTavilyClient(client *http.Client, apiKey string) *tavilyClient {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	apiKey = strings.TrimSpace(apiKey)
	return &tavilyClient{apiKey: apiKey, httpClient: client}
}

func (tc *tavilyClient) enabled() bool {
	return tc != nil && tc.apiKey != ""
}

func (tc *tavilyClient) searchTool() core.ToolHandle {
	if !tc.enabled() {
		return nil
	}

	type searchInput struct {
		Query                    string   `json:"query"`
		Topic                    string   `json:"topic,omitempty"`
		SearchDepth              string   `json:"search_depth,omitempty"`
		AutoParameters           bool     `json:"auto_parameters,omitempty"`
		ChunksPerSource          int      `json:"chunks_per_source,omitempty"`
		MaxResults               int      `json:"max_results,omitempty"`
		TimeRange                string   `json:"time_range,omitempty"`
		Days                     int      `json:"days,omitempty"`
		StartDate                string   `json:"start_date,omitempty"`
		EndDate                  string   `json:"end_date,omitempty"`
		IncludeAnswer            bool     `json:"include_answer,omitempty"`
		IncludeRawContent        bool     `json:"include_raw_content,omitempty"`
		IncludeImages            bool     `json:"include_images,omitempty"`
		IncludeImageDescriptions bool     `json:"include_image_descriptions,omitempty"`
		IncludeFavicon           bool     `json:"include_favicon,omitempty"`
		IncludeDomains           []string `json:"include_domains,omitempty"`
		ExcludeDomains           []string `json:"exclude_domains,omitempty"`
		Country                  string   `json:"country,omitempty"`
	}

	type searchImage struct {
		URL         string `json:"url"`
		Description string `json:"description,omitempty"`
	}

	type searchResult struct {
		Title      string          `json:"title"`
		URL        string          `json:"url"`
		Content    string          `json:"content,omitempty"`
		Score      float64         `json:"score,omitempty"`
		RawContent json.RawMessage `json:"raw_content,omitempty"`
		Favicon    string          `json:"favicon,omitempty"`
	}

	type searchOutput struct {
		Query          string         `json:"query"`
		Answer         string         `json:"answer,omitempty"`
		Results        []searchResult `json:"results"`
		Images         []searchImage  `json:"images,omitempty"`
		AutoParameters map[string]any `json:"auto_parameters,omitempty"`
		ResponseTime   string         `json:"response_time,omitempty"`
		RequestID      string         `json:"request_id,omitempty"`
	}

	tool := tools.New[searchInput, searchOutput](
		"web_search",
		"Search the public web with Tavily; follow up with url_extract via Tavily Extract when you need the full document.",
		func(ctx context.Context, in searchInput, meta core.ToolMeta) (searchOutput, error) {
			if strings.TrimSpace(in.Query) == "" {
				return searchOutput{}, errors.New("query is required")
			}

			body := map[string]any{
				"query": strings.TrimSpace(in.Query),
			}
			if topic := strings.TrimSpace(in.Topic); topic != "" {
				body["topic"] = strings.ToLower(topic)
			}
			if depth := strings.TrimSpace(in.SearchDepth); depth != "" {
				body["search_depth"] = strings.ToLower(depth)
			}
			if in.AutoParameters {
				body["auto_parameters"] = true
			}
			if in.ChunksPerSource > 0 {
				body["chunks_per_source"] = in.ChunksPerSource
			}
			if in.MaxResults > 0 {
				body["max_results"] = in.MaxResults
			}
			if tr := strings.TrimSpace(in.TimeRange); tr != "" {
				body["time_range"] = strings.ToLower(tr)
			}
			if in.Days > 0 {
				body["days"] = in.Days
			}
			if start := strings.TrimSpace(in.StartDate); start != "" {
				body["start_date"] = start
			}
			if end := strings.TrimSpace(in.EndDate); end != "" {
				body["end_date"] = end
			}
			if in.IncludeAnswer {
				body["include_answer"] = true
			}
			if in.IncludeRawContent {
				body["include_raw_content"] = true
			}
			if in.IncludeImages {
				body["include_images"] = true
			}
			if in.IncludeImageDescriptions {
				body["include_image_descriptions"] = true
			}
			if in.IncludeFavicon {
				body["include_favicon"] = true
			}
			if len(in.IncludeDomains) > 0 {
				body["include_domains"] = in.IncludeDomains
			}
			if len(in.ExcludeDomains) > 0 {
				body["exclude_domains"] = in.ExcludeDomains
			}
			if country := strings.TrimSpace(in.Country); country != "" {
				body["country"] = strings.ToLower(country)
			}

			var resp struct {
				Query   string        `json:"query"`
				Answer  string        `json:"answer"`
				Images  []searchImage `json:"images"`
				Results []struct {
					Title      string          `json:"title"`
					URL        string          `json:"url"`
					Content    string          `json:"content"`
					Score      float64         `json:"score"`
					RawContent json.RawMessage `json:"raw_content"`
					Favicon    string          `json:"favicon"`
				} `json:"results"`
				AutoParameters map[string]any  `json:"auto_parameters"`
				ResponseTime   json.RawMessage `json:"response_time"`
				RequestID      string          `json:"request_id"`
			}

			if err := tc.do(ctx, http.MethodPost, "https://api.tavily.com/search", body, &resp); err != nil {
				return searchOutput{}, err
			}

			out := searchOutput{
				Query:          resp.Query,
				Answer:         strings.TrimSpace(resp.Answer),
				AutoParameters: resp.AutoParameters,
				Images:         make([]searchImage, 0, len(resp.Images)),
				Results:        make([]searchResult, 0, len(resp.Results)),
				RequestID:      resp.RequestID,
			}

			if len(resp.ResponseTime) > 0 {
				out.ResponseTime = strings.Trim(string(resp.ResponseTime), "\"")
			}

			if len(resp.Images) > 0 {
				out.Images = append(out.Images, resp.Images...)
			}

			for _, item := range resp.Results {
				out.Results = append(out.Results, searchResult{
					Title:      item.Title,
					URL:        item.URL,
					Content:    item.Content,
					Score:      item.Score,
					RawContent: item.RawContent,
					Favicon:    item.Favicon,
				})
			}

			return out, nil
		},
	)

	return tools.NewCoreAdapter(tool)
}

func (tc *tavilyClient) extractTool() core.ToolHandle {
	if !tc.enabled() {
		return nil
	}

	type extractInput struct {
		URLs           []string `json:"urls"`
		IncludeImages  bool     `json:"include_images,omitempty"`
		IncludeFavicon bool     `json:"include_favicon,omitempty"`
		ExtractDepth   string   `json:"extract_depth,omitempty"`
		Format         string   `json:"format,omitempty"`
		TimeoutSeconds int      `json:"timeout,omitempty"`
	}

	type extractResult struct {
		URL        string          `json:"url"`
		RawContent json.RawMessage `json:"raw_content,omitempty"`
		Images     []struct {
			URL         string `json:"url"`
			Description string `json:"description,omitempty"`
		} `json:"images,omitempty"`
		Favicon string `json:"favicon,omitempty"`
	}

	type extractOutput struct {
		Results      []extractResult  `json:"results"`
		Failed       []map[string]any `json:"failed_results,omitempty"`
		ResponseTime json.RawMessage  `json:"response_time,omitempty"`
		RequestID    string           `json:"request_id,omitempty"`
	}

	tool := tools.New[extractInput, extractOutput](
		"url_extract",
		"Fetch full page content with Tavily Extract; use this after web_search finds promising sources.",
		func(ctx context.Context, in extractInput, meta core.ToolMeta) (extractOutput, error) {
			if len(in.URLs) == 0 {
				return extractOutput{}, errors.New("at least one url is required")
			}

			payload := map[string]any{}
			if len(in.URLs) == 1 {
				payload["urls"] = strings.TrimSpace(in.URLs[0])
			} else {
				urls := make([]string, 0, len(in.URLs))
				for _, u := range in.URLs {
					if trimmed := strings.TrimSpace(u); trimmed != "" {
						urls = append(urls, trimmed)
					}
				}
				if len(urls) == 0 {
					return extractOutput{}, errors.New("at least one url is required")
				}
				payload["urls"] = urls
			}

			if in.IncludeImages {
				payload["include_images"] = true
			}
			if in.IncludeFavicon {
				payload["include_favicon"] = true
			}
			if depth := strings.TrimSpace(in.ExtractDepth); depth != "" {
				payload["extract_depth"] = strings.ToLower(depth)
			}
			if format := strings.TrimSpace(in.Format); format != "" {
				payload["format"] = strings.ToLower(format)
			}
			if in.TimeoutSeconds > 0 {
				payload["timeout"] = in.TimeoutSeconds
			}

			var resp struct {
				Results      []extractResult  `json:"results"`
				Failed       []map[string]any `json:"failed_results"`
				ResponseTime json.RawMessage  `json:"response_time"`
				RequestID    string           `json:"request_id"`
			}

			if err := tc.do(ctx, http.MethodPost, "https://api.tavily.com/extract", payload, &resp); err != nil {
				return extractOutput{}, err
			}

			out := extractOutput{
				Results:      make([]extractResult, 0, len(resp.Results)),
				Failed:       resp.Failed,
				RequestID:    resp.RequestID,
				ResponseTime: resp.ResponseTime,
			}

			out.Results = append(out.Results, resp.Results...)
			return out, nil
		},
	)

	return tools.NewCoreAdapter(tool)
}

func (tc *tavilyClient) do(ctx context.Context, method, rawURL string, payload any, v any) error {
	if !tc.enabled() {
		return errors.New("tavily api key missing")
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
	req.Header.Set("Authorization", "Bearer "+tc.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("tavily %s %s: %s", method, rawURL, strings.TrimSpace(string(data)))
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
