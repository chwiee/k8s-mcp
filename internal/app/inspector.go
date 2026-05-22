package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Inspector interface {
	InspectNamespace(ctx context.Context, namespace string) (*NamespaceDetails, error)
	InspectPod(ctx context.Context, namespace, name string, tailLines int64) (*PodDetails, error)
	InspectDeployment(ctx context.Context, namespace, name string) (*DeploymentDetails, error)
	InspectService(ctx context.Context, namespace, name string) (*ServiceDetails, error)
	InspectComponents(ctx context.Context, components []string) ([]ComponentDetails, error)
}

type UnavailableInspector struct {
	err error
}

func NewUnavailableInspector(err error) Inspector {
	return UnavailableInspector{err: err}
}

func (u UnavailableInspector) InspectNamespace(context.Context, string) (*NamespaceDetails, error) {
	return nil, u.err
}

func (u UnavailableInspector) InspectPod(context.Context, string, string, int64) (*PodDetails, error) {
	return nil, u.err
}

func (u UnavailableInspector) InspectDeployment(context.Context, string, string) (*DeploymentDetails, error) {
	return nil, u.err
}

func (u UnavailableInspector) InspectService(context.Context, string, string) (*ServiceDetails, error) {
	return nil, u.err
}

func (u UnavailableInspector) InspectComponents(context.Context, []string) ([]ComponentDetails, error) {
	return nil, u.err
}

type KubeInspector struct {
	client kubernetes.Interface
}

func NewKubeInspector(kubeconfig string) (Inspector, error) {
	cfg, err := loadKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return &KubeInspector{client: clientset}, nil
}

func loadKubeConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory for kubeconfig: %w", err)
	}
	path := filepath.Join(home, ".kube", "config")
	return clientcmd.BuildConfigFromFlags("", path)
}

func (k *KubeInspector) InspectNamespace(ctx context.Context, namespace string) (*NamespaceDetails, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	pods, err := k.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods in namespace %s: %w", namespace, err)
	}
	result := &NamespaceDetails{Name: namespace, PodCount: len(pods.Items)}
	for _, pod := range pods.Items {
		summary := podSummary(pod)
		if summary.Phase != string(corev1.PodRunning) || summary.Ready != "ready" {
			result.NonRunningPods = append(result.NonRunningPods, summary)
		}
	}
	result.Events, _ = k.listEvents(ctx, namespace, "")
	return result, nil
}

func (k *KubeInspector) InspectPod(ctx context.Context, namespace, name string, tailLines int64) (*PodDetails, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and pod name are required")
	}
	pod, err := k.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
	}
	result := &PodDetails{Summary: podSummary(*pod)}
	logs, err := k.getPodLogs(ctx, namespace, name, tailLines)
	if err == nil {
		result.Logs = logs
	}
	result.Events, _ = k.listEvents(ctx, namespace, name)
	return result, nil
}

func (k *KubeInspector) InspectDeployment(ctx context.Context, namespace, name string) (*DeploymentDetails, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and deployment name are required")
	}
	deployment, err := k.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	result := &DeploymentDetails{
		Name:            deployment.Name,
		Namespace:       deployment.Namespace,
		Replicas:        deployment.Status.Replicas,
		ReadyReplicas:   deployment.Status.ReadyReplicas,
		UpdatedReplicas: deployment.Status.UpdatedReplicas,
		Available:       deployment.Status.AvailableReplicas >= maxReplicas(deployment),
	}
	result.Pods, _ = k.listPodsBySelector(ctx, namespace, deployment.Spec.Selector)
	result.Events, _ = k.listEvents(ctx, namespace, name)
	return result, nil
}

func (k *KubeInspector) InspectService(ctx context.Context, namespace, name string) (*ServiceDetails, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and service name are required")
	}
	service, err := k.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get service %s/%s: %w", namespace, name, err)
	}
	result := &ServiceDetails{
		Name:      service.Name,
		Namespace: service.Namespace,
		Type:      string(service.Spec.Type),
		ClusterIP: service.Spec.ClusterIP,
		Selector:  service.Spec.Selector,
	}
	result.Endpoints, _ = k.listServiceEndpoints(ctx, namespace, name)
	if len(service.Spec.Selector) > 0 {
		selector := &metav1.LabelSelector{MatchLabels: service.Spec.Selector}
		result.Pods, _ = k.listPodsBySelector(ctx, namespace, selector)
	}
	result.HPAs, _ = k.listNamespaceHPAs(ctx, namespace)
	result.Events, _ = k.listEvents(ctx, namespace, name)
	return result, nil
}

