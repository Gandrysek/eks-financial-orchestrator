package alerting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
)

// DefaultAlertManager implements the AlertManager interface.
type DefaultAlertManager struct {
	channels       []NotificationSender
	silenceWindows []v1alpha1.SilenceWindowSpec
	logger         logr.Logger
}

// NewDefaultAlertManager creates a new DefaultAlertManager.
func NewDefaultAlertManager(logger logr.Logger) *DefaultAlertManager {
	return &DefaultAlertManager{
		channels:       make([]NotificationSender, 0),
		silenceWindows: make([]v1alpha1.SilenceWindowSpec, 0),
		logger:         logger,
	}
}

// CheckAnomalies detects cost anomalies (>20% deviation from forecast).
// Returns an AnomalyDetection indicating whether an anomaly was found.
func (m *DefaultAlertManager) CheckAnomalies(ctx context.Context, actual float64, forecast float64) *AnomalyDetection {
	var deviationPct float64
	if forecast != 0 {
		deviationPct = ((actual - forecast) / forecast) * 100
	} else if actual > 0 {
		// If forecast is zero but actual is positive, deviation is infinite — treat as anomaly.
		deviationPct = 100
	}

	isAnomaly := actual > forecast*1.2

	m.logger.V(1).Info("Checked anomalies",
		"actual", actual,
		"forecast", forecast,
		"deviationPct", deviationPct,
		"isAnomaly", isAnomaly,
	)

	return &AnomalyDetection{
		IsAnomaly:    isAnomaly,
		ActualCost:   actual,
		ForecastCost: forecast,
		DeviationPct: deviationPct,
	}
}

// SendAlert dispatches a notification through all configured channels.
// Alerts are skipped if the current time falls within a silence window for the namespace.
// Failures on individual channels do not block other channels.
func (m *DefaultAlertManager) SendAlert(ctx context.Context, alert *Alert) error {
	// Check silence window.
	if m.IsInSilenceWindow(ctx, alert.Namespace) {
		m.logger.Info("Alert suppressed by silence window",
			"alertID", alert.ID,
			"namespace", alert.Namespace,
		)
		return nil
	}

	// Validate required fields.
	if err := validateAlert(alert); err != nil {
		return fmt.Errorf("invalid alert: %w", err)
	}

	// Build notification from alert.
	notification := &Notification{
		AlertID:           alert.ID,
		Severity:          alert.Severity,
		Category:          alert.Category,
		Namespace:         alert.Namespace,
		CurrentCost:       alert.CurrentCost,
		BudgetLimit:       alert.BudgetLimit,
		UsagePercent:      alert.UsagePercent,
		Message:           alert.Message,
		RecommendedAction: alert.RecommendedAction,
		Timestamp:         alert.Timestamp,
	}

	// Send to all configured channels independently.
	var errs []string
	for i, sender := range m.channels {
		m.logger.Info("Sending alert to channel",
			"alertID", alert.ID,
			"channelIndex", i,
		)
		if err := sendWithRetry(ctx, sender, notification, 3, m.logger); err != nil {
			errs = append(errs, fmt.Sprintf("channel %d: %v", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to send alert to some channels: %s", strings.Join(errs, "; "))
	}
	return nil
}

// IsInSilenceWindow checks if the current time falls within any configured
// silence window for the given namespace.
func (m *DefaultAlertManager) IsInSilenceWindow(ctx context.Context, namespace string) bool {
	now := time.Now()
	for _, sw := range m.silenceWindows {
		if !now.Before(sw.Start.Time) && now.Before(sw.End.Time) {
			return true
		}
	}
	return false
}

// ConfigureChannel sets up a notification channel and adds it to the manager.
// Supported types: slack, email, pagerduty, sns.
func (m *DefaultAlertManager) ConfigureChannel(ctx context.Context, channel NotificationChannel) error {
	var sender NotificationSender

	switch channel.Type {
	case "slack":
		sender = NewSlackSender(m.logger, channel.Config)
	case "email":
		sender = NewEmailSender(m.logger, channel.Config)
	case "pagerduty":
		sender = NewPagerDutySender(m.logger, channel.Config)
	case "sns":
		sender = NewSNSSender(m.logger, channel.Config)
	default:
		return fmt.Errorf("unsupported channel type: %s", channel.Type)
	}

	m.channels = append(m.channels, sender)
	m.logger.Info("Configured notification channel", "type", channel.Type)
	return nil
}

// SetSilenceWindows configures the silence windows for the manager.
func (m *DefaultAlertManager) SetSilenceWindows(windows []v1alpha1.SilenceWindowSpec) {
	m.silenceWindows = windows
}

// validateAlert checks that an alert has all required fields.
func validateAlert(alert *Alert) error {
	var missing []string
	if alert.Namespace == "" {
		missing = append(missing, "Namespace")
	}
	if alert.RecommendedAction == "" {
		missing = append(missing, "RecommendedAction")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}
