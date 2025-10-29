package sandbox

import "errors"

var (
	// ErrUnsupportedBackend indicates the requested backend is not available.
	ErrUnsupportedBackend = errors.New("sandbox: unsupported backend")

	// ErrSessionClosed indicates operations were attempted on a closed session.
	ErrSessionClosed = errors.New("sandbox: session closed")

	// ErrCommandEmpty is returned when Exec is invoked without a command.
	ErrCommandEmpty = errors.New("sandbox: command arguments required")
)
