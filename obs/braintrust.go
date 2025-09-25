package obs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	braintrust "github.com/braintrustdata/braintrust-go"
	"github.com/braintrustdata/braintrust-go/option"
	"github.com/braintrustdata/braintrust-go/packages/param"
	"github.com/braintrustdata/braintrust-go/shared"
)

const defaultBraintrustBaseURL = "https://api.braintrust.dev"

type braintrustSink struct {
	cfg           BraintrustOptions
	queue         chan Completion
	projectLogSvc *braintrust.ProjectLogService
	datasetSvc    *braintrust.DatasetService
	projectID     string
	datasetID     string
	wg            sync.WaitGroup
	cancel        context.CancelFunc
}

func newBraintrustSink(ctx context.Context, cfg BraintrustOptions) (*braintrustSink, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("braintrust api key required")
	}
	if cfg.Project == "" && cfg.ProjectID == "" {
		return nil, errors.New("braintrust project name or id required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 3 * time.Second
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBraintrustBaseURL
	}

	clientOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(&http.Client{Timeout: cfg.HTTPTimeout}),
	}

	projectSvc := braintrust.NewProjectService(clientOpts...)
	datasetSvc := braintrust.NewDatasetService(clientOpts...)
	projectLogSvc := braintrust.NewProjectLogService(clientOpts...)

	projectID, err := resolveProjectID(ctx, &projectSvc, cfg)
	if err != nil {
		return nil, err
	}

	datasetName := strings.TrimSpace(cfg.Dataset)
	if datasetName == "<auto>" {
		datasetName = ""
	}

	var datasetID string
	if datasetName != "" {
		datasetID, err = ensureDataset(ctx, &datasetSvc, projectID, datasetName)
		if err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	sink := &braintrustSink{
		cfg:           cfg,
		queue:         make(chan Completion, cfg.BatchSize*4),
		projectLogSvc: &projectLogSvc,
		datasetSvc:    &datasetSvc,
		projectID:     projectID,
		datasetID:     datasetID,
		cancel:        cancel,
	}

	sink.wg.Add(1)
	go sink.run(ctx)
	return sink, nil
}

func (b *braintrustSink) LogCompletion(ctx context.Context, c Completion) error {
	select {
	case b.queue <- c:
		return nil
	default:
		return errors.New("braintrust queue full")
	}
}

func (b *braintrustSink) Shutdown(ctx context.Context) error {
	b.cancel()
	close(b.queue)
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *braintrustSink) run(ctx context.Context) {
	defer b.wg.Done()
	ticker := time.NewTicker(b.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]Completion, 0, b.cfg.BatchSize)
	flush := func(force bool) {
		if len(batch) == 0 {
			return
		}
		if err := b.flushBatch(ctx, batch); err != nil {
			log.Printf("braintrust flush error: %v", err)
		}
		batch = batch[:0]
		if force {
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush(true)
			return
		case <-ticker.C:
			flush(false)
		case item, ok := <-b.queue:
			if !ok {
				flush(true)
				return
			}
			batch = append(batch, item)
			if len(batch) >= b.cfg.BatchSize {
				flush(false)
			}
		}
	}
}

func (b *braintrustSink) flushBatch(ctx context.Context, batch []Completion) error {
	if b.datasetID != "" && b.datasetSvc != nil {
		events := make([]shared.InsertDatasetEventParam, 0, len(batch))
		for _, item := range batch {
			events = append(events, completionToDatasetEvent(item))
		}
		_, err := b.datasetSvc.Insert(ctx, b.datasetID, braintrust.DatasetInsertParams{
			Events: events,
		})
		return err
	}

	if b.projectLogSvc == nil {
		return errors.New("braintrust project log service not configured")
	}
	events := make([]shared.InsertProjectLogsEventParam, 0, len(batch))
	for _, item := range batch {
		events = append(events, completionToProjectLogEvent(item))
	}
	_, err := b.projectLogSvc.Insert(ctx, b.projectID, braintrust.ProjectLogInsertParams{
		Events: events,
	})
	return err
}

func resolveProjectID(ctx context.Context, svc *braintrust.ProjectService, cfg BraintrustOptions) (string, error) {
	if cfg.ProjectID != "" {
		return cfg.ProjectID, nil
	}
	if cfg.Project == "" {
		return "", errors.New("braintrust project name required when project id missing")
	}

	resp, err := svc.List(ctx, braintrust.ProjectListParams{
		Limit:       param.NewOpt[int64](1),
		ProjectName: param.NewOpt(cfg.Project),
	})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Objects) == 0 {
		return "", fmt.Errorf("braintrust project %q not found", cfg.Project)
	}
	return resp.Objects[0].ID, nil
}

func ensureDataset(ctx context.Context, svc *braintrust.DatasetService, projectID, datasetName string) (string, error) {
	dataset, err := svc.New(ctx, braintrust.DatasetNewParams{
		Name:      datasetName,
		ProjectID: projectID,
	})
	if err != nil {
		return "", err
	}
	return dataset.ID, nil
}

