#!/bin/sh
# Installs baggyd, Baggy's runtime with no window, and runs it as a service
# of the current user (docs/remote.md):
#
#   curl -fsSL https://github.com/zucchiniEvader/baggy/releases/latest/download/baggyd-install.sh | sh
#
# Options, after `sh -s --`:
#   --no-service     install the binary only
#   --uninstall      stop the service and remove the binary (the data stays)
# Environment:
#   BAGGYD_VERSION   a release such as 0.1.5 (default: the latest)
#   BAGGYD_BIN       where the binary goes (default: ~/.local/bin)
#   BAGGYD_URL       where the release's files are (tests)
set -eu

repo="zucchiniEvader/baggy"
bin_dir="${BAGGYD_BIN:-$HOME/.local/bin}"
label="io.github.zucchinievader.baggyd"
service=1
uninstall=0
for arg in "$@"; do
  case "$arg" in
    --no-service) service=0 ;;
    --uninstall) uninstall=1 ;;
    *) echo "baggyd-install: unknown option $arg" >&2; exit 2 ;;
  esac
done

say() { printf '%s\n' "$*"; }
die() { printf 'baggyd-install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "baggyd runs on macOS and Linux" ;;
esac
case "$(uname -m)" in
  arm64 | aarch64) arch=arm64 ;;
  x86_64 | amd64) arch=amd64 ;;
  *) die "no build for $(uname -m)" ;;
esac

plist="$HOME/Library/LaunchAgents/$label.plist"
unit="$HOME/.config/systemd/user/baggyd.service"

stop_service() {
  if [ "$os" = darwin ]; then
    launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
  elif command -v systemctl >/dev/null 2>&1; then
    systemctl --user disable --now baggyd.service 2>/dev/null || true
  fi
}

if [ "$uninstall" = 1 ]; then
  stop_service
  rm -f "$plist" "$unit" "$bin_dir/baggyd"
  say "baggyd removed. Its data (the chats, bots and settings) stays where it was."
  exit 0
fi

if [ -n "${BAGGYD_URL:-}" ]; then
  base="$BAGGYD_URL"
elif [ -n "${BAGGYD_VERSION:-}" ]; then
  base="https://github.com/$repo/releases/download/v${BAGGYD_VERSION#v}"
else
  base="https://github.com/$repo/releases/latest/download"
fi
asset="baggyd-$os-$arch.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading ${asset}…"
curl -fsSL "$base/$asset" -o "$tmp/$asset" || die "could not download $base/$asset"
curl -fsSL "$base/baggyd-checksums.txt" -o "$tmp/checksums.txt" || die "could not download the checksums"
want="$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | cut -d' ' -f1)"
else
  got="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"
fi
[ -n "$want" ] && [ "$want" = "$got" ] || die "the download does not match its checksum"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$bin_dir"
stop_service
install -m 755 "$tmp/baggyd" "$bin_dir/baggyd"
say "Installed $("$bin_dir/baggyd" version) to $bin_dir/baggyd"

if ! command -v tailscale >/dev/null 2>&1 && [ ! -d /Applications/Tailscale.app ]; then
  say "Tailscale was not found: baggyd listens on this machine's Tailscale address, so install it and sign in."
fi

if [ "$service" = 0 ]; then
  say "Run it with: $bin_dir/baggyd"
  exit 0
fi

if [ "$os" = darwin ]; then
  mkdir -p "$(dirname "$plist")" "$HOME/Library/Logs"
  cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$label</string>
  <key>ProgramArguments</key><array><string>$bin_dir/baggyd</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$HOME/Library/Logs/baggyd.log</string>
  <key>StandardErrorPath</key><string>$HOME/Library/Logs/baggyd.log</string>
</dict>
</plist>
EOF
  launchctl bootstrap "gui/$(id -u)" "$plist"
  log="$HOME/Library/Logs/baggyd.log"
  say "baggyd runs as a launch agent of $(id -un) (it starts at login; keep the user logged in)."
else
  command -v systemctl >/dev/null 2>&1 || die "no systemd: run $bin_dir/baggyd yourself, or pass --no-service"
  mkdir -p "$(dirname "$unit")"
  cat > "$unit" <<EOF
[Unit]
Description=Baggy runtime
After=network-online.target

[Service]
ExecStart=$bin_dir/baggyd
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now baggyd.service
  if command -v loginctl >/dev/null 2>&1 && [ "$(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null)" != yes ]; then
    loginctl enable-linger "$(id -un)" 2>/dev/null ||
      say "To keep baggyd running after you log out: sudo loginctl enable-linger $(id -un)"
  fi
  log="journalctl --user -u baggyd"
  say "baggyd runs as a systemd user service."
fi

sleep 3
say ""
if links="$("$bin_dir/baggyd" link 2>&1)"; then
  say "Paste this link in Baggy's Settings › General › Runtime on your other computer:"
  say "$links"
else
  say "baggyd has not started yet ($links). See its log: $log"
  say "Then print the link with: $bin_dir/baggyd link"
fi
