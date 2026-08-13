# Runbook — OTP request anomaly

**Alert:** `OTPRequestAnomaly` · **Severity:** ticket · **Fires:** OTP request rate > 5× the 7-day seasonal baseline (fraud / SMS-spend signal).

## What it means
OTP requests are spiking well above normal. This is primarily a **cost + fraud**
signal (bottleneck #4): SMS OTP is paid and abuse-prone. It can be an attack
(OTP pumping / SMS toll fraud) or a legitimate surge (a marketing spike).

## Impact
Runaway SMS spend, and potential provider rate-limiting/blocking that would hurt
real signups. Not a data-integrity issue.

## Diagnose
- Shape of the spike: `sum by (country,channel) (rate(otp_requests_total[5m]))`
  — concentrated on premium-rate countries/number ranges = likely toll fraud.
- Are the existing controls binding? Per-number 3/hr + 5/day, per-IP 10/day
  (§15.4). Check rejection rates: `rate(otp_rejections_total[5m])`.
- Verify-success ratio: a flood of requests with near-zero verifies = abuse.

## Mitigate
1. Tighten limits via the **flag console** (no deploy): lower per-IP/per-number
   caps, or geo-restrict OTP to expected countries.
2. Trip the **OTP spend circuit-breaker** kill-switch if spend is spiking.
3. If a specific IP range/ASN is the source, block it at the edge.
4. Confirm the SMS provider's own spend cap / velocity limits are set as a
   backstop.

## Verify recovery
Request rate returns toward baseline; verify-success ratio normal; spend curve
flattens.

## Escalate
Sustained fraud pattern or provider blocking real traffic → security on-call +
finance/owner (spend). Feeds the P4 abuse red-team review.

## Related
Alert `ops/alerts/platform.yaml` · threat-model-abuse.md §3 (T-controls) · HLD
§15.4, §20 #4 · kill-switches (core-api-lld §5).
