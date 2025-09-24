package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultBraintrustBaseURL = "https://api.braintrust.dev"

type braintrustSink struct {
	cfg    BraintrustOptions
	queue  chan Completion
	client *http.Client
	wg     sync.WaitGroup
	cancel context.CancelFunc
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
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBraintrustBaseURL
	}

	ctx, cancel := context.WithCancel(ctx)
	sink := &braintrustSink{
		cfg:    cfg,
		queue:  make(chan Completion, cfg.BatchSize*4),
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		cancel: cancel,
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
		// Drop if queue is full to avoid blocking hot path.
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
	payload := make([]braintrustRecord, 0, len(batch))
	for _, item := range batch {
		payload = append(payload, toBraintrustRecord(item, b.cfg))
	}
	body, err := json.Marshal(braintrustEnvelope{
		Project:   b.cfg.Project,
		ProjectID: b.cfg.ProjectID,
		Dataset:   b.cfg.Dataset,
		Records:   payload,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/log", b.cfg.BaseURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("braintrust status %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

type braintrustEnvelope struct {
	Project   string             `json:"project,omitempty"`
	ProjectID string             `json:"project_id,omitempty"`
	Dataset   string             `json:"dataset,omitempty"`
	Records   []braintrustRecord `json:"records"`
}

type braintrustRecord struct {
	RequestID string         `json:"request_id,omitempty"`
	Provider  string         `json:"provider,omitempty"`
	Model     string         `json:"model,omitempty"`
	Input     []Message      `json:"input,omitempty"`
	Output    Message        `json:"output"`
	Usage     UsageTokens    `json:"usage"`
	LatencyMS int64          `json:"latency_ms,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp int64          `json:"timestamp_ms"`
}

func toBraintrustRecord(c Completion, cfg BraintrustOptions) braintrustRecord {
	return braintrustRecord{
		RequestID: c.RequestID,
		Provider:  c.Provider,
		Model:     c.Model,
		Input:     c.Input,
		Output:    c.Output,
		Usage:     c.Usage,
		LatencyMS: c.LatencyMS,
		Metadata:  c.Metadata,
		Error:     c.Error,
		Timestamp: c.CreatedAtUTC,
	}
}
