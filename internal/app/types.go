package app

import (
	"regexp"
	"strings"
	"time"
)

type SolveRequest struct {
	RequestID         string   `json:"request_id,omitempty"`
	Prompt            string   `json:"prompt,omitempty"`
	Action            string   `json:"action,omitempty"`
	Namespace         string   `json:"namespace,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	Name              string   `json:"name,omitempty"`
	TailLines         int64    `json:"tail_lines,omitempty"`
	RelatedComponents []string `json:"related_components,omitempty"`
}

type SolveResponse struct {
	RequestID     string             `json:"request_id"`
	Action        string             `json:"action"`
	Summary       string             `json:"summary"`
	Prompt        string             `json:"prompt,omitempty"`
	Target        ResourceRef        `json:"target,omitempty"`
	Namespace     *NamespaceDetails  `json:"namespace,omitempty"`
	Pod           *PodDetails        `json:"pod,omitempty"`
	Deployment    *DeploymentDetails `json:"deployment,omitempty"`
	Service       *ServiceDetails    `json:"service,omitempty"`
	Related       []ComponentDetails `json:"related,omitempty"`
	KnowledgeBase []string           `json:"knowledge_base,omitempty"`
	Errors        []string           `json:"errors,omitempty"`
	GeneratedAt   time.Time          `json:"generated_at"`
}

type ResourceRef struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type NamespaceDetails struct {
	Name           string        `json:"name"`
	PodCount       int           `json:"pod_count"`
	NonRunningPods []PodSummary  `json:"non_running_pods,omitempty"`
	Events         []EventRecord `json:"events,omitempty"`
}

type PodDetails struct {
	Summary PodSummary    `json:"summary"`
	Logs    string        `json:"logs,omitempty"`
	Events  []EventRecord `json:"events,omitempty"`
}

type PodSummary struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Phase     string   `json:"phase,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Node      string   `json:"node,omitempty"`
	Ready     string   `json:"ready,omitempty"`
	Restarts  int32    `json:"restarts,omitempty"`
	Images    []string `json:"images,omitempty"`
}

type DeploymentDetails struct {
	Name            string        `json:"name"`
	Namespace       string        `json:"namespace"`
	Replicas        int32         `json:"replicas"`
	ReadyReplicas   int32         `json:"ready_replicas"`
	UpdatedReplicas int32         `json:"updated_replicas"`
	Available       bool          `json:"available"`
	Pods            []PodSummary  `json:"pods,omitempty"`
	Events          []EventRecord `json:"events,omitempty"`
}

type ServiceDetails struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	ClusterIP string            `json:"cluster_ip,omitempty"`
	Selector  map[string]string `json:"selector,omitempty"`
	Endpoints []string          `json:"endpoints,omitempty"`
	Pods      []PodSummary      `json:"pods,omitempty"`
	HPAs      []HPASummary      `json:"hpas,omitempty"`
	Events    []EventRecord     `json:"events,omitempty"`
}

type HPASummary struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	CurrentReplicas    int32  `json:"current_replicas"`
	DesiredReplicas    int32  `json:"desired_replicas"`
	MinReplicas        int32  `json:"min_replicas"`
	MaxReplicas        int32  `json:"max_replicas"`
	LastScaleTime      string `json:"last_scale_time,omitempty"`
	ScaleTargetRefKind string `json:"scale_target_ref_kind,omitempty"`
	ScaleTargetRefName string `json:"scale_target_ref_name,omitempty"`
}

type ComponentDetails struct {
	Component string       `json:"component"`
	Namespace string       `json:"namespace"`
	Pods      []PodSummary `json:"pods,omitempty"`
	Notes     []string     `json:"notes,omitempty"`
}

