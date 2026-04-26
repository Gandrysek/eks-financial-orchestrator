package collector

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1api "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestCollectClusterMetrics_Basic(t *testing.T) {
	ctx := context.Background()

	// Set up fake Kubernetes objects.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-app-1",
			Namespace: "default",
			Labels:    map[string]string{"app": "web"},
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Labels: map[string]string{
				"node.kubernetes.io/instance-type":  "m5.large",
				"eks.amazonaws.com/capacityType":    "ON_DEMAND",
			},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
		},
	}

	kubeClient := fake.NewSimpleClientset(ns, pod, node)

	// Set up fake metrics objects using reactors for proper list handling.
	podMetrics := metricsv1beta1api.PodMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "web-app-1", Namespace: "default"},
		Containers: []metricsv1beta1api.ContainerMetrics{
			{
				Name: "web",
				Usage: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
		},
	}
	nodeMetrics := metricsv1beta1api.NodeMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Usage: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
	}

	metricsClientset := metricsfake.NewSimpleClientset()
	metricsClientset.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1api.PodMetricsList{
			Items: []metricsv1beta1api.PodMetrics{podMetrics},
		}, nil
	})
	metricsClientset.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1api.NodeMetricsList{
			Items: []metricsv1beta1api.NodeMetrics{nodeMetrics},
		}, nil
	})

	collector := NewDefaultCostCollector(
		kubeClient,
		metricsClientset.MetricsV1beta1(),
		logr.Discard(),
	)

	result, err := collector.CollectClusterMetrics(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify pods.
	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(result.Pods))
	}
	p := result.Pods[0]
	if p.Name != "web-app-1" {
		t.Errorf("expected pod name web-app-1, got %s", p.Name)
	}
	if p.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", p.Namespace)
	}
	if p.NodeName != "node-1" {
		t.Errorf("expected node name node-1, got %s", p.NodeName)
	}
	// 250m = 0.25 cores
	if p.CPUUsage != 0.25 {
		t.Errorf("expected CPU usage 0.25, got %f", p.CPUUsage)
	}
	// 512Mi = 536870912 bytes
	expectedMem := float64(512 * 1024 * 1024)
	if p.MemUsage != expectedMem {
		t.Errorf("expected memory usage %f, got %f", expectedMem, p.MemUsage)
	}
	if p.Labels["app"] != "web" {
		t.Errorf("expected label app=web, got %v", p.Labels)
	}

	// Verify nodes.
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	n := result.Nodes[0]
	if n.Name != "node-1" {
		t.Errorf("expected node name node-1, got %s", n.Name)
	}
	if n.InstanceType != "m5.large" {
		t.Errorf("expected instance type m5.large, got %s", n.InstanceType)
	}
	if n.PurchaseOption != "on_demand" {
		t.Errorf("expected purchase option on_demand, got %s", n.PurchaseOption)
	}
	if n.CPUCapacity != 4.0 {
		t.Errorf("expected CPU capacity 4.0, got %f", n.CPUCapacity)
	}
	if n.CPUUsage != 2.0 {
		t.Errorf("expected CPU usage 2.0, got %f", n.CPUUsage)
	}
}

func TestCollectClusterMetrics_EmptyCluster(t *testing.T) {
	ctx := context.Background()

	kubeClient := fake.NewSimpleClientset()
	metricsClientset := metricsfake.NewSimpleClientset()

	collector := NewDefaultCostCollector(
		kubeClient,
		metricsClientset.MetricsV1beta1(),
		logr.Discard(),
	)

	result, err := collector.CollectClusterMetrics(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Pods) != 0 {
		t.Errorf("expected 0 pods, got %d", len(result.Pods))
	}
	if len(result.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(result.Nodes))
	}
	if result.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestDetectPurchaseOption(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		annots   map[string]string
		expected string
	}{
		{
			name:     "custom label",
			labels:   map[string]string{"finops.eks.io/purchase-option": "reserved"},
			expected: "reserved",
		},
		{
			name:     "custom annotation",
			annots:   map[string]string{"finops.eks.io/purchase-option": "savings_plan"},
			expected: "savings_plan",
		},
		{
			name:     "karpenter spot",
			labels:   map[string]string{"karpenter.sh/capacity-type": "spot"},
			expected: "spot",
		},
		{
			name:     "karpenter on-demand",
			labels:   map[string]string{"karpenter.sh/capacity-type": "on-demand"},
			expected: "on_demand",
		},
		{
			name:     "EKS SPOT",
			labels:   map[string]string{"eks.amazonaws.com/capacityType": "SPOT"},
			expected: "spot",
		},
		{
			name:     "EKS ON_DEMAND",
			labels:   map[string]string{"eks.amazonaws.com/capacityType": "ON_DEMAND"},
			expected: "on_demand",
		},
		{
			name:     "no labels defaults to on_demand",
			labels:   map[string]string{},
			expected: "on_demand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      tt.labels,
					Annotations: tt.annots,
				},
			}
			got := detectPurchaseOption(node)
			if got != tt.expected {
				t.Errorf("detectPurchaseOption() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestQuantityConversions(t *testing.T) {
	// CPU: 500m = 0.5 cores
	cpuQty := resource.MustParse("500m")
	if got := quantityToFloat64Cores(&cpuQty); got != 0.5 {
		t.Errorf("quantityToFloat64Cores(500m) = %f, want 0.5", got)
	}

	// CPU: 2 = 2.0 cores
	cpuQty2 := resource.MustParse("2")
	if got := quantityToFloat64Cores(&cpuQty2); got != 2.0 {
		t.Errorf("quantityToFloat64Cores(2) = %f, want 2.0", got)
	}

	// Memory: 1Gi = 1073741824 bytes
	memQty := resource.MustParse("1Gi")
	expected := float64(1024 * 1024 * 1024)
	if got := quantityToFloat64Bytes(&memQty); got != expected {
		t.Errorf("quantityToFloat64Bytes(1Gi) = %f, want %f", got, expected)
	}
}

func TestStubMethodsReturnErrors(t *testing.T) {
	ctx := context.Background()
	kubeClient := fake.NewSimpleClientset()
	metricsClientset := metricsfake.NewSimpleClientset()

	// Without AWS config, FetchAWSCosts and FetchCURData return config errors.
	collector := NewDefaultCostCollector(
		kubeClient,
		metricsClientset.MetricsV1beta1(),
		logr.Discard(),
	)

	if _, err := collector.FetchAWSCosts(ctx, time.Time{}, time.Time{}); err == nil {
		t.Error("FetchAWSCosts should return error when AWS fetcher not configured")
	}
	if _, err := collector.FetchCURData(ctx, time.Time{}, time.Time{}); err == nil {
		t.Error("FetchCURData should return error when AWS fetcher not configured")
	}
	// CorrelateAndAggregate handles nil inputs gracefully (returns empty aggregation).
	result, err := collector.CorrelateAndAggregate(ctx, nil, nil)
	if err != nil {
		t.Errorf("CorrelateAndAggregate with nil inputs should succeed, got error: %v", err)
	}
	if result == nil {
		t.Error("CorrelateAndAggregate with nil inputs should return non-nil result")
	}
	if _, err := collector.AllocateSharedCosts(ctx, nil); err == nil {
		t.Error("AllocateSharedCosts should return error for nil input")
	}
}
