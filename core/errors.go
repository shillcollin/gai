package core

import (
	"errors"
	"fmt"
)

// ErrorCode categorizes SDK errors.
type ErrorCode string

const (
	ErrRateLimited     ErrorCode = "rate_limited"
	ErrContentFiltered ErrorCode = "content_filtered"
	ErrBadRequest      ErrorCode = "bad_request"
	ErrTransient       ErrorCode = "transient"
	ErrProviderError   ErrorCode = "provider_error"
	ErrTimeout         ErrorCode = "timeout"
	ErrCanceled        ErrorCode = "canceled"
	ErrToolError       ErrorCode = "tool_error"
	ErrInternal        ErrorCode = "internal"
)

// AIError provides rich context for SDK consumers.
type AIError struct {
	Code       ErrorCode
	Message    string
	Status     int
	Retryable  bool
	Details    map[string]any
	RetryAfter int64
	wrapped    error
}

func (e *AIError) Error() string {
	if e == nil {
		return ""
	}
	if e.wrapped != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.wrapped)
	}
	return e.Message
}

func (e *AIError) Unwrap() error { return e.wrapped }

// WrapError creates a new AIError with the provided code.
func WrapError(err error, code ErrorCode) *AIError {
	if err == nil {
		return nil
	}
	var ai *AIError
	if errors.As(err, &ai) {
		return ai
	}
	return &AIError{Code: code, Message: err.Error(), wrapped: err}
}

// NewError builds an AIError explicitly.
func NewError(code ErrorCode, message string, opts ...ErrorOption) *AIError {
	e := &AIError{Code: code, Message: message}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ErrorOption mutates an AIError during construction.
type ErrorOption func(*AIError)

// WithStatus sets the HTTP status code.
func WithStatus(status int) ErrorOption {
	return func(e *AIError) { e.Status = status }
}

// WithRetryable marks whether retry is recommended.
func WithRetryable(retryable bool) ErrorOption {
	return func(e *AIError) { e.Retryable = retryable }
}

// WithRetryAfter sets retry-after seconds.
func WithRetryAfter(seconds int64) ErrorOption {
	return func(e *AIError) { e.RetryAfter = seconds }
}

// WithDetails attaches structured context.
func WithDetails(details map[string]any) ErrorOption {
	return func(e *AIError) { e.Details = details }
}

// WithWrapped attaches an underlying error.
func WithWrapped(err error) ErrorOption {
	return func(e *AIError) { e.wrapped = err }
}

func classify(code ErrorCode) func(error) bool {
	return func(err error) bool {
		var ai *AIError
		if err == nil {
			return false
		}
		if errors.As(err, &ai) {
			return ai.Code == code
		}
		return false
	}
}

// Helper predicates for common error handling patterns.
var (
	IsRateLimited     = classify(ErrRateLimited)
	IsContentFiltered = classify(ErrContentFiltered)
	IsBadRequest      = classify(ErrBadRequest)
	IsTransient       = classify(ErrTransient)
	IsTimeout         = classify(ErrTimeout)
	IsCanceled        = classify(ErrCanceled)
	IsToolError       = classify(ErrToolError)
)

// GetRetryAfter extracts the retry-after hint.
func GetRetryAfter(err error) int64 {
	var ai *AIError
	if errors.As(err, &ai) {
		return ai.RetryAfter
	}
	return 0
}
