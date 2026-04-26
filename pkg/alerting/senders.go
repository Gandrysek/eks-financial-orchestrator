package alerting

import (
	"context"

	"github.com/go-logr/logr"
)

// SlackSender is a stub NotificationSender for Slack.
type SlackSender struct {
	logger logr.Logger
	config map[string]string
}

// NewSlackSender creates a new SlackSender.
func NewSlackSender(logger logr.Logger, config map[string]string) *SlackSender {
	return &SlackSender{logger: logger, config: config}
}

// Send logs the notification details (stub implementation).
func (s *SlackSender) Send(ctx context.Context, notification *Notification) error {
	s.logger.Info("Slack notification sent (stub)",
		"alertID", notification.AlertID,
		"namespace", notification.Namespace,
		"severity", notification.Severity,
		"message", notification.Message,
	)
	return nil
}

// HealthCheck returns nil (stub implementation).
func (s *SlackSender) HealthCheck(ctx context.Context) error {
	return nil
}

// EmailSender is a stub NotificationSender for email.
type EmailSender struct {
	logger logr.Logger
	config map[string]string
}

// NewEmailSender creates a new EmailSender.
func NewEmailSender(logger logr.Logger, config map[string]string) *EmailSender {
	return &EmailSender{logger: logger, config: config}
}

// Send logs the notification details (stub implementation).
func (s *EmailSender) Send(ctx context.Context, notification *Notification) error {
	s.logger.Info("Email notification sent (stub)",
		"alertID", notification.AlertID,
		"namespace", notification.Namespace,
		"severity", notification.Severity,
		"message", notification.Message,
	)
	return nil
}

// HealthCheck returns nil (stub implementation).
func (s *EmailSender) HealthCheck(ctx context.Context) error {
	return nil
}

// PagerDutySender is a stub NotificationSender for PagerDuty.
type PagerDutySender struct {
	logger logr.Logger
	config map[string]string
}

// NewPagerDutySender creates a new PagerDutySender.
func NewPagerDutySender(logger logr.Logger, config map[string]string) *PagerDutySender {
	return &PagerDutySender{logger: logger, config: config}
}

// Send logs the notification details (stub implementation).
func (s *PagerDutySender) Send(ctx context.Context, notification *Notification) error {
	s.logger.Info("PagerDuty notification sent (stub)",
		"alertID", notification.AlertID,
		"namespace", notification.Namespace,
		"severity", notification.Severity,
		"message", notification.Message,
	)
	return nil
}

// HealthCheck returns nil (stub implementation).
func (s *PagerDutySender) HealthCheck(ctx context.Context) error {
	return nil
}

// SNSSender is a stub NotificationSender for Amazon SNS.
type SNSSender struct {
	logger logr.Logger
	config map[string]string
}

// NewSNSSender creates a new SNSSender.
func NewSNSSender(logger logr.Logger, config map[string]string) *SNSSender {
	return &SNSSender{logger: logger, config: config}
}

// Send logs the notification details (stub implementation).
func (s *SNSSender) Send(ctx context.Context, notification *Notification) error {
	s.logger.Info("SNS notification sent (stub)",
		"alertID", notification.AlertID,
		"namespace", notification.Namespace,
		"severity", notification.Severity,
		"message", notification.Message,
	)
	return nil
}

// HealthCheck returns nil (stub implementation).
func (s *SNSSender) HealthCheck(ctx context.Context) error {
	return nil
}
