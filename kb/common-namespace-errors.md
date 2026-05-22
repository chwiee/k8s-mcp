# Common namespace errors

- Review pods that are not `Running` or not `Ready`.
- Review warning events for quota, denied admission webhook requests, failed mounts, and image pull errors.
- Confirm network policies and service accounts created in the namespace.
- Validate ConfigMaps, Secrets, and projected volumes when many workloads fail at the same time.
