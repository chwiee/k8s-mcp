package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeInspector struct{}

func (fakeInspector) InspectNamespace(context.Context, string) (*NamespaceDetails, error) {
	return &NamespaceDetails{Name: "payments", PodCount: 2}, nil
}

func (fakeInspector) InspectPod(context.Context, string, string, int64) (*PodDetails, error) {
	return &PodDetails{Summary: PodSummary{Name: "api-123", Namespace: "payments", Phase: "Running", Ready: "ready"}, Logs: "ok"}, nil
}

func (fakeInspector) InspectDeployment(context.Context, string, string) (*DeploymentDetails, error) {
	return &DeploymentDetails{Name: "api", Namespace: "payments", Replicas: 2, ReadyReplicas: 2, UpdatedReplicas: 2, Available: true}, nil
}

func (fakeInspector) InspectService(context.Context, string, string) (*ServiceDetails, error) {
	return &ServiceDetails{Name: "api", Namespace: "payments", Type: "ClusterIP", Endpoints: []string{"10.0.0.1:8080"}}, nil
}

func (fakeInspector) InspectComponents(context.Context, []string) ([]ComponentDetails, error) {
	return []ComponentDetails{{Component: "keda", Namespace: "keda"}}, nil
}

func TestSolveEndpointInfersNamespaceAction(t *testing.T) {
	t.Parallel()
	kb, err := NewKBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewKBStore() error = %v", err)
	}
	server := NewServer(Config{PodLogTailLines: 100}, kb, fakeInspector{}, slog.Default())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/solve", strings.NewReader(`{"prompt":"estou com problemas no namespace payments"}`))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response SolveResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.Action != "inspect_namespace" {
		t.Fatalf("action = %q, want %q", response.Action, "inspect_namespace")
	}
	if response.Target.Namespace != "payments" {
		t.Fatalf("namespace = %q, want %q", response.Target.Namespace, "payments")
	}
	if response.Namespace == nil || response.Namespace.Name != "payments" {
		t.Fatalf("expected namespace details in response, got %+v", response.Namespace)
	}
}

func TestSolveEndpointStreamsResults(t *testing.T) {
	t.Parallel()
	kb, err := NewKBStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewKBStore() error = %v", err)
	}
	server := NewServer(Config{PodLogTailLines: 100}, kb, fakeInspector{}, slog.Default())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/solve?stream=true", strings.NewReader(`{"namespace":"payments","kind":"pod","name":"api-123"}`))

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if !strings.Contains(string(body), "event: result") {
		t.Fatalf("stream response missing result event: %s", string(body))
	}
}
