import type { ConfigContext, ExpoConfig } from "expo/config";

// Dynamic Expo config so release.yml can stamp versionName/versionCode from the
// git tag + run number (env injected in the android/ios prebuild jobs). The
// mere existence of this file flips release.yml's `detect` gate → the Android
// APK and iOS archive jobs activate (task T0.18 DONE criterion).
// Read CI-injected env via globalThis so this typechecks against the ESNext lib
// alone (no @types/node dependency, which would perturb global typing); Node
// supplies the real process.env at prebuild time.
const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};

export default ({ config }: ConfigContext): ExpoConfig => {
  const version = (env.APP_VERSION ?? "0.1.0").replace(/^v/, "");
  const androidVersionCode = Number(env.ANDROID_VERSION_CODE ?? "1");
  const iosBuildNumber = env.IOS_BUILD_NUMBER ?? "1";

  return {
    ...config,
    name: "WhatsApp V2",
    slug: "whatsapp-v2",
    scheme: "wav2",
    version,
    orientation: "portrait",
    userInterfaceStyle: "automatic",
    newArchEnabled: true,
    assetBundlePatterns: ["**/*"],
    ios: {
      supportsTablet: false,
      bundleIdentifier: "com.whatsappv2.app",
      buildNumber: iosBuildNumber,
      // iOS App Transport Security blocks cleartext http:// by default. Allow
      // local-network cleartext so the in-app server URL can reach a LAN dev box
      // at http://<lan-ip>:8080. Production is https:// (TLS at the ingress), so
      // this only affects the LAN-dev path. (Requires a rebuild to take effect.)
      infoPlist: {
        NSAppTransportSecurity: { NSAllowsLocalNetworking: true },
      },
    },
    android: {
      package: "com.whatsappv2.app",
      versionCode: androidVersionCode,
    },
    plugins: [
      "expo-router",
      "expo-secure-store",
      // Pin the Android Kotlin toolchain: the Compose Compiler (1.5.15) pulled
      // in by the native modules requires Kotlin 1.9.25, but Expo SDK 52
      // defaults to 1.9.24. expo-build-properties writes this into the native
      // project at prebuild so the release build compiles.
      //
      // usesCleartextTraffic: Android 9+ blocks plaintext http:// by default, so
      // a release APK can't reach a dev backend at http://<lan-ip>:8080. Allow
      // cleartext so the in-app server URL can point at a LAN dev box. Production
      // traffic terminates TLS at the ingress (https://), so this only matters
      // for the http LAN-dev path. (Requires an APK rebuild to take effect.)
      ["expo-build-properties", { android: { kotlinVersion: "1.9.25", usesCleartextTraffic: true } }],
    ],
    experiments: { typedRoutes: false },
  };
};
