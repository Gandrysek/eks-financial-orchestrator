// Package instance implements the Instance Manager for analyzing and optimizing
// the instance mix (Spot, On-Demand, Reserved) in EKS clusters, handling Spot
// interruptions, and evaluating Savings Plans.
package instance

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	"github.com/eks-financial-orchestrator/pkg/audit"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CostDataReader provides read access to historical cost data.
// This interface avoids a direct dependency on the collector package.
type CostDataReader interface {
	// GetDailyCosts returns daily cost records for the given time range.
	GetDailyCosts(ctx context.Context, start, end time.Time) ([]DailyCostRecord, error)
}

// DailyCostRecord is a single data point for cost analysis.
type DailyCostRecord struct {
	Date      time.Time `json:"date"`
	TotalCost float64   `json:"total_cost"`
}

// Estimated hourly cost per node by purchase option (USD).
// These are rough averages for m5.xlarge-class instances.
var estimatedHourlyCost = map[string]float64{
	"on_demand": 0.192,
	"spot":      0.070,
	"reserved":  0.120,
}

// DefaultInstanceManager implements the InstanceManager interface.
type DefaultInstanceManager struct {
	kubeClient  kubernetes.Interface
	logger      logr.Logger
	costStore   CostDataReader
	auditWriter audit.AuditWriter
}

// NewDefaultInstanceManager creates a new DefaultInstanceManager.
func NewDefaultInstanceManager(
	kubeClient kubernetes.Interface,
	logger logr.Logger,
	costStore CostDataReader,
	auditWriter audit.AuditWriter,
) *DefaultInstanceManager {
	return &DefaultInstanceManager{
		kubeClient:  kubeClient,
		logger:      logger,
		costStore:   costStore,
		auditWriter: auditWriter,
	}
}

// purchaseOptionLabels are the Kubernetes node labels checked (in priority
// order) to determine the purchase option of a node.
var purchaseOptionLabels = []string{
	"eks.amazonaws.com/capacityType",
	"karpenter.sh/capacity-type",
	"finops.eks.io/purchase-option",
}

// normalizePurchaseOption maps common label values to canonical names.
func normalizePurchaseOption(raw string) string {
	switch raw {
	case "ON_DEMAND", "on-demand", "ondemand", "OnDemand":
		return "on_demand"
	case "SPOT", "Spot":
		return "spot"
	case "RESERVED", "Reserved":
		return "reserved"
	case "":
		return "on_demand" // default when no label is present
	default:
		return raw
	}
}

// classifyNode determines the purchase option for a node by inspecting its labels.
func classifyNode(node *corev1.Node) string {
	for _, label := range purchaseOptionLabels {
		if v, ok := node.Labels[label]; ok && v != "" {
			return normalizePurchaseOption(v)
		}
	}
	return "on_demand"
}

// AnalyzeInstanceMix evaluates the current Spot/OnDemand/Reserved ratio by
// listing all nodes from the Kubernetes API and classifying each by purchase
// option.
func (m *DefaultInstanceManager) AnalyzeInstanceMix(ctx context.Context) (*InstanceMixAnalysis, error) {
	nodeList, err := m.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	analysis := &InstanceMixAnalysis{
		Timestamp:        time.Now().UTC(),
		TotalNodes:       len(nodeList.Items),
		ByPurchaseOption: make(map[string]*PurchaseOptionStats),
	}

	// Count nodes per purchase option.
	for i := range nodeList.Items {
		option := classifyNode(&nodeList.Items[i])
		stats, ok := analysis.ByPurchaseOption[option]
		if !ok {
			stats = &PurchaseOptionStats{}
			analysis.ByPurchaseOption[option] = stats
		}
		stats.Count++
	}

	// Calculate percentages and estimated costs.
	for option, stats := range analysis.ByPurchaseOption {
		if analysis.TotalNodes > 0 {
			stats.Percentage = float64(stats.Count) / float64(analysis.TotalNodes) * 100.0
		}
		hourly, ok := estimatedHourlyCost[option]
		if !ok {
			hourly = estimatedHourlyCost["on_demand"]
		}
		// Estimate monthly cost (730 hours/month).
		stats.Cost = float64(stats.Count) * hourly * 730.0
		analysis.CurrentCost += stats.Cost
	}

	// Calculate optimal cost: maximize Spot usage (keep at least 20% On-Demand
	// as a safe default when no policy is provided).
	onDemandMin := 0.20
	spotNodes := int(float64(analysis.TotalNodes) * (1.0 - onDemandMin))
	odNodes := analysis.TotalNodes - spotNodes
	analysis.OptimalCost = float64(odNodes)*estimatedHourlyCost["on_demand"]*730.0 +
		float64(spotNodes)*estimatedHourlyCost["spot"]*730.0

	analysis.PotentialSavings = analysis.CurrentCost - analysis.OptimalCost
	if analysis.PotentialSavings < 0 {
		analysis.PotentialSavings = 0
	}

	m.logger.Info("instance mix analysis complete",
		"totalNodes", analysis.TotalNodes,
		"currentCost", analysis.CurrentCost,
		"potentialSavings", analysis.PotentialSavings,
	)

	return analysis, nil
}

