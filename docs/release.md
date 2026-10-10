# Releases, updates and the website

## Building a release

`.github/workflows/release.yml` runs when a tag `v*` is pushed (or by hand, naming an existing tag). It checks that the tag is `v` and `desktop/mygo.json`'s `version`, creates a draft release with GitHub's notes, then builds on three runners at once with `go tool mygo build -platform … -upload`:

| Row | Runner | Targets | Files |
| --- | --- | --- | --- |
| macOS | macos-latest | `darwin/universal` (Apple silicon and Intel in one app) | `Baggy <version>.dmg`, the update archive and manifest |
| Windows | ubuntu-latest, with NSIS | `windows/amd64` (Arm runs it emulated; two Windows targets would give installers of the same name) | `Baggy Setup <version>.exe`, the update |
| Linux | ubuntu-latest | `linux/amd64`, `linux/arm64` | the archives, `install.sh`, the Debian packages, the updates |

The draft is published by hand after a look; publishing makes it the latest release, which apps update to.

To release: bump `version` in `desktop/mygo.json`, add its `## <version>` section to `desktop/CHANGELOG.md` (the update window shows it; the build fails without it), commit, and push the tag:

```sh
git tag v0.1.1 && git push origin v0.1.1
```

`.github/workflows/ci.yml` vets and tests both modules on every push to `main` and every pull request, on macOS, and checks that Windows and Linux compile.

## Signing

- **Updates** are signed with the key in the `MYGO_UPDATER_PRIVATE_KEY` secret, whose public half is `updates.publicKey` in `mygo.json`; installed apps take only updates it signed. The key was made with `mygo keygen -o ~/Library/Application\ Support/mygo/update-keys/baggy` and lives there and in the secret only, never in the repository. Losing it strands the installed apps.
- **macOS**: with the secrets `MACOS_CERTIFICATE` (the Developer ID Application certificate and its key, a base64 `.p12`), `MACOS_CERTIFICATE_PASSWORD`, `APPLE_ID`, `APPLE_TEAM_ID` and `APPLE_APP_PASSWORD` (an app-specific password), the macOS row imports the certificate into a keychain of its own, stores the notary credentials as the profile `notary`, and writes `macos.signingIdentity` (the one Developer ID identity in that keychain) and `macos.notarize` into `mygo.json` for the build, which signs, notarizes and staples. Without them it warns and builds an app signed ad hoc, which Gatekeeper blocks on other Macs. The repository's `mygo.json` has no signing fields, so builds on a Mac without the notary profile work.
- **Windows** builds are not signed (no certificate yet): SmartScreen warns on first launch.

## Updates in the app

`desktop/main.go` uses MyGo's native update window (`native.New(updater.Options{Language})`, the app's language when one is chosen): it checks the latest release's manifest for this platform once a day, shows the new version's notes, and installs on the user's word, relaunching into it. The app menu has Check for Updates…, and Settings' About shows the version, a switch for the daily check, and Check for Updates…, when the app can update itself (`mygo.Updater.Enabled`: a release build installed where the user can write; never `mygo dev`, nor the Debian package, which apt updates).

## The website

`site/` is the landing page, plain HTML with its icon and screenshots, which `.github/workflows/pages.yml` publishes to GitHub Pages (https://zucchinievader.github.io/baggy/) when it changes. Its download links come from the latest release's files through the GitHub API, the button picking the visitor's system; without a release they point to the releases page. `BAGGY_SITE=../site go test -run TestSiteScreenshots` in `desktop/` renders the screenshots again, light and dark at twice their size.
