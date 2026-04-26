package alerting

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// sendWithRetry attempts to send a notification through a sender, retrying on
// failure with exponential backoff (1s, 2s, 4s). It logs each retry attempt
// and, on exhaustion, logs the error with the full alert payload.
func sendWithRetry(ctx context.Context, sender NotificationSender, notification *Notification, maxRetries int, logger logr.Logger) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			delay := time.Duration(1<<uint(attempt-1)) * time.Second
			logger.Info("Retrying notification send",
				"alertID", notification.AlertID,
				"attempt", attempt,
				"delay", delay.String(),
			)

			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		lastErr = sender.Send(ctx, notification)
		if lastErr == nil {
			return nil
		}

		logger.Info("Notification send failed",
			"alertID", notification.AlertID,
			"attempt", attempt,
			"error", lastErr.Error(),
		)
	}

	// All retries exhausted — log error with full payload.
	logger.Error(lastErr, "All retries exhausted for notification",
		"alertID", notification.AlertID,
		"severity", notification.Severity,
		"category", notification.Category,
		"namespace", notification.Namespace,
		"currentCost", notification.CurrentCost,
		"budgetLimit", notification.BudgetLimit,
		"usagePercent", notification.UsagePercent,
		"message", notification.Message,
		"recommendedAction", notification.RecommendedAction,
	)

	return fmt.Errorf("all %d retries exhausted: %w", maxRetries, lastErr)
}
