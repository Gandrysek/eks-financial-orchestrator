package alerting

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// mockSender is a test double for NotificationSender.
type mockSender struct {
	sendCalls    int
	sendErr      error
	failNTimes   int // fail the first N calls, then succeed
	notifications []*Notification
}

func (m *mockSender) Send(ctx context.Context, notification *Notification) error {
	m.sendCalls++
	m.notifications = append(m.notifications, notification)
	if m.failNTimes > 0 && m.sendCalls <= m.failNTimes {
		return m.sendErr
	}
	if m.sendErr != nil && m.failNTimes == 0 {
		return m.sendErr
	}
	return nil
}

func (m *mockSender) HealthCheck(ctx context.Context) error {
	return nil
}

func newTestAlert() *Alert {
	return &Alert{
		ID:                "test-alert-1",
		Timestamp:         time.Now(),
		Severity:          AlertSeverityWarning,
		Category:          AlertCategoryBudget,
		Namespace:         "production",
		CurrentCost:       850.0,
		BudgetLimit:       1000.0,
		UsagePercent:      85.0,
		Message:           "Budget threshold reached",
		RecommendedAction: "Review spending",
	}
}

func TestCheckAnomalies_Detected(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	// actual = 130, forecast = 100 → 30% deviation → anomaly
	result := mgr.CheckAnomalies(ctx, 130.0, 100.0)

	if !result.IsAnomaly {
		t.Error("expected anomaly to be detected")
	}
	if result.ActualCost != 130.0 {
		t.Errorf("expected ActualCost 130.0, got %f", result.ActualCost)
	}
	if result.ForecastCost != 100.0 {
		t.Errorf("expected ForecastCost 100.0, got %f", result.ForecastCost)
	}
	if result.DeviationPct != 30.0 {
		t.Errorf("expected DeviationPct 30.0, got %f", result.DeviationPct)
	}
}

func TestCheckAnomalies_NotDetected(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	// actual = 115, forecast = 100 → 15% deviation → no anomaly
	result := mgr.CheckAnomalies(ctx, 115.0, 100.0)

	if result.IsAnomaly {
		t.Error("expected no anomaly")
	}
	if result.DeviationPct != 15.0 {
		t.Errorf("expected DeviationPct 15.0, got %f", result.DeviationPct)
	}
}

func TestCheckAnomalies_ExactThreshold(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	// actual = 120, forecast = 100 → exactly 20% → no anomaly (must be >20%)
	result := mgr.CheckAnomalies(ctx, 120.0, 100.0)

	if result.IsAnomaly {
		t.Error("expected no anomaly at exactly 20% deviation")
	}
}

func TestSendAlert_AllChannels(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	sender1 := &mockSender{}
	sender2 := &mockSender{}
	mgr.channels = []NotificationSender{sender1, sender2}

	alert := newTestAlert()
	if err := mgr.SendAlert(ctx, alert); err != nil {
		t.Fatalf("SendAlert failed: %v", err)
	}

	if sender1.sendCalls != 1 {
		t.Errorf("expected sender1 to be called 1 time, got %d", sender1.sendCalls)
	}
	if sender2.sendCalls != 1 {
		t.Errorf("expected sender2 to be called 1 time, got %d", sender2.sendCalls)
	}
}

func TestSendAlert_SkipsInSilenceWindow(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	sender := &mockSender{}
	mgr.channels = []NotificationSender{sender}

	// Set a silence window that covers now.
	now := time.Now()
	mgr.SetSilenceWindows([]v1alpha1.SilenceWindowSpec{
		{
			Start:  metav1.NewTime(now.Add(-1 * time.Hour)),
			End:    metav1.NewTime(now.Add(1 * time.Hour)),
			Reason: "maintenance",
		},
	})

	alert := newTestAlert()
	if err := mgr.SendAlert(ctx, alert); err != nil {
		t.Fatalf("SendAlert failed: %v", err)
	}

	if sender.sendCalls != 0 {
		t.Errorf("expected sender not to be called during silence window, got %d calls", sender.sendCalls)
	}
}

