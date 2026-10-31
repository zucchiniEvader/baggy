// The iOS 27 SDK asserts at launch unless the app adopts the scene-based life cycle. SDK 58's
// template does this; SDK 57's does not, though its `expo` package already has the scene delegate.
// This brings SDK 58's shape to prebuild: a SceneDelegate that creates the window and starts
// React Native, the scene manifest in Info.plist, and an AppDelegate that leaves the window to it.
const fs = require("fs");
const path = require("path");
const { IOSConfig, withAppDelegate, withDangerousMod, withInfoPlist, withXcodeProject } = require("expo/config-plugins");

const sceneDelegate = `internal import Expo

@objc(SceneDelegate)
class SceneDelegate: ExpoAppSceneDelegate {
  // Extension point for config plugins.
}
`;

const windowBlock = /\n#if os\(iOS\) \|\| os\(tvOS\)\n {4}window = UIWindow\(frame: UIScreen\.main\.bounds\)[\s\S]*?#endif\n/;

module.exports = function withSceneDelegate(config) {
  config = withAppDelegate(config, (c) => {
    let src = c.modResults.contents;
    if (!src.includes("ExpoReactNativeFactoryProvider")) {
      src = src.replace("class AppDelegate: ExpoAppDelegate {", "class AppDelegate: ExpoAppDelegate, ExpoReactNativeFactoryProvider {");
    }
    src = src.replace(
      windowBlock,
      "\n    // The window is created and React Native is started by `SceneDelegate` under the\n    // scene-based life cycle (required by the iOS 27 SDK).\n",
    );
    if (!src.includes("ExpoReactNativeFactoryProvider") || windowBlock.test(src)) {
      throw new Error("with-scene-delegate: AppDelegate.swift is not the shape it expects");
    }
    c.modResults.contents = src;
    return c;
  });

  config = withInfoPlist(config, (c) => {
    c.modResults.UIApplicationSceneManifest = {
      UIApplicationSupportsMultipleScenes: false,
      UISceneConfigurations: {
        UIWindowSceneSessionRoleApplication: [
          { UISceneConfigurationName: "Default Configuration", UISceneDelegateClassName: "$(PRODUCT_MODULE_NAME).SceneDelegate" },
        ],
      },
    };
    return c;
  });

  config = withDangerousMod(config, [
    "ios",
    (c) => {
      const name = IOSConfig.XcodeUtils.getProjectName(c.modRequest.projectRoot);
      fs.writeFileSync(path.join(c.modRequest.platformProjectRoot, name, "SceneDelegate.swift"), sceneDelegate);
      return c;
    },
  ]);

  return withXcodeProject(config, (c) => {
    const name = IOSConfig.XcodeUtils.getProjectName(c.modRequest.projectRoot);
    const filepath = `${name}/SceneDelegate.swift`;
    if (!c.modResults.hasFile(filepath)) {
      IOSConfig.XcodeUtils.addBuildSourceFileToGroup({ filepath, groupName: name, project: c.modResults });
    }
    return c;
  });
};
