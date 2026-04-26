package alerting

import (
	"context"
	"time"
)

// AlertSeverity represents the severity level of an alert.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertCategory represents the category of an alert.
type AlertCategory string

const (
	AlertCategoryBudget          AlertCategory = "budget"
	AlertCategoryAnomaly         AlertCategory = "anomaly"
	AlertCategorySpotInterruption AlertCategory = "spot_interruption"
	AlertCategoryPolicy          AlertCategory = "policy"
)

// Alert represents a notification to be sent.
type Alert struct {
	ID                string        `json:"id"`
	Timestamp         time.Time     `json:"timestamp"`
	Severity          AlertSeverity `json:"severity"`
	Category          AlertCategory `json:"category"`
	Namespace         string        `json:"namespace"`
	CurrentCost       float64       `json:"current_cost"`
	BudgetLimit       float64       `json:"budget_limit"`
	UsagePercent      float64       `json:"usage_percent"`
	Message           string        `json:"message"`
	RecommendedAction string        `json:"recommended_action"`
}

// AnomalyDetection holds the result of an anomaly check.
type AnomalyDetection struct {
	IsAnomaly    bool    `json:"is_anomaly"`
	ActualCost   float64 `json:"actual_cost"`
	ForecastCost float64 `json:"forecast_cost"`
	DeviationPct float64 `json:"deviation_pct"`
}

// Notification represents a message to be delivered through a channel.
type Notification struct {
	AlertID           string        `json:"alert_id"`
	Channel           string        `json:"channel"`
	Severity          AlertSeverity `json:"severity"`
	Category          AlertCategory `json:"category"`
	Namespace         string        `json:"namespace"`
	CurrentCost       float64       `json:"current_cost"`
	BudgetLimit       float64       `json:"budget_limit"`
	UsagePercent      float64       `json:"usage_percent"`
	Message           string        `json:"message"`
	RecommendedAction string        `json:"recommended_action"`
	Timestamp         time.Time     `json:"timestamp"`
}

// NotificationChannel defines a configured notification channel.
type NotificationChannel struct {
	Type   string            `json:"type"` // slack, email, pagerduty, sns
	Config map[string]string `json:"config,omitempty"`
}

// AlertManager defines the interface for the alerting system.
type AlertManager interface {
	// SendAlert dispatches a notification through configured channels.
	SendAlert(ctx context.Context, alert *Alert) error

	// CheckAnomalies detects cost anomalies (>20% deviation from forecast).
	CheckAnomalies(ctx context.Context, actual float64, forecast float64) *AnomalyDetection

	// IsInSilenceWindow checks if the current time falls within a silence window.
	IsInSilenceWindow(ctx context.Context, namespace string) bool

	// ConfigureChannel sets up a notification channel (Slack, email, PagerDuty, SNS).
	ConfigureChannel(ctx context.Context, channel NotificationChannel) error
}

// NotificationSender is implemented by each notification channel.
type NotificationSender interface {
	// Send delivers a notification through the specific channel.
	Send(ctx context.Context, notification *Notification) error

	// HealthCheck verifies the channel is reachable.
	HealthCheck(ctx context.Context) error
}