type EventRecord struct {
	Type          string `json:"type,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Message       string `json:"message,omitempty"`
	LastTimestamp string `json:"last_timestamp,omitempty"`
	Count         int32  `json:"count,omitempty"`
}

var (
	namespacePattern  = regexp.MustCompile(`\b(?:namespace|ns)\s+([a-z0-9-]+)`)
	podPattern        = regexp.MustCompile(`\b(?:pod|po|por)\s+([a-z0-9-]+)`)
	deploymentPattern = regexp.MustCompile(`\b(?:deployment|deploy)\s+([a-z0-9-]+)`)
	servicePattern    = regexp.MustCompile(`\b(?:service|servico|serviço|svc)\s+([a-z0-9-]+)`)
)

func normalizeRequest(req SolveRequest, defaultTailLines int64) SolveRequest {
	if req.TailLines <= 0 {
		req.TailLines = defaultTailLines
	}
	if req.TailLines > 2000 {
		req.TailLines = 2000
	}

	prompt := normalizeText(req.Prompt)
	if req.Namespace == "" {
		req.Namespace = firstCapture(namespacePattern, prompt)
	}
	if req.Name == "" {
		switch {
		case strings.Contains(prompt, "deployment") || strings.Contains(prompt, "deploy"):
			req.Name = firstCapture(deploymentPattern, prompt)
		case strings.Contains(prompt, "service") || strings.Contains(prompt, "servico") || strings.Contains(prompt, "serviço") || strings.Contains(prompt, "svc") || strings.Contains(prompt, "keda"):
			req.Name = firstCapture(servicePattern, prompt)
		default:
			req.Name = firstCapture(podPattern, prompt)
		}
	}
	if req.Action == "" {
		switch kind := strings.ToLower(req.Kind); kind {
		case "namespace":
			req.Action = "inspect_namespace"
		case "pod":
			req.Action = "inspect_pod"
		case "deployment":
			req.Action = "describe_deployment"
		case "service":
			req.Action = "inspect_service"
		}
	}
	if req.Action == "" {
		switch {
		case strings.Contains(prompt, "namespace"):
			req.Action = "inspect_namespace"
		case strings.Contains(prompt, "keda") || strings.Contains(prompt, "service") || strings.Contains(prompt, "servico") || strings.Contains(prompt, "serviço") || strings.Contains(prompt, "escalando") || strings.Contains(prompt, "scaling"):
			req.Action = "inspect_service"
		case strings.Contains(prompt, "deployment") || strings.Contains(prompt, "deploy"):
			req.Action = "describe_deployment"
		case strings.Contains(prompt, "pod") || strings.Contains(prompt, "running") || strings.Contains(prompt, "crashloop"):
			req.Action = "inspect_pod"
		default:
			req.Action = "solve_issue"
		}
	}
	if req.Kind == "" {
		switch req.Action {
		case "inspect_namespace":
			req.Kind = "namespace"
		case "inspect_pod":
			req.Kind = "pod"
		case "describe_deployment":
			req.Kind = "deployment"
		case "inspect_service":
			req.Kind = "service"
		}
	}
	req.RelatedComponents = appendUnique(req.RelatedComponents, inferRelatedComponents(prompt, req.Name)...)
	return req
}

func inferRelatedComponents(prompt, name string) []string {
	combined := normalizeText(strings.Join([]string{prompt, name}, " "))
	components := make([]string, 0, 4)
	for _, component := range []string{"keda", "calico", "istio", "nginx"} {
		if strings.Contains(combined, component) {
			components = append(components, component)
		}
	}
	return components
}

func appendUnique(values []string, more ...string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[strings.ToLower(value)] = struct{}{}
	}
	for _, value := range more {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "" {
			continue
		}
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		values = append(values, lower)
	}
	return values
}

func normalizeText(value string) string {
	replacer := strings.NewReplacer(
		"ã", "a",
		"á", "a",
		"à", "a",
		"â", "a",
		"é", "e",
		"ê", "e",
		"í", "i",
		"ó", "o",
		"ô", "o",
		"õ", "o",
		"ú", "u",
		"ç", "c",
	)
	return replacer.Replace(strings.ToLower(value))
}

func firstCapture(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}
