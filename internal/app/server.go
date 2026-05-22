package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	cfg       Config
	kb        *KBStore
	inspector Inspector
	logger    *slog.Logger
}

func NewServer(cfg Config, kb *KBStore, inspector Inspector, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, kb: kb, inspector: inspector, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/v1/knowledge-base", s.handleKnowledgeBase)
	mux.HandleFunc("/v1/solve", s.handleSolve)
	mux.HandleFunc("/mcp", s.handleMCP)
	return mux
}

func (s *Server) handleKnowledgeBase(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"documents": s.kb.Snapshot()})
}

func (s *Server) handleSolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s is not allowed", r.Method))
		return
	}
	var req SolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode solve request: %w", err))
		return
	}
	req = normalizeRequest(req, s.cfg.PodLogTailLines)
	req.RequestID = ensureRequestID(req.RequestID)
	if wantsStream(r) {
		s.streamSolve(w, r, req)
		return
	}
	response := s.solve(r.Context(), req)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) streamSolve(w http.ResponseWriter, r *http.Request, req SolveRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming is not supported by this server"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	writeSSE(w, "status", map[string]any{"request_id": req.RequestID, "phase": "accepted", "action": req.Action})
	flusher.Flush()
	response := s.solve(r.Context(), req)
	writeSSE(w, "result", response)
	flusher.Flush()
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s is not allowed", r.Method))
		return
	}
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode mcp request: %w", err))
		return
	}

	result, err := s.mcpResult(r.Context(), request.Method, request.Params)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      decodeRawID(request.ID),
			"error": map[string]any{
				"code":    -32602,
				"message": err.Error(),
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": decodeRawID(request.ID), "result": result})
}

func (s *Server) mcpResult(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return map[string]any{
			"serverInfo":   map[string]any{"name": "k8s-mcp", "version": "0.1.0"},
			"capabilities": map[string]any{"tools": map[string]any{}},
		}, nil
	case "tools/list":
		return map[string]any{"tools": []map[string]any{
			{
				"name":        "solve_issue",
				"description": "Diagnose namespace, pod, deployment, service, or generic Kubernetes issues.",
				"inputSchema": solveRequestSchema(),
			},
			{
				"name":        "get_pod_logs",
				"description": "Fetch recent logs for a pod.",
				"inputSchema": map[string]any{"type": "object", "required": []string{"namespace", "name"}},
			},
			{
				"name":        "describe_resource",
				"description": "Describe a Kubernetes pod or deployment.",
				"inputSchema": map[string]any{"type": "object", "required": []string{"namespace", "kind", "name"}},
			},
			{
				"name":        "inspect_related_components",
				"description": "Inspect common add-on namespaces like calico, keda, istio, or nginx.",
				"inputSchema": map[string]any{"type": "object", "required": []string{"components"}},
			},
		}}, nil
	case "tools/call":
		var request struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("decode tool call parameters: %w", err)
		}
		return s.callTool(ctx, request.Name, request.Arguments)
	default:
		return nil, fmt.Errorf("unsupported method %q", method)
	}
}

