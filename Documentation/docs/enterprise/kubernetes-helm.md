---
id: kubernetes-helm
title: Production Kubernetes & Helm Deployment
sidebar_label: Setup B - Kubernetes (Helm)
description: Production-grade deployment on AWS EKS, Azure AKS, Google GKE, or OpenShift using Helm.
---

# Production Kubernetes & Helm Deployment

For scalable production deployments across multiple worker nodes, the appliance includes a complete production Helm chart located at `deploy/helm/loopers-firewall`.

The chart provisions:
- Deployment controllers with Horizontal Pod Autoscalers (HPA) for `loopers-engine` and `loopers-api`.
- Strict Kubernetes `NetworkPolicy` objects restricting egress to authorized model providers and internal infrastructure.
- Zero-drift Secret templates auto-generating internal secrets and AES-256 keys.
- PodDisruptionBudgets (PDB) ensuring zero-downtime rolling updates during active agent streaming sessions.

---

## 1. Quick Install with Helm

From the root of the extracted package:

```bash
# 1. Create a dedicated namespace
kubectl create namespace loopers-system

# Option A: Deploy directly from GHCR OCI Registry
helm install loopers-firewall oci://ghcr.io/cursed-me/charts/loopers-firewall \
  --namespace loopers-system \
  --set license.token="$(cat deploy/license.jwt)"

# Option B: Deploy using local extracted chart
helm install loopers-firewall ./deploy/helm/loopers-firewall \
  --namespace loopers-system \
  --set license.token="$(cat deploy/license.jwt)"

# 3. Verify pod readiness
kubectl get pods -n loopers-system -l app.kubernetes.io/name=loopers-firewall
```

---

## 2. Using Private Container Registries (Harbor, AWS ECR, ACR)

In corporate environments, images should be pulled from your internal container registry rather than built on-cluster:

```bash
helm install loopers-firewall ./deploy/helm/loopers-firewall \
  --namespace loopers-system \
  --set global.imageRegistry="myregistry.internal/loopers" \
  --set global.imagePullSecrets[0].name="regcred" \
  --set license.token="$(cat deploy/license.jwt)"
```

---

## 3. Horizontal Pod Autoscaling (HPA) Guidelines

When sizing `loopers-engine` pods for high-concurrency multi-agent streaming workloads:

```yaml
# Recommended values.yaml override
engine:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
  resources:
    requests:
      cpu: "1000m"
      memory: "1Gi"
    limits:
      cpu: "4000m"
      memory: "4Gi"
```

### Ingress & Load Balancer Tuning:
* **Algorithm:** Use **Least Connections (`least_conn`)** instead of round-robin so long-running SSE streaming sessions distribute evenly across pods.
* **Buffering:** Disable proxy response buffering (`proxy_buffering off` in NGINX or buffer disabling on AWS ALB) so tokens stream to agents without artificial delay.
* **Idle Timeout:** Set load balancer idle timeouts to at least **90 seconds** to accommodate complex multi-step tool calls.
