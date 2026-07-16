# CI/CD — GitHub Actions

**Policy: nothing builds or runs on developer machines.** Every check, build,
and release happens here. Pipeline design: [Docs/08-devops/ci-cd.md](../Docs/08-devops/ci-cd.md).

## Workflows

| Workflow | Trigger | Does |
|---|---|---|
| [ci.yml](workflows/ci.yml) | every push to `main` + every PR | backend: `go build` / `go vet` / `go test -race` / golangci-lint · proto: `buf lint` + breaking-change gate (PRs) + codegen proof (artifact) · clients: typecheck + tests |
| [release.yml](workflows/release.yml) | pushing a tag `v*` | version-stamped backend binaries (linux amd64/arm64) · Android APK · iOS archive · GitHub Release with all artifacts attached |

## Versioning

- Source of truth for the next version: [`/VERSION`](../VERSION) (semver).
- **Releasing:** `git tag v0.1.0 && git push --tags` — the tag is the released version.
- Backend binaries are stamped via `-ldflags "-X main.version=… -X main.commit=…"`.
- Mobile: `versionName` = tag, `versionCode` / iOS build number = GitHub run number.
- Artifact filenames: `whatsapp-v2-<component>-<version>-<os>-<arch>[.ext]`, e.g.
  `whatsapp-v2-core-api-0.1.0-linux-amd64`, `whatsapp-v2-0.1.0-android.apk`.

## App builds

The Android/iOS jobs **skip automatically** until the Expo app exists at
`clients/mobile` (task T0.18) — the `detect` job checks for `app.json`.

### Secrets for store-ready builds (optional until launch)

| Secret / variable | Purpose |
|---|---|
| repo variable `ANDROID_SIGNING=true` | enables release-keystore signing |
| `ANDROID_KEYSTORE_BASE64` | keystore file, base64-encoded |
| `ANDROID_KEYSTORE_PASSWORD` / `ANDROID_KEY_ALIAS` / `ANDROID_KEY_PASSWORD` | keystore credentials |
| (iOS, later) Apple cert + provisioning-profile secrets | signed IPA / TestFlight — until then iOS produces an **unsigned** archive proving the build |

Set these in GitHub → Settings → Secrets and variables → Actions. Never commit
keystores or Apple certs — `.gitignore` already blocks `*.p8`/`*.pem`.

## Offline profile note

On the self-hosted/air-gapped deployment (HLD §17.5) these same workflows run
on Gitea Actions with a Harbor registry — the YAML is kept actions-compatible.
