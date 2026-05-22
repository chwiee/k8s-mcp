# Autoscaling and KEDA

- Confirm the KEDA operator pods are healthy in the `keda` namespace.
- Check HorizontalPodAutoscaler status for desired replicas, metrics fetch failures, and cooldown behavior.
- Review ScaledObject trigger authentication, scaler metadata, and external metric connectivity.
- If the service is not scaling, verify the deployment target, queue lag metrics, and whether replicas are capped by maxReplicaCount.
