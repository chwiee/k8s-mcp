# Common pod errors

- **CrashLoopBackOff**: inspect container logs, last termination reason, probes, and missing environment variables.
- **ImagePullBackOff**: validate image name, registry credentials, imagePullSecrets, and network egress to the registry.
- **Pending**: check node pressure, resource requests, taints, tolerations, PVC binding, and cluster autoscaler capacity.
- **OOMKilled**: compare memory limit versus live usage and increase the limit or reduce heap pressure.
