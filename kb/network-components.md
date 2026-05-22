# Network components

- **Calico**: review `calico-system` or `kube-system` pods for CNI startup failures and BGP errors.
- **Istio**: review `istio-system` pods, control plane revisions, and sidecar injection labels.
- **NGINX Ingress**: review `ingress-nginx` pods, leader election, admission webhooks, and upstream endpoint health.
- Check DNS, service endpoints, and network policies whenever cross-namespace communication fails.
