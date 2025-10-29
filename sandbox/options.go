package sandbox

import (
	"context"
	"io"
	"time"

	"dagger.io/dagger"
)

// ManagerOptions configure sandbox manager construction.
type ManagerOptions struct {
	DefaultRuntime RuntimeSpec
	DefaultLimits  ResourceLimits
	Logger         io.Writer
	ConnectTimeout time.Duration

	DaggerOpts []dagger.ClientOpt

	clientFactory daggerClientFactory
}

// daggerClient captures the subset of dagger.Client we rely upon; this allows test doubles.
type daggerClient interface {
	Container(...dagger.ContainerOpts) *dagger.Container
	Host() *dagger.Host
	Close() error
}

type daggerClientFactory interface {
	Connect(ctx context.Context, opts ...dagger.ClientOpt) (daggerClient, error)
}

type defaultDaggerFactory struct{}

func (defaultDaggerFactory) Connect(ctx context.Context, opts ...dagger.ClientOpt) (daggerClient, error) {
	return dagger.Connect(ctx, opts...)
}