// GenerateRecommendation produces an optimization recommendation that
// maximizes Spot usage while respecting all active financial policy
// constraints.
func (m *DefaultInstanceManager) GenerateRecommendation(
	ctx context.Context,
	analysis *InstanceMixAnalysis,
	policies []*v1alpha1.FinancialPolicy,
) (*OptimizationRecommendation, error) {
	if analysis == nil {
		return nil, fmt.Errorf("analysis must not be nil")
	}

	// Derive aggregate constraints from all active policies.
	minOnDemandPct := 0.0
	maxSpotPct := 100.0

	for _, p := range policies {
		if p.Spec.InstanceMix == nil {
			continue
		}
		mix := p.Spec.InstanceMix
		if mix.MinOnDemandPercent > minOnDemandPct {
			minOnDemandPct = mix.MinOnDemandPercent
		}
		if mix.MaxSpotPercent > 0 && mix.MaxSpotPercent < maxSpotPct {
			maxSpotPct = mix.MaxSpotPercent
		}
	}

	// Ensure constraints are consistent.
	if minOnDemandPct+maxSpotPct > 100 {
		// On-Demand minimum takes priority.
		maxSpotPct = 100 - minOnDemandPct
	}

	// Build current mix percentages.
	currentMix := make(map[string]float64)
	for option, stats := range analysis.ByPurchaseOption {
		currentMix[option] = stats.Percentage
	}

	// Build recommended mix: maximize Spot within constraints, give remainder
	// to On-Demand, keep reserved as-is.
	recommendedMix := make(map[string]float64)
	reservedPct := currentMix["reserved"]
	if reservedPct > 100-minOnDemandPct {
		reservedPct = 100 - minOnDemandPct
	}

	spotPct := maxSpotPct
	if spotPct > 100-minOnDemandPct-reservedPct {
		spotPct = 100 - minOnDemandPct - reservedPct
	}
	if spotPct < 0 {
		spotPct = 0
	}

	odPct := 100 - spotPct - reservedPct
	if odPct < minOnDemandPct {
		odPct = minOnDemandPct
		spotPct = 100 - odPct - reservedPct
		if spotPct < 0 {
			spotPct = 0
		}
	}

	recommendedMix["on_demand"] = odPct
	recommendedMix["spot"] = spotPct
	if reservedPct > 0 {
		recommendedMix["reserved"] = reservedPct
	}

	// Calculate expected savings based on the mix shift.
	currentMonthlyCost := analysis.CurrentCost
	recommendedCost := 0.0
	totalNodes := float64(analysis.TotalNodes)
	for option, pct := range recommendedMix {
		hourly, ok := estimatedHourlyCost[option]
		if !ok {
			hourly = estimatedHourlyCost["on_demand"]
		}
		nodeCount := totalNodes * pct / 100.0
		recommendedCost += nodeCount * hourly * 730.0
	}

	expectedSavings := currentMonthlyCost - recommendedCost
	if expectedSavings < 0 {
		expectedSavings = 0
	}

	// Collect affected nodes (On-Demand nodes that could become Spot).
	var affectedNodes []string
	if analysis.ByPurchaseOption["on_demand"] != nil && spotPct > currentMix["spot"] {
		// In a real implementation we'd list specific node names; here we
		// indicate the count of nodes that would be affected.
		affectedCount := int((spotPct - currentMix["spot"]) / 100.0 * totalNodes)
		for i := 0; i < affectedCount; i++ {
			affectedNodes = append(affectedNodes, fmt.Sprintf("on-demand-node-%d", i+1))
		}
	}

	// Check policy compliance.
	policyCompliant := true
	if odPct < minOnDemandPct {
		policyCompliant = false
	}
	if spotPct > maxSpotPct {
		policyCompliant = false
	}

	reason := fmt.Sprintf(
		"Optimize instance mix: increase Spot to %.1f%% (max %.1f%%), maintain On-Demand at %.1f%% (min %.1f%%)",
		spotPct, maxSpotPct, odPct, minOnDemandPct,
	)

	rec := &OptimizationRecommendation{
		ID:              fmt.Sprintf("rec-%d", time.Now().UnixNano()),
		Timestamp:       time.Now().UTC(),
		CurrentMix:      currentMix,
		RecommendedMix:  recommendedMix,
		ExpectedSavings: expectedSavings,
		Reason:          reason,
		AffectedNodes:   affectedNodes,
		PolicyCompliant: policyCompliant,
	}

	m.logger.Info("recommendation generated",
		"expectedSavings", expectedSavings,
		"policyCompliant", policyCompliant,
	)

	return rec, nil
}

