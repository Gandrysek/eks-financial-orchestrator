package collector

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/aws/smithy-go"
)

// isRetryableError checks if an error is retryable (throttling, transient network).
// Non-retryable errors (validation, permission, malformed request) are not retried.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for AWS API errors with HTTP status codes.
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		// Only retry throttling (429) and server errors (5xx).
		var httpErr interface{ HTTPStatusCode() int }
		if errors.As(err, &httpErr) {
			code := httpErr.HTTPStatusCode()
			return code == http.StatusTooManyRequests || code >= 500
		}
		// Retry throttling error codes.
		code := apiErr.ErrorCode()
		return code == "Throttling" || code == "ThrottlingException" ||
			code == "RequestLimitExceeded" || code == "TooManyRequestsException"
	}

	// Retry transient network errors.
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout")
}

// retryWithBackoff retries fn with exponential backoff and jitter.
// It retries up to maxRetries times with a base delay of 1s, max delay of 32s,
// and ±25% jitter on each delay. Only retryable errors are retried.
func retryWithBackoff(ctx context.Context, maxRetries int, fn func() error) error {
	const (
		baseDelay = 1 * time.Second
		maxDelay  = 32 * time.Second
		jitter    = 0.25
	)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Don't retry non-retryable errors.
		if !isRetryableError(lastErr) {
			return lastErr
		}

		// Don't sleep after the last attempt.
		if attempt == maxRetries {
			break
		}

		// Calculate exponential delay: baseDelay * 2^attempt.
		delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt)))
		if delay > maxDelay {
			delay = maxDelay
		}

		// Apply ±25% jitter.
		jitterRange := float64(delay) * jitter
		jitterOffset := (rand.Float64()*2 - 1) * jitterRange
		delay = time.Duration(float64(delay) + jitterOffset)

		if delay < 0 {
			delay = 0
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}
