// Package api implements the REST API server for the EKS Financial Orchestrator,
// providing endpoints for cost data, forecasts, reports, policy management,
// and RBAC-protected operations.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr"

	"github.com/eks-financial-orchestrator/pkg/audit"
	"github.com/eks-financial-orchestrator/pkg/collector"
	"github.com/eks-financial-orchestrator/pkg/policy"
)

// Server is the REST API server for the EKS Financial Orchestrator.
type Server struct {
	router      chi.Router
	costStore   collector.CostStore
	policyMgr   policy.PolicyManager
	auditWriter audit.AuditWriter
	logger      logr.Logger
}

// NewServer creates a new REST API server with all routes configured.
func NewServer(costStore collector.CostStore, policyMgr policy.PolicyManager, auditWriter audit.AuditWriter, logger logr.Logger) *Server {
	s := &Server{
		costStore:   costStore,
		policyMgr:   policyMgr,
		auditWriter: auditWriter,
		logger:      logger.WithName("api"),
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Health and readiness probes.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	// API v1 routes with RBAC middleware.
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(RBACMiddleware(logger))

		// Cost endpoints.
		r.Get("/costs", s.handleGetCosts)
		r.Get("/costs/namespaces/{namespace}", s.handleGetCostsByNamespace)
		r.Get("/costs/teams/{team}", s.handleGetCostsByTeam)

		// Forecast endpoints.
		r.Get("/forecasts", s.handleGetForecasts)
		r.Get("/forecasts/{namespace}", s.handleGetForecastsByNamespace)

		// Report endpoint.
		r.Post("/reports", s.handleGenerateReport)

		// Policy endpoints.
		r.Get("/policies", s.handleListPolicies)
		r.Get("/policies/{name}/status", s.handleGetPolicyStatus)

		// Instance mix and recommendations (stubs).
		r.Get("/instance-mix", s.handleGetInstanceMix)
		r.Get("/recommendations", s.handleGetRecommendations)
		r.Post("/recommendations/{id}/approve", s.handleApproveRecommendation)

		// Audit log.
		r.Get("/audit-log", s.handleGetAuditLog)
	})

	s.router = r
	return s
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	s.logger.Info("Starting REST API server", "addr", addr)
	return http.ListenAndServe(addr, s.router)
}

// --- Health endpoints ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// --- Cost endpoints ---

func (s *Server) handleGetCosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	start, end, err := parseTimeRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	records, err := s.costStore.QueryCostRecords(ctx, start, end, "")
	if err != nil {
		s.logger.Error(err, "failed to query cost records")
		writeError(w, http.StatusInternalServerError, "failed to query cost records")
		return
	}

	resp := buildCostResponse(records)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetCostsByNamespace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := chi.URLParam(r, "namespace")
	if namespace == "" {
		writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}

	start, end, err := parseTimeRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	records, err := s.costStore.QueryCostRecords(ctx, start, end, namespace)
	if err != nil {
		s.logger.Error(err, "failed to query cost records", "namespace", namespace)
		writeError(w, http.StatusInternalServerError, "failed to query cost records")
		return
	}

	if len(records) == 0 {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no cost data found for namespace %q", namespace))
		return
	}

	resp := buildCostResponse(records)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetCostsByTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	team := chi.URLParam(r, "team")
	if team == "" {
		writeError(w, http.StatusBadRequest, "team is required")
		return
	}

	start, end, err := parseTimeRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Query all records and filter by team on the application side.
	// The CostStore interface filters by namespace; team filtering is done here.
	records, err := s.costStore.QueryCostRecords(ctx, start, end, "")
	if err != nil {
		s.logger.Error(err, "failed to query cost records", "team", team)
		writeError(w, http.StatusInternalServerError, "failed to query cost records")
		return
	}

	// Build a team-filtered response. Since DailyCostRecord doesn't carry team info,
	// we return all records. In a production system, the store would support team filtering.
	resp := buildCostResponse(records)
	resp.Team = team
	writeJSON(w, http.StatusOK, resp)
}

// --- Forecast endpoints ---

