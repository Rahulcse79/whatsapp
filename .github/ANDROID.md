# Build & run the Android app

Two ways to get an installable APK, both built entirely on GitHub Actions (no
local Android toolchain needed).

## Option A — on-demand (recommended for testing)

1. **Actions → “Android APK” → Run workflow.**
   - For an **emulator** on the same machine as the backend, keep the defaults
     (`http://10.0.2.2:8080`). `10.0.2.2` is the emulator’s alias for your host.
   - For a **physical phone**, set the backend URL to your machine’s LAN IP,
     e.g. `http://192.168.1.42:8080` and `ws://192.168.1.42:8081/v1/ws`
     (find it with `ipconfig getifaddr en0` on macOS / `hostname -I` on Linux).
2. When the run finishes, open it and download the **`whatsapp-v2-android-apk`**
   artifact (bottom of the run summary).
3. Unzip it and install:
   ```bash
   adb install -r whatsapp-v2-<run>.apk
   ```
   or copy the `.apk` to the phone and tap it (allow *Install unknown apps* for
   your file manager / browser).

The workflow runs `assembleRelease`, which on this Expo project bundles the
JavaScript and signs with the debug keystore — so the APK is self-contained and
runs **without** a Metro dev server. No signing secrets required.

## Option B — a versioned release

Push a tag and `release.yml` builds version-stamped backend binaries + the
Android APK + an iOS archive and attaches them to a GitHub Release:

```bash
git tag v0.1.0 && git push --tags
```

For a Play-Store-signable APK, set repo variable `ANDROID_SIGNING=true` and the
secrets `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`,
`ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`.

## Run the backend it talks to

```bash
./start.sh          # infra (Docker) + migrations + the 4 Go services + web
./start.sh status   # what’s running + URLs
./start.sh down     # stop everything
```

`start.sh` runs core-api on `:8080` and ws-gateway on `:8081` — the ports the
APK’s defaults expect. If the app can’t connect:

- **Emulator:** confirm the backend is up (`./start.sh status`) and that the APK
  was built with the `10.0.2.2` defaults.
- **Phone:** it must be on the **same Wi-Fi** as your machine, the APK must be
  built with your **LAN IP**, and your firewall must allow inbound `8080`/`8081`.

## Where the URL comes from

`clients/mobile/src/services/appServices.ts` reads `EXPO_PUBLIC_API_URL` /
`EXPO_PUBLIC_WS_URL` (baked in at build time by the workflow) and falls back to
`localhost:8080` / `localhost:8081` for a local `expo start` during development.
