package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/alerts"
)

// =============================================================================
// Alert metrics tests (TASK-332)
// =============================================================================

// mockAlertSource implements AlertMetricsSource for testing.
type mockAlertSource struct {
	snap alerts.AlertMetricsSnapshot
}

func (s *mockAlertSource) AlertSnapshot() alerts.AlertMetricsSnapshot {
	return s.snap
}

func TestPrometheusExporter_AlertMetrics_NotEmittedWhenNilSource(t *testing.T) {
	exp := NewPrometheusExporter(&mockMetricsSource{})
	var buf bytes.Buffer
	if err := exp.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "alerts_fired_total") {
		t.Error("alert series should not appear when no AlertMetricsSource is wired")
	}
}

func TestPrometheusExporter_AlertMetrics_SeriesRendered(t *testing.T) {
	m := alerts.NewAlertMetrics()
	m.RecordFired("task-failed-rule", "critical")
	m.RecordFired("task-failed-rule", "critical")
	m.RecordDelivery("slack-ops", "slack", "success")
	m.RecordDelivery("slack-ops", "slack", "failure")
	m.RecordDropped()

	snap := m.Snapshot()
	snap.QueueDepth = 7

	exp := NewPrometheusExporter(&mockMetricsSource{})
	exp.SetAlertsSource(&mockAlertSource{snap: snap})

	var buf bytes.Buffer
	if err := exp.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus error: %v", err)
	}
	out := buf.String()

	expectations := []string{
		`# HELP alerts_fired_total`,
		`# TYPE alerts_fired_total counter`,
		`alerts_fired_total{rule="task-failed-rule",severity="critical"} 2`,
		`# HELP alert_delivery_total`,
		`# TYPE alert_delivery_total counter`,
		`alert_delivery_total{channel="slack-ops",type="slack",result="success"} 1`,
		`alert_delivery_total{channel="slack-ops",type="slack",result="failure"} 1`,
		`# HELP alert_events_dropped_total`,
		`# TYPE alert_events_dropped_total counter`,
		`alert_events_dropped_total 1`,
		`# HELP alert_queue_depth`,
		`# TYPE alert_queue_depth gauge`,
		`alert_queue_depth 7`,
	}
	for _, want := range expectations {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q\ngot:\n%s", want, out)
		}
	}
}

func TestPrometheusExporter_AlertMetrics_SetAlertsSourceViaServer(t *testing.T) {
	srv := NewServer(&Config{Host: "127.0.0.1", Port: 0})
	srv.SetMetricsSource(&mockMetricsSource{})

	m := alerts.NewAlertMetrics()
	m.RecordFired("stuck-task", "warning")
	snap := m.Snapshot()

	srv.SetAlertsMetricsSource(&mockAlertSource{snap: snap})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `alerts_fired_total{rule="stuck-task",severity="warning"} 1`) {
		t.Errorf("alert series not found in output:\n%s", w.Body.String())
	}
}

func TestPrometheusExporter_AlertMetrics_SetAlertsSourceBeforeMetricsSource(t *testing.T) {
	// Verify SetAlertsMetricsSource is safe to call before SetMetricsSource
	srv := NewServer(&Config{Host: "127.0.0.1", Port: 0})

	m := alerts.NewAlertMetrics()
	m.RecordDropped()
	snap := m.Snapshot()

	srv.SetAlertsMetricsSource(&mockAlertSource{snap: snap})
	srv.SetMetricsSource(&mockMetricsSource{}) // exporter created after alertsSource is stored

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "alert_events_dropped_total 1") {
		t.Errorf("alert_events_dropped_total not found:\n%s", w.Body.String())
	}
}
