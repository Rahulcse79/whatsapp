# Runbook — Certificate expiry

**Alerts:** `CertExpiringTicket` (< 14 d, ticket) · `CertExpiringPage` (< 3 d, page).

## What it means
A TLS certificate (ingress/edge, internal mTLS, or the offline step-ca chain) is
approaching expiry. At < 3 days this pages — an expired cert is a hard outage
(clients refuse the connection).

## Impact
On expiry: TLS handshakes fail → total outage for the affected surface (public
edge, or internal service-to-service if mTLS). The offline profile's private-CA
chain expiring breaks the whole LAN deployment.

## Diagnose
- Which cert? The alert labels the secret/issuer. `kubectl get certificate -A`
  (cert-manager) → look for `Ready=False` / near `notAfter`.
- Renewal blocked? `kubectl describe certificate <name>` + cert-manager logs —
  ACME/DNS challenge failing, or the offline step-ca issuer down.
- Offline profile: is the step-ca `StatefulSet` healthy and the ClusterIssuer
  reachable?

## Mitigate
1. **cert-manager should auto-renew.** If it hasn't, force it:
   `kubectl cert-manager renew <name>` (or delete the CertificateRequest to retrigger).
2. If the ACME/DNS challenge is failing: fix the challenge path (DNS record,
   solver config), then renew.
3. Offline profile: ensure step-ca is up and the intermediate is valid; rotate
   per the CA-rotation runbook if the intermediate itself expired.
4. Last resort under time pressure: issue/upload a valid cert manually to the
   TLS secret to stop the bleeding, then fix the automation.

## Verify recovery
`kubectl get certificate` shows a fresh `notAfter`; the edge serves the new cert
(`echo | openssl s_client -connect host:443 | openssl x509 -noout -dates`).

## Escalate
< 24 h to expiry with renewal still failing → page on-call lead immediately.

## Related
Alert `ops/alerts/platform.yaml` · security-architecture.md · deploy step-ca
(T0.22, offline) · HLD §17.5.