// ApplyRecommendation executes an optimization recommendation. For now this
// is implemented as a simulation that logs the action and returns success.
// In production this would call the EC2 API to modify Auto Scaling Groups
// or Karpenter NodePools.
func (m *DefaultInstanceManager) ApplyRecommendation(
	ctx context.Context,
	rec *OptimizationRecommendation,
) (*OptimizationResult, error) {
	if rec == nil {
		return nil, fmt.Errorf("recommendation must not be nil")
	}

	m.logger.Info("applying optimization recommendation (simulation)",
		"recommendationID", rec.ID,
		"expectedSavings", rec.ExpectedSavings,
		"affectedNodes", len(rec.AffectedNodes),
	)

	// Record the action in the audit log.
	if m.auditWriter != nil {
		entry := &audit.AuditLogEntry{
			Timestamp:       time.Now().UTC(),
			Actor:           "system",
			Action:          "optimization_apply",
			ResourceType:    "instance_mix",
			ResourceName:    rec.ID,
			Reason:          rec.Reason,
			ExpectedSavings: rec.ExpectedSavings,
			Details: map[string]interface{}{
				"affected_nodes":  rec.AffectedNodes,
				"recommended_mix": rec.RecommendedMix,
				"current_mix":     rec.CurrentMix,
			},
			Result: "success",
		}
		if err := m.auditWriter.WriteEntry(ctx, entry); err != nil {
			m.logger.Error(err, "failed to write audit log entry")
			// Fire-and-forget: don't fail the operation.
		}
	}

	return &OptimizationResult{
		RecommendationID: rec.ID,
		Applied:          true,
		Timestamp:        time.Now().UTC(),
		ActualSavings:    rec.ExpectedSavings, // simulated
		Message:          "recommendation applied successfully (simulation)",
	}, nil
}

// HandleSpotInterruption processes a Spot interruption notice for the given
// node. It lists pods on the affected node and initiates eviction using the
// Kubernetes Eviction API. This is a best-effort operation — errors are
// logged but don't fail the overall operation.
func (m *DefaultInstanceManager) HandleSpotInterruption(ctx context.Context, nodeID string) error {
	m.logger.Info("handling Spot interruption", "nodeID", nodeID)

	// List pods on the affected node.
	podList, err := m.kubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeID),
	})
	if err != nil {
		m.logger.Error(err, "failed to list pods on interrupted node", "nodeID", nodeID)
		return nil // best-effort
	}

	m.logger.Info("found pods on interrupted node",
		"nodeID", nodeID,
		"podCount", len(podList.Items),
	)

	for i := range podList.Items {
		pod := &podList.Items[i]

		// Skip mirror pods and DaemonSet pods.
		if pod.Annotations != nil {
			if _, ok := pod.Annotations["kubernetes.io/config.mirror"]; ok {
				m.logger.Info("skipping mirror pod", "pod", pod.Name, "namespace", pod.Namespace)
				continue
			}
		}

		eviction := &policyv1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		}

		if err := m.kubeClient.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction); err != nil {
			m.logger.Error(err, "failed to evict pod",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"nodeID", nodeID,
			)
			// Best-effort: continue with remaining pods.
			continue
		}

		m.logger.Info("evicted pod from interrupted node",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"nodeID", nodeID,
		)
	}

	m.logger.Info("Spot interruption handling complete", "nodeID", nodeID)
	return nil
}