func (k *KubeInspector) InspectComponents(ctx context.Context, components []string) ([]ComponentDetails, error) {
	if len(components) == 0 {
		return nil, nil
	}
	result := make([]ComponentDetails, 0, len(components))
	for _, component := range components {
		namespaces := componentNamespaces(component)
		for _, namespace := range namespaces {
			pods, err := k.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			matched := make([]PodSummary, 0, len(pods.Items))
			for _, pod := range pods.Items {
				name := normalizeText(pod.Name)
				if strings.Contains(name, normalizeText(component)) || normalizeText(component) == "nginx" && strings.Contains(name, "ingress") {
					matched = append(matched, podSummary(pod))
				}
			}
			if len(matched) == 0 {
				continue
			}
			result = append(result, ComponentDetails{
				Component: component,
				Namespace: namespace,
				Pods:      matched,
				Notes: []string{
					"Component pods were included because the request referenced a common cluster add-on.",
				},
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Component == result[j].Component {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Component < result[j].Component
	})
	return result, nil
}

func componentNamespaces(component string) []string {
	switch strings.ToLower(component) {
	case "keda":
		return []string{"keda"}
	case "istio":
		return []string{"istio-system"}
	case "calico":
		return []string{"calico-system", "kube-system"}
	case "nginx":
		return []string{"ingress-nginx", "nginx-ingress"}
	default:
		return []string{component}
	}
}

func (k *KubeInspector) listPodsBySelector(ctx context.Context, namespace string, selector *metav1.LabelSelector) ([]PodSummary, error) {
	if selector == nil {
		return nil, nil
	}
	compiled, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	pods, err := k.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: compiled.String()})
	if err != nil {
		return nil, err
	}
	result := make([]PodSummary, 0, len(pods.Items))
	for _, pod := range pods.Items {
		result = append(result, podSummary(pod))
	}
	return result, nil
}

func (k *KubeInspector) listServiceEndpoints(ctx context.Context, namespace, name string) ([]string, error) {
	endpoints, err := k.client.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var result []string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			for _, port := range subset.Ports {
				result = append(result, fmt.Sprintf("%s:%d", address.IP, port.Port))
			}
		}
	}
	return result, nil
}

func (k *KubeInspector) listNamespaceHPAs(ctx context.Context, namespace string) ([]HPASummary, error) {
	hpas, err := k.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]HPASummary, 0, len(hpas.Items))
	for _, hpa := range hpas.Items {
		result = append(result, hpaSummary(hpa))
	}
	return result, nil
}

func (k *KubeInspector) listEvents(ctx context.Context, namespace, name string) ([]EventRecord, error) {
	events, err := k.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]EventRecord, 0, len(events.Items))
	for _, event := range events.Items {
		if name != "" && event.InvolvedObject.Name != name {
			continue
		}
		timestamp := event.LastTimestamp.Time
		if timestamp.IsZero() {
			timestamp = event.EventTime.Time
		}
		result = append(result, EventRecord{
			Type:          event.Type,
			Reason:        event.Reason,
			Message:       event.Message,
			LastTimestamp: timestamp.Format(time.RFC3339),
			Count:         event.Count,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastTimestamp > result[j].LastTimestamp
	})
	if len(result) > 10 {
		return result[:10], nil
	}
	return result, nil
}

func (k *KubeInspector) getPodLogs(ctx context.Context, namespace, name string, tailLines int64) (string, error) {
	request := k.client.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{TailLines: &tailLines})
	stream, err := request.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(stream); err != nil {
		return "", err
	}
	return strings.TrimSpace(buffer.String()), nil
}

func podSummary(pod corev1.Pod) PodSummary {
	images := make([]string, 0, len(pod.Spec.Containers))
	var restarts int32
	var readyContainers int
	for _, container := range pod.Spec.Containers {
		images = append(images, container.Image)
	}
	for _, status := range pod.Status.ContainerStatuses {
		restarts += status.RestartCount
		if status.Ready {
			readyContainers++
		}
	}
	reason := pod.Status.Reason
	if reason == "" && len(pod.Status.ContainerStatuses) > 0 {
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
				reason = status.State.Waiting.Reason
				break
			}
			if status.State.Terminated != nil && status.State.Terminated.Reason != "" {
				reason = status.State.Terminated.Reason
				break
			}
		}
	}
	ready := "not-ready"
	if len(pod.Spec.Containers) > 0 && readyContainers == len(pod.Spec.Containers) {
		ready = "ready"
	}
	return PodSummary{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Phase:     string(pod.Status.Phase),
		Reason:    reason,
		Node:      pod.Spec.NodeName,
		Ready:     ready,
		Restarts:  restarts,
		Images:    images,
	}
}

func hpaSummary(hpa autoscalingv2.HorizontalPodAutoscaler) HPASummary {
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}
	lastScaleTime := ""
	if hpa.Status.LastScaleTime != nil {
		lastScaleTime = hpa.Status.LastScaleTime.Format(time.RFC3339)
	}
	return HPASummary{
		Name:               hpa.Name,
		Namespace:          hpa.Namespace,
		CurrentReplicas:    hpa.Status.CurrentReplicas,
		DesiredReplicas:    hpa.Status.DesiredReplicas,
		MinReplicas:        minReplicas,
		MaxReplicas:        hpa.Spec.MaxReplicas,
		LastScaleTime:      lastScaleTime,
		ScaleTargetRefKind: hpa.Spec.ScaleTargetRef.Kind,
		ScaleTargetRefName: hpa.Spec.ScaleTargetRef.Name,
	}
}

func maxReplicas(deployment *appsv1.Deployment) int32 {
	if deployment.Spec.Replicas == nil {
		return 1
	}
	return *deployment.Spec.Replicas
}
