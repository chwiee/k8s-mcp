# k8s-mcp

Kubernetes MCP server implemented in Go for running on EKS or any other Kubernetes provider.

## Features

- Streamable HTTP solve endpoint for requests coming from another Kubernetes cluster.
- Multiple request handling through the Go HTTP server plus per-request metrics.
- MCP-compatible JSON-RPC endpoint with multiple tools to execute the solve action.
- Prometheus `/metrics` endpoint with Datadog OpenMetrics annotations in the deployment manifest.
- Markdown knowledge base mounted from a ConfigMap and automatically reloaded without restarting the pod.
- Kubernetes diagnostics for namespaces, pods, deployments, services, pod logs, and common add-on namespaces such as Calico, KEDA, Istio, and NGINX Ingress.
- GitHub Actions pipeline to test, build, publish, and optionally deploy with Kustomize overlays.

## Endpoints

- `POST /v1/solve` – solve a request and return JSON.
- `POST /v1/solve?stream=true` – same solve flow using Server-Sent Events.
- `POST /mcp` – JSON-RPC endpoint exposing MCP-style tools.
- `GET /v1/knowledge-base` – current in-memory knowledge base documents.
- `GET /metrics` – Prometheus metrics.
- `GET /healthz` and `GET /readyz` – health checks.

## Example requests

### Solve from a natural-language prompt

```bash
curl -X POST http://localhost:8080/v1/solve \
  -H 'content-type: application/json' \
  -d '{
    "prompt": "estou com problemas no namespace payments"
  }'
```

### Solve with streaming HTTP

```bash
curl -N -X POST 'http://localhost:8080/v1/solve?stream=true' \
  -H 'content-type: application/json' \
  -d '{
    "prompt": "meu pod api-5c8c9d8b86-5s42m não está running",
    "namespace": "payments"
  }'
```

### Inspect a service that should scale with KEDA

```bash
curl -X POST http://localhost:8080/v1/solve \
  -H 'content-type: application/json' \
  -d '{
    "prompt": "meu serviço worker nao esta escalando com keda",
    "namespace": "jobs",
    "name": "worker",
    "related_components": ["keda"]
  }'
```

### MCP tool calls

Initialize:

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'
```

List tools:

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

Call `solve_issue`:

```bash
curl -X POST http://localhost:8080/mcp \
  -H 'content-type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "solve_issue",
      "arguments": {
        "prompt": "meu serviço worker nao esta escalando com keda",
        "namespace": "jobs",
        "related_components": ["keda"]
      }
    }
  }'
```

## Supported solve actions

- `inspect_namespace`
- `inspect_pod`
- `describe_deployment`
- `inspect_service`
- `solve_issue`

The server also exposes dedicated MCP tools for:

- fetching pod logs;
- describing a pod or deployment;
- inspecting common related components.

## Local run

```bash
go run .
```

Optional environment variables:

- `PORT` (default `8080`)
- `KUBECONFIG` (when not running in-cluster)
- `KB_DIR` (default `kb`)
- `KB_REFRESH_INTERVAL` (default `30s`)
- `POD_LOG_TAIL_LINES` (default `200`)

## Kubernetes deployment

Use Kustomize:

```bash
kubectl apply -k k8s/overlays/dev
```

The base manifest includes:

- service account plus read-only cluster role;
- service and deployment;
- ConfigMap-generated markdown knowledge base;
- Prometheus and Datadog annotations;
- readiness and liveness probes.

### Knowledge base ConfigMap refresh

The deployment mounts the generated ConfigMap at `/var/run/k8s-mcp/kb` and the server rescans the directory every `KB_REFRESH_INTERVAL`. Updating the ConfigMap content refreshes the in-memory KB automatically after the projected volume updates.

## GitHub Actions pipeline

Workflow file: `/home/runner/work/k8s-mcp/k8s-mcp/.github/workflows/ci-deploy.yaml`

Pipeline stages:

1. `test` – `go test ./...` and `go build ./...`
2. `publish` – build and push the container image to GHCR on non-PR runs
3. `deploy` – optional `workflow_dispatch` deployment using `kubectl` and a selected Kustomize overlay

Deployment prerequisites:

- repository packages permission enabled for GHCR pushes;
- repository or environment secret `KUBECONFIG_B64` containing a base64 kubeconfig;
- optional protected environments named `dev` and `prod`.

## Initial knowledge base contents

- common pod startup and crash errors
- namespace-wide failure patterns
- autoscaling and KEDA troubleshooting
- network component checks for Calico, Istio, and NGINX Ingress
