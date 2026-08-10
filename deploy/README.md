# deploy/

Infrastructure-as-code. Nothing is applied from developer machines — GitOps
(ArgoCD) is the only writer to clusters.

| Dir | Contains | Blueprint |
|---|---|---|
| `compose/` | Full dev stack on a laptop / single-box pilot (incl. offline: ntfy, step-ca) | [offline-local-server.md](../Docs/08-devops/offline-local-server.md) §5 |
| `helm/` | `wa-service` (reusable chart) + `wa-platform` (umbrella, one aliased instance per deployable); one chart set for **all** profiles, values overlays only | [kubernetes-deployment.md](../Docs/08-devops/kubernetes-deployment.md) |
| `argocd/` | App-of-apps root + ApplicationSet (per-env `wa-platform` Applications) | same |
| `terraform/` | Cluster, node pools, DNS, buckets, secrets bootstrap | same §6 |

## Helm (`helm/`)

- **`wa-service`** — the deployable template: an **Argo Rollout** (canary), Service,
  ConfigMap (WA_* config via `envFrom`), HPA, PodDisruptionBudget, default-deny
  NetworkPolicy, ServiceAccount, soft pod anti-affinity. Secrets come from a
  pre-created (SOPS) Secret named in `envFromSecret`.
- **`wa-platform`** — umbrella depending on `wa-service` aliased ×4
  (core-api, ws-gateway, media-svc, notification-svc) + platform bootstrap:
  a **DB role/grant Job** (pre-install hook) and, in the offline overlay, a
  **step-ca** StatefulSet + cert-manager ClusterIssuer.
- Overlays: `values-dev.yaml`, `values-staging.yaml`, `values-prod.yaml`,
  `values-offline.yaml`. ws-gateway carries the **drain** hook (preStop → `/drain`,
  `maxUnavailable: 0`, long grace).

Render locally (mirrors the CI `deploy (helm)` job):

```bash
helm dependency update deploy/helm/wa-platform
helm template wa-prod deploy/helm/wa-platform \
  -f deploy/helm/wa-platform/values.yaml \
  -f deploy/helm/wa-platform/values-prod.yaml
```

## ArgoCD (`argocd/`)

Bootstrap once, then GitOps takes over:

```bash
kubectl apply -f deploy/argocd/appproject.yaml -f deploy/argocd/root-app.yaml
```

The root Application (app-of-apps) syncs `argocd/apps/`, whose ApplicationSet
generates one `wa-platform-<env>` Application per environment. Set `repoURL` in
both files to your GitOps repository first.