func completionToProjectLogEvent(c Completion) shared.InsertProjectLogsEventParam {
	event := shared.InsertProjectLogsEventParam{}
	if c.RequestID != "" {
		event.ID = param.NewOpt(c.RequestID)
	}
	if c.CreatedAtUTC != 0 {
		event.Created = param.NewOpt(time.UnixMilli(c.CreatedAtUTC).UTC())
	}

	event.Input = messagesToAny(c.Input)
	event.Output = messageToAny(c.Output)
	if c.Error != "" {
		event.Error = c.Error
	}

	event.Metadata = buildProjectMetadata(c)
	event.Metrics = buildProjectMetrics(c)
	if len(c.ToolCalls) > 0 {
		if event.Metadata.ExtraFields == nil {
			event.Metadata.ExtraFields = make(map[string]any)
		}
		event.Metadata.ExtraFields["tool_calls"] = ToolCallsToAny(c.ToolCalls)
	}

	return event
}

func completionToDatasetEvent(c Completion) shared.InsertDatasetEventParam {
	event := shared.InsertDatasetEventParam{}
	if c.RequestID != "" {
		event.ID = param.NewOpt(c.RequestID)
	}
	if c.CreatedAtUTC != 0 {
		event.Created = param.NewOpt(time.UnixMilli(c.CreatedAtUTC).UTC())
	}

	event.Input = map[string]any{
		"messages": messagesToAny(c.Input),
	}
	if c.Provider != "" {
		event.Input.(map[string]any)["provider"] = c.Provider
	}
	if c.Model != "" {
		event.Input.(map[string]any)["model"] = c.Model
	}

	event.Expected = messageToAny(c.Output)

	event.Metadata = shared.InsertDatasetEventMetadataParam{
		ExtraFields: copyMetadata(c.Metadata),
	}
	if c.Model != "" {
		event.Metadata.Model = param.NewOpt(c.Model)
	}
	if c.Error != "" {
		if event.Metadata.ExtraFields == nil {
			event.Metadata.ExtraFields = make(map[string]any)
		}
		event.Metadata.ExtraFields["error"] = c.Error
	}
	if len(c.ToolCalls) > 0 {
		if event.Metadata.ExtraFields == nil {
			event.Metadata.ExtraFields = make(map[string]any)
		}
		event.Metadata.ExtraFields["tool_calls"] = ToolCallsToAny(c.ToolCalls)
	}

	return event
}

func buildProjectMetadata(c Completion) shared.InsertProjectLogsEventMetadataParam {
	extras := copyMetadata(c.Metadata)
	if extras == nil {
		extras = make(map[string]any)
	}
	if c.Provider != "" {
		extras["provider"] = c.Provider
	}
	if c.RequestID != "" {
		extras["request_id"] = c.RequestID
	}
	if len(c.ToolCalls) > 0 {
		extras["tool_calls"] = ToolCallsToAny(c.ToolCalls)
	}

	metadata := shared.InsertProjectLogsEventMetadataParam{
		ExtraFields: extras,
	}
	if c.Model != "" {
		metadata.Model = param.NewOpt(c.Model)
	}
	return metadata
}

func buildProjectMetrics(c Completion) shared.InsertProjectLogsEventMetricsParam {
	metrics := shared.InsertProjectLogsEventMetricsParam{}
	if c.Usage.InputTokens > 0 {
		metrics.PromptTokens = param.NewOpt(int64(c.Usage.InputTokens))
	}
	if c.Usage.OutputTokens > 0 {
		metrics.CompletionTokens = param.NewOpt(int64(c.Usage.OutputTokens))
	}
	if c.Usage.TotalTokens > 0 {
		metrics.Tokens = param.NewOpt(int64(c.Usage.TotalTokens))
	}

	if c.LatencyMS > 0 || c.Usage.CostUSD > 0 || c.Usage.ReasoningTokens > 0 || c.Usage.AudioTokens > 0 || c.Usage.CachedTokens > 0 {
		metrics.ExtraFields = make(map[string]float64)
	}
	if c.LatencyMS > 0 {
		metrics.ExtraFields["latency_ms"] = float64(c.LatencyMS)
	}
	if c.Usage.CostUSD > 0 {
		metrics.ExtraFields["cost_usd"] = c.Usage.CostUSD
	}
	if c.Usage.ReasoningTokens > 0 {
		metrics.ExtraFields["reasoning_tokens"] = float64(c.Usage.ReasoningTokens)
	}
	if c.Usage.AudioTokens > 0 {
		metrics.ExtraFields["audio_tokens"] = float64(c.Usage.AudioTokens)
	}
	if c.Usage.CachedTokens > 0 {
		metrics.ExtraFields["cached_tokens"] = float64(c.Usage.CachedTokens)
	}

	return metrics
}

func messagesToAny(messages []Message) any {
	if len(messages) == 0 {
		return []any{}
	}
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		entry := map[string]any{
			"role": msg.Role,
		}
		if msg.Text != "" {
			entry["text"] = msg.Text
		}
		if msg.Data != nil && len(msg.Data) > 0 {
			entry["data"] = msg.Data
		}
		out = append(out, entry)
	}
	return out
}

func messageToAny(msg Message) any {
	if msg.Role == "" && msg.Text == "" && len(msg.Data) == 0 {
		return nil
	}
	entry := map[string]any{
		"role": msg.Role,
	}
	if msg.Text != "" {
		entry["text"] = msg.Text
	}
	if msg.Data != nil && len(msg.Data) > 0 {
		entry["data"] = msg.Data
	}
	return entry
}

func copyMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
