# deploy/

No manifests yet (blueprint phase).

| Dir | Will contain | Blueprint |
|---|---|---|
| `compose/` | Full dev stack on a laptop / single-box pilot (incl. offline profile services: ntfy, step-ca) | [Docs/08-devops/offline-local-server.md](../Docs/08-devops/offline-local-server.md) §5 |
| `helm/` | Charts per deployable + umbrella; one chart set for **all** profiles (values overlays only) | [Docs/08-devops/kubernetes-deployment.md](../Docs/08-devops/kubernetes-deployment.md) |
| `argocd/` | App-of-apps, env overlays dev/staging/prod/offline | same |
| `terraform/` | Cluster, node pools, DNS, buckets, secrets bootstrap | same §6 |

First tasks: T0.03 (compose), T0.22 (K8s) in [task-breakdown.md](../Docs/12-planning/task-breakdown.md).
