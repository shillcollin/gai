package sandbox

import (
	"io"
	"time"
)

// ManagerOptions configure sandbox manager construction.
type ManagerOptions struct {
	DefaultRuntime RuntimeSpec
	DefaultLimits  ResourceLimits
	Logger         io.Writer
	ConnectTimeout time.Duration
}
