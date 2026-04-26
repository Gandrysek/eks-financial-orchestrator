package collector

// System namespace constants — namespaces that contain shared cluster
// infrastructure costs (control plane, networking, DNS, etc.).
const (
	NamespaceKubeSystem    = "kube-system"
	NamespaceKubePublic    = "kube-public"
	NamespaceKubeNodeLease = "kube-node-lease"
)

// SharedLabelKey is the label that marks a namespace as containing shared
// costs that should be distributed across other namespaces.
const SharedLabelKey = "finops.eks.io/shared"

// UnallocatedTeam is the team name used for namespaces that do not have
// a team label.
const UnallocatedTeam = "unallocated"

// systemNamespaces is the set of well-known Kubernetes system namespaces.
var systemNamespaces = map[string]bool{
	NamespaceKubeSystem:    true,
	NamespaceKubePublic:    true,
	NamespaceKubeNodeLease: true,
}

// isSystemNamespace returns true if the given namespace is a well-known
// Kubernetes system namespace (kube-system, kube-public, kube-node-lease)
// or has the "finops.eks.io/shared=true" label. The labels parameter is
// optional; if nil, only the well-known names are checked.
func isSystemNamespace(namespace string, labels map[string]string) bool {
	if systemNamespaces[namespace] {
		return true
	}
	if labels != nil && labels[SharedLabelKey] == "true" {
		return true
	}
	return false
}

// allocateSharedCosts distributes shared cluster costs (from system
// namespaces) proportionally across non-system namespaces based on their
// DirectCost as a proxy for resource usage.
//
// The algorithm:
//  1. Separate namespaces into system (shared) and non-system (recipients).
//  2. Sum the DirectCost of all system namespaces → totalSharedCost.
//  3. Sum the DirectCost of all non-system namespaces → totalNonSystemDirect.
//  4. For each non-system namespace, allocate a share of the shared cost
//     proportional to its DirectCost / totalNonSystemDirect.
//  5. If totalNonSystemDirect is zero (all non-system namespaces have zero
//     direct cost), distribute shared costs evenly.
//  6. Update TotalCost = DirectCost + SharedCost for each non-system namespace.
//  7. Rebuild team aggregation with indirect costs.
//
// Edge cases:
//   - No shared costs → return costs unchanged.
//   - No non-system namespaces → shared costs remain unallocated (no recipients).
//   - All namespaces are system namespaces → same as above.
func allocateSharedCosts(costs *AggregatedCosts) *AggregatedCosts {
	return allocateSharedCostsWithLabels(costs, nil)
}

// allocateSharedCostsWithLabels is the label-aware version that accepts
// namespace labels to correctly identify custom shared namespaces.
func allocateSharedCostsWithLabels(costs *AggregatedCosts, nsLabels map[string]map[string]string) *AggregatedCosts {
	if costs == nil {
		return nil
	}

	if len(costs.ByNamespace) == 0 {
		return costs
	}

	// Partition namespaces into system and non-system using actual labels.
	var systemNS []string
	var nonSystemNS []string
	for nsName := range costs.ByNamespace {
		labels := nsLabels[nsName] // may be nil if labels weren't fetched
		if isSystemNamespace(nsName, labels) {
			systemNS = append(systemNS, nsName)
		} else {
			nonSystemNS = append(nonSystemNS, nsName)
		}
	}

	// Calculate total shared cost from system namespaces.
	var totalSharedCost float64
	for _, nsName := range systemNS {
		totalSharedCost += costs.ByNamespace[nsName].DirectCost
	}

	// If there are no shared costs, nothing to distribute.
	if totalSharedCost == 0 {
		return costs
	}

	// If there are no non-system namespaces, shared costs remain unallocated.
	if len(nonSystemNS) == 0 {
		return costs
	}

	// Calculate total direct cost across non-system namespaces.
	var totalNonSystemDirect float64
	for _, nsName := range nonSystemNS {
		totalNonSystemDirect += costs.ByNamespace[nsName].DirectCost
	}

	// Distribute shared costs proportionally (or evenly if zero direct costs).
	for _, nsName := range nonSystemNS {
		nsCost := costs.ByNamespace[nsName]
		var share float64
		if totalNonSystemDirect == 0 {
			// Distribute evenly when all non-system namespaces have zero direct cost.
			share = totalSharedCost / float64(len(nonSystemNS))
		} else {
			share = totalSharedCost * (nsCost.DirectCost / totalNonSystemDirect)
		}
		nsCost.SharedCost += share
		nsCost.TotalCost = nsCost.DirectCost + nsCost.SharedCost
	}

	// Zero out system namespace TotalCost to prevent double-counting.
	// Their cost has been fully redistributed into non-system namespaces'
	// SharedCost. We keep them in ByNamespace for visibility but with
	// TotalCost = 0 so the cluster total is not inflated.
	for _, nsName := range systemNS {
		nsCost := costs.ByNamespace[nsName]
		nsCost.TotalCost = 0
	}

	// Rebuild team aggregation with indirect costs (shared cost allocation).
	newByTeam := make(map[string]*TeamCost)
	for _, nsName := range nonSystemNS {
		nsCost := costs.ByNamespace[nsName]
		team := nsCost.Team
		if team == "" {
			team = UnallocatedTeam
		}

		tc, ok := newByTeam[team]
		if !ok {
			tc = &TeamCost{
				Team:       team,
				Namespaces: make(map[string]float64),
			}
			newByTeam[team] = tc
		}
		tc.DirectCost += nsCost.DirectCost
		tc.IndirectCost += nsCost.SharedCost
		tc.TotalCost = tc.DirectCost + tc.IndirectCost
		tc.Namespaces[nsName] = nsCost.TotalCost
	}

	costs.ByTeam = newByTeam

	// Recalculate total cost — only non-system namespaces contribute,
	// since system namespace costs have been redistributed.
	var newTotal float64
	for _, nsName := range nonSystemNS {
		newTotal += costs.ByNamespace[nsName].TotalCost
	}
	costs.TotalCost = newTotal

	return costs
}
