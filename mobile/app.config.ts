import type { ExpoConfig } from "expo/config";

// Baggy on the phone: a companion of the desktop app, reaching it over the local network
// (docs/mobile.md).
export default (): ExpoConfig => ({
  name: "Baggy",
  slug: "baggy",
  version: "0.1.0",
  scheme: "baggy",
  orientation: "portrait",
  icon: "./assets/icon.png",
  userInterfaceStyle: "automatic",
  ios: {
    bundleIdentifier: "io.github.zucchinievader.baggy.mobile",
    supportsTablet: true,
    infoPlist: {
      NSCameraUsageDescription: "Baggy scans the pairing code your computer shows.",
      NSLocalNetworkUsageDescription: "Baggy reaches the Baggy app on your computer over the local network.",
      // The desktop serves plain HTTP at a local address; ATS leaves IP addresses alone.
      NSAppTransportSecurity: { NSAllowsLocalNetworking: true },
      ITSAppUsesNonExemptEncryption: false,
    },
  },
  plugins: [
    "expo-router",
    "expo-secure-store",
    "expo-image",
    "expo-localization",
    ["expo-camera", { cameraPermission: "Baggy scans the pairing code your computer shows." }],
  ],
  experiments: { typedRoutes: true },
});
