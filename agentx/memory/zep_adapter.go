//go:build zep

package memory

import (
	"context"
	"fmt"

	zepclient "github.com/getzep/zep-go/v3/client"
	"github.com/getzep/zep-go/v3/option"
	"github.com/getzep/zep-go/v3/thread"
)

// ZepConfig configures Zep-backed memory retrieval.
type ZepConfig struct {
	APIKey    string
	BaseURL   string
	ThreadID  string
	Mode      string
	MinRating *float64
}

// NewZepProvider constructs a Provider backed by Zep's thread context API.
func NewZepProvider(cfg ZepConfig) (Provider, error) {
	if cfg.ThreadID == "" {
		return nil, fmt.Errorf("memory: zep thread id is required")
	}
	opts := []option.ClientOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := zepclient.NewClient(opts...)
	return &zepProvider{client: client.Thread, config: cfg}, nil
}

type zepProvider struct {
	client *thread.Client
	config ZepConfig
}

func (p *zepProvider) Query(ctx context.Context, scope Scope, q Query) ([]Entry, error) {
	opts := &thread.GetUserContextRequest{}
	if p.config.Mode != "" {
		opts.Mode = thread.ContextMode(p.config.Mode)
	}
	if p.config.MinRating != nil {
		opts.MinRating = p.config.MinRating
	}
	contextStr, err := p.client.GetUserContext(ctx, p.config.ThreadID, opts)
	if err != nil {
		return nil, err
	}
	if contextStr == "" {
		return nil, nil
	}
	return []Entry{{Title: "Zep Context", Content: contextStr}}, nil
}