func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) (any, error) {
	switch name {
	case "solve_issue":
		var req SolveRequest
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("decode solve_issue arguments: %w", err)
		}
		response := s.solve(ctx, normalizeRequest(req, s.cfg.PodLogTailLines))
		return toolResult(response.Summary, response), nil
	case "get_pod_logs":
		var req SolveRequest
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("decode get_pod_logs arguments: %w", err)
		}
		pod, err := s.inspector.InspectPod(ctx, req.Namespace, req.Name, normalizeRequest(req, s.cfg.PodLogTailLines).TailLines)
		if err != nil {
			return nil, err
		}
		return toolResult("Fetched pod logs.", pod), nil
	case "describe_resource":
		var req SolveRequest
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("decode describe_resource arguments: %w", err)
		}
		switch strings.ToLower(req.Kind) {
		case "pod":
			pod, err := s.inspector.InspectPod(ctx, req.Namespace, req.Name, normalizeRequest(req, s.cfg.PodLogTailLines).TailLines)
			if err != nil {
				return nil, err
			}
			return toolResult("Described pod.", pod), nil
		case "deployment":
			deployment, err := s.inspector.InspectDeployment(ctx, req.Namespace, req.Name)
			if err != nil {
				return nil, err
			}
			return toolResult("Described deployment.", deployment), nil
		default:
			return nil, fmt.Errorf("kind %q is not supported by describe_resource", req.Kind)
		}
	case "inspect_related_components":
		var req struct {
			Components []string `json:"components"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("decode inspect_related_components arguments: %w", err)
		}
		components, err := s.inspector.InspectComponents(ctx, req.Components)
		if err != nil {
			return nil, err
		}
		return toolResult("Inspected related components.", components), nil
	default:
		return nil, fmt.Errorf("unsupported tool %q", name)
	}
}

func (s *Server) solve(ctx context.Context, req SolveRequest) SolveResponse {
	start := time.Now()
	activeRequests.Inc()
	defer activeRequests.Dec()
	defer requestDuration.WithLabelValues(req.Action).Observe(time.Since(start).Seconds())

	response := SolveResponse{
		RequestID:   req.RequestID,
		Action:      req.Action,
		Prompt:      req.Prompt,
		GeneratedAt: time.Now().UTC(),
		Target:      ResourceRef{Kind: req.Kind, Namespace: req.Namespace, Name: req.Name},
	}
	response.RequestID = ensureRequestID(response.RequestID)

	var err error
	keywords := append([]string{req.Action, req.Namespace, req.Kind, req.Name}, req.RelatedComponents...)
	if req.Prompt != "" {
		keywords = append(keywords, req.Prompt)
	}
	response.KnowledgeBase = s.kb.Search(keywords)

	switch req.Action {
	case "inspect_namespace":
		response.Namespace, err = s.inspector.InspectNamespace(ctx, req.Namespace)
		response.Summary = fmt.Sprintf("Inspected namespace %s and summarized pod health plus recent events.", req.Namespace)
	case "inspect_pod":
		response.Pod, err = s.inspector.InspectPod(ctx, req.Namespace, req.Name, req.TailLines)
		response.Summary = fmt.Sprintf("Described pod %s/%s and collected recent logs.", req.Namespace, req.Name)
	case "describe_deployment":
		response.Deployment, err = s.inspector.InspectDeployment(ctx, req.Namespace, req.Name)
		response.Summary = fmt.Sprintf("Described deployment %s/%s and listed its pods.", req.Namespace, req.Name)
	case "inspect_service":
		response.Service, err = s.inspector.InspectService(ctx, req.Namespace, req.Name)
		response.Summary = fmt.Sprintf("Inspected service %s/%s, endpoints, and namespace autoscalers.", req.Namespace, req.Name)
	default:
		response.Summary = "Solved the request using the knowledge base and optional related component inspection."
	}
	if err != nil {
		response.Errors = append(response.Errors, err.Error())
	}
	if len(req.RelatedComponents) > 0 {
		components, componentErr := s.inspector.InspectComponents(ctx, req.RelatedComponents)
		if componentErr != nil {
			response.Errors = append(response.Errors, componentErr.Error())
		} else {
			response.Related = components
		}
	}
	status := "success"
	if len(response.Errors) > 0 {
		status = "partial"
	}
	requestsTotal.WithLabelValues(req.Action, status).Inc()
	return response
}

func ensureRequestID(value string) string {
	if value != "" {
		return value
	}
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func solveRequestSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":             map[string]any{"type": "string"},
			"action":             map[string]any{"type": "string"},
			"namespace":          map[string]any{"type": "string"},
			"kind":               map[string]any{"type": "string"},
			"name":               map[string]any{"type": "string"},
			"tail_lines":         map[string]any{"type": "integer"},
			"related_components": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
}

func toolResult(message string, value any) map[string]any {
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": message}},
		"structuredContent": value,
	}
}

func wantsStream(r *http.Request) bool {
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		return true
	}
	return r.URL.Query().Get("stream") == "true"
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	encoded, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func decodeRawID(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}