func TestIsInSilenceWindow_True(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	now := time.Now()
	mgr.SetSilenceWindows([]v1alpha1.SilenceWindowSpec{
		{
			Start: metav1.NewTime(now.Add(-1 * time.Hour)),
			End:   metav1.NewTime(now.Add(1 * time.Hour)),
		},
	})

	if !mgr.IsInSilenceWindow(ctx, "production") {
		t.Error("expected to be in silence window")
	}
}

func TestIsInSilenceWindow_False(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	now := time.Now()
	mgr.SetSilenceWindows([]v1alpha1.SilenceWindowSpec{
		{
			Start: metav1.NewTime(now.Add(-3 * time.Hour)),
			End:   metav1.NewTime(now.Add(-1 * time.Hour)),
		},
	})

	if mgr.IsInSilenceWindow(ctx, "production") {
		t.Error("expected not to be in silence window")
	}
}

func TestIsInSilenceWindow_NoWindows(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	if mgr.IsInSilenceWindow(ctx, "production") {
		t.Error("expected not to be in silence window when no windows configured")
	}
}

func TestRetry_SucceedsAfterFailures(t *testing.T) {
	sender := &mockSender{
		sendErr:    fmt.Errorf("temporary failure"),
		failNTimes: 2, // fail first 2 calls, succeed on 3rd
	}

	ctx := context.Background()
	notification := &Notification{
		AlertID:   "retry-test",
		Namespace: "test-ns",
	}

	err := sendWithRetry(ctx, sender, notification, 3, log.Log)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}

	if sender.sendCalls != 3 {
		t.Errorf("expected 3 send calls (2 failures + 1 success), got %d", sender.sendCalls)
	}
}

func TestRetry_ExhaustsAllRetries(t *testing.T) {
	sender := &mockSender{
		sendErr: fmt.Errorf("permanent failure"),
	}

	ctx := context.Background()
	notification := &Notification{
		AlertID:   "retry-exhaust-test",
		Namespace: "test-ns",
	}

	err := sendWithRetry(ctx, sender, notification, 3, log.Log)
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}

	// 1 initial attempt + 3 retries = 4 total calls
	if sender.sendCalls != 4 {
		t.Errorf("expected 4 send calls (1 initial + 3 retries), got %d", sender.sendCalls)
	}
}

func TestConfigureChannel_AllTypes(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	types := []string{"slack", "email", "pagerduty", "sns"}
	for _, chType := range types {
		err := mgr.ConfigureChannel(ctx, NotificationChannel{
			Type:   chType,
			Config: map[string]string{"key": "value"},
		})
		if err != nil {
			t.Errorf("ConfigureChannel(%s) failed: %v", chType, err)
		}
	}

	if len(mgr.channels) != 4 {
		t.Errorf("expected 4 channels, got %d", len(mgr.channels))
	}
}

func TestConfigureChannel_UnsupportedType(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	err := mgr.ConfigureChannel(ctx, NotificationChannel{Type: "unknown"})
	if err == nil {
		t.Error("expected error for unsupported channel type")
	}
}

func TestSendAlert_InvalidAlert(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	// Alert missing required fields.
	alert := &Alert{
		ID:        "invalid-alert",
		Timestamp: time.Now(),
	}

	err := mgr.SendAlert(ctx, alert)
	if err == nil {
		t.Error("expected error for invalid alert")
	}
}

func TestSendAlert_PartialChannelFailure(t *testing.T) {
	mgr := NewDefaultAlertManager(log.Log)
	ctx := context.Background()

	successSender := &mockSender{}
	failSender := &mockSender{sendErr: fmt.Errorf("channel down")}
	mgr.channels = []NotificationSender{successSender, failSender}

	alert := newTestAlert()
	err := mgr.SendAlert(ctx, alert)

	// Should return an error because one channel failed.
	if err == nil {
		t.Error("expected error when one channel fails")
	}

	// But the successful sender should still have been called.
	if successSender.sendCalls != 1 {
		t.Errorf("expected success sender to be called 1 time, got %d", successSender.sendCalls)
	}
}
