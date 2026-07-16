# ADR-002: Go Backend

Status: **Accepted** (reaffirmed 2026-07-16 against a Java/Spring proposal) · Upstream: HLD §1.1, §6

## Context

The dominant workload is protocol plumbing: tens of thousands of long-lived WebSockets, message fan-out, backpressure, small hot transactions — not enterprise CRUD. A Spring Boot stack was proposed (2026-07-16) and evaluated.

## Decision

**Go 1.24+** for all five deployables.

## Alternatives

- **Java 21 + Spring Boot:** goroutines cost ~4–8 KB vs JVM per-connection overhead (one Go pod holds all 20k conns in ~1.3 GB); ~15 MB distroless images vs 200+ MB; ms cold starts (HPA bursts); no heap/GC tuning on-call burden; NATS/LiveKit/coturn/K8s are Go-native — one pprof-shaped toolchain end to end. Spring's strengths (integration ecosystem, JPA) buy nothing for a ciphertext relay. Rejected.
- **Node/TypeScript:** weaker CPU-bound performance and type safety for binary protocol code. Rejected.
- **Elixir/BEAM:** excellent architectural fit for connection fan-out; niche hiring pool decided against it. Rejected with respect.

## Consequences

- ✅ Density, tiny images, single toolchain; mainstream hiring.
- ⚠️ Team must hold the line on Go discipline (contexts as packages, no framework magic) — see coding-standards.md.
- Revisit trigger: none foreseeable at any tier of the scale ladder.