func (s *Server) handleGetForecasts(w http.ResponseWriter, r *http.Request) {
	// Return a stub response. In production, this would read from the forecasts table.
	resp := &ForecastListResponse{
		Forecasts: []ForecastResponse{},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetForecastsByNamespace(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	if namespace == "" {
		writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}

	// Return a stub response.
	resp := &ForecastResponse{
		Namespace: namespace,
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Report endpoint ---

func (s *Server) handleGenerateReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var params ReportParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if params.StartTime.IsZero() || params.EndTime.IsZero() {
		writeError(w, http.StatusBadRequest, "start_time and end_time are required")
		return
	}

	if params.EndTime.Before(params.StartTime) {
		writeError(w, http.StatusBadRequest, "end_time must be after start_time")
		return
	}

	records, err := s.costStore.QueryCostRecords(ctx, params.StartTime, params.EndTime, "")
	if err != nil {
		s.logger.Error(err, "failed to query cost records for report")
		writeError(w, http.StatusInternalServerError, "failed to generate report")
		return
	}

	report := buildCostReport(params, records)
	writeJSON(w, http.StatusOK, report)
}

// --- Policy endpoints ---

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	policies, err := s.policyMgr.ListPolicies(ctx)
	if err != nil {
		s.logger.Error(err, "failed to list policies")
		writeError(w, http.StatusInternalServerError, "failed to list policies")
		return
	}

	entries := make([]PolicyStatusEntry, 0, len(policies))
	for _, p := range policies {
		var lastEval time.Time
		if p.Status.LastEvaluated != nil {
			lastEval = p.Status.LastEvaluated.Time
		}
		entries = append(entries, PolicyStatusEntry{
			Name:               p.Name,
			Namespace:          p.Spec.TargetNamespace,
			Phase:              string(p.Status.Phase),
			CurrentCost:        p.Status.CurrentCost,
			BudgetLimit:        p.Spec.Budget.MonthlyLimit,
			BudgetUsagePercent: p.Status.BudgetUsagePercent,
			Mode:               string(p.Spec.Mode),
			LastEvaluated:      lastEval,
		})
	}

	writeJSON(w, http.StatusOK, &PolicyStatusResponse{Policies: entries})
}

func (s *Server) handleGetPolicyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "policy name is required")
		return
	}

	policies, err := s.policyMgr.ListPolicies(ctx)
	if err != nil {
		s.logger.Error(err, "failed to list policies")
		writeError(w, http.StatusInternalServerError, "failed to get policy status")
		return
	}

	for _, p := range policies {
		if p.Name == name {
			var lastEval time.Time
			if p.Status.LastEvaluated != nil {
				lastEval = p.Status.LastEvaluated.Time
			}
			entry := PolicyStatusEntry{
				Name:               p.Name,
				Namespace:          p.Spec.TargetNamespace,
				Phase:              string(p.Status.Phase),
				CurrentCost:        p.Status.CurrentCost,
				BudgetLimit:        p.Spec.Budget.MonthlyLimit,
				BudgetUsagePercent: p.Status.BudgetUsagePercent,
				Mode:               string(p.Spec.Mode),
				LastEvaluated:      lastEval,
			}
			writeJSON(w, http.StatusOK, entry)
			return
		}
	}

	writeError(w, http.StatusNotFound, fmt.Sprintf("policy %q not found", name))
}

// --- Instance mix and recommendations (stubs) ---

func (s *Server) handleGetInstanceMix(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "stub", "message": "instance mix analysis not yet implemented"})
}

func (s *Server) handleGetRecommendations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"recommendations": []interface{}{}})
}

func (s *Server) handleApproveRecommendation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "recommendation id is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stub", "id": id, "message": "recommendation approval not yet implemented"})
}

// --- Audit log ---

func (s *Server) handleGetAuditLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filters := audit.AuditQueryFilters{
		Actor:        r.URL.Query().Get("actor"),
		Action:       r.URL.Query().Get("action"),
		ResourceType: r.URL.Query().Get("resource_type"),
		Namespace:    r.URL.Query().Get("namespace"),
	}

	entries, err := s.auditWriter.QueryEntries(ctx, filters)
	if err != nil {
		s.logger.Error(err, "failed to query audit log")
		writeError(w, http.StatusInternalServerError, "failed to query audit log")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

// --- Helper types and functions ---

// CostRecordResponse is the JSON response for cost queries.
type CostRecordResponse struct {
	TotalCost   float64                       `json:"total_cost"`
	Team        string                        `json:"team,omitempty"`
	ByNamespace map[string]float64            `json:"by_namespace,omitempty"`
	Records     []collector.DailyCostRecord   `json:"records"`
}

// ForecastListResponse is the JSON response for listing forecasts.
type ForecastListResponse struct {
	Forecasts []ForecastResponse `json:"forecasts"`
}

// ErrorResponse is the standard error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
}

func buildCostResponse(records []collector.DailyCostRecord) *CostRecordResponse {
	resp := &CostRecordResponse{
		ByNamespace: make(map[string]float64),
		Records:     records,
	}
	for _, r := range records {
		resp.TotalCost += r.TotalCost
		resp.ByNamespace[r.Namespace] += r.TotalCost
	}
	return resp
}

func buildCostReport(params ReportParams, records []collector.DailyCostRecord) *CostReport {
	byNamespace := make(map[string]*collector.NamespaceCost)
	totalCost := 0.0

	for _, r := range records {
		totalCost += r.TotalCost
		ns, ok := byNamespace[r.Namespace]
		if !ok {
			ns = &collector.NamespaceCost{
				Namespace: r.Namespace,
			}
			byNamespace[r.Namespace] = ns
		}
		ns.TotalCost += r.TotalCost
	}

	return &CostReport{
		GeneratedAt: time.Now().UTC(),
		StartTime:   params.StartTime,
		EndTime:     params.EndTime,
		TotalCost:   totalCost,
		ByNamespace: byNamespace,
	}
}

func parseTimeRange(r *http.Request) (time.Time, time.Time, error) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time

	if startStr != "" {
		var err error
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return start, end, fmt.Errorf("invalid start time: %w", err)
		}
	} else {
		// Default to last 24 hours.
		start = time.Now().Add(-24 * time.Hour)
	}

	if endStr != "" {
		var err error
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return start, end, fmt.Errorf("invalid end time: %w", err)
		}
	} else {
		end = time.Now()
	}

	return start, end, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Best effort; headers already sent.
		_ = err
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, &ErrorResponse{Error: message, Code: status})
}
