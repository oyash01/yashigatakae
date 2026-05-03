#!/usr/bin/env sh
# yashigatakae installer (Mac/Linux)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
#
# Env overrides:
#   YASHI_VERSION=v0.1.0          # specific version (default: latest)
#   YASHI_PREFIX=$HOME/.local/bin # install dir (default: $HOME/.local/bin)
#   YASHI_NO_INIT=1               # skip the auto-`yashigatakae init` at the end

set -eu

REPO="oyash01/yashigatakae"
VERSION="${YASHI_VERSION:-latest}"
PREFIX="${YASHI_PREFIX:-$HOME/.local/bin}"

err()  { printf "✗ %s\n" "$*" >&2; exit 1; }
info() { printf "  · %s\n" "$*"; }
ok()   { printf "✓ %s\n" "$*"; }

# ── detect platform ────────────────────────────────────────────────
uname_s=$(uname -s 2>/dev/null || echo unknown)
uname_m=$(uname -m 2>/dev/null || echo unknown)
case "$uname_s" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *)      err "unsupported OS: $uname_s (only macOS and Linux from this script)" ;;
esac
case "$uname_m" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=amd64 ;;
  *)             err "unsupported arch: $uname_m" ;;
esac
asset="yashigatakae-${os}-${arch}.tar.gz"

# ── prerequisites ──────────────────────────────────────────────────
for c in curl tar; do
  command -v "$c" >/dev/null 2>&1 || err "$c not found in PATH"
done

# ── resolve version ────────────────────────────────────────────────
if [ "$VERSION" = "latest" ]; then
  info "resolving latest release..."
  api="https://api.github.com/repos/${REPO}/releases/latest"
  VERSION=$(curl -fsSL "$api" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)
  if [ -z "$VERSION" ]; then
    err "no published release yet — set YASHI_VERSION explicitly or build from source: \`go install github.com/${REPO}/cmd/yashigatakae@latest\`"
  fi
fi
info "version: $VERSION"
info "asset:   $asset"

# ── download ───────────────────────────────────────────────────────
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
info "downloading $url"
curl -fsSL -o "$tmp/$asset" "$url" || err "download failed"

# ── extract ────────────────────────────────────────────────────────
info "extracting"
tar -xzf "$tmp/$asset" -C "$tmp"
[ -x "$tmp/yashigatakae" ] || err "binary missing from archive"

# ── install ────────────────────────────────────────────────────────
mkdir -p "$PREFIX"
install -m 0755 "$tmp/yashigatakae" "$PREFIX/yashigatakae"
ok "installed to $PREFIX/yashigatakae"

# ── PATH check ─────────────────────────────────────────────────────
case ":$PATH:" in
  *":$PREFIX:"*) : ;;
  *) info "add to PATH: export PATH=\"$PREFIX:\$PATH\"" ;;
esac

# ── version sanity ─────────────────────────────────────────────────
"$PREFIX/yashigatakae" --version || err "binary failed to run"

# ── done ───────────────────────────────────────────────────────────
# We deliberately do NOT auto-run `yashigatakae init` here. `init` is now
# an interactive Bubble Tea wizard, and `curl | sh` runs without a TTY,
# so the wizard couldn't render anyway. Tell the user to launch it
# themselves and they get the proper interactive setup.
echo
ok "yashigatakae installed"
echo
echo "Next steps:"
echo "  1. Run the interactive setup:   yashigatakae init"
echo "     (or skip the wizard with:    yashigatakae init -y )"
echo "  2. Verify everything is wired:  yashigatakae doctor"
echo "  3. Build the wiki for a repo:   cd <repo> && yashigatakae graphify . --pro"
echo "  4. Open Claude Code — caveman + mempalace + bifrost are auto-loaded."
echo
case ":$PATH:" in
  *":$PREFIX:"*) : ;;
  *) echo "(${PREFIX} is not on your PATH; add it to your shell rc and re-source)" ;;
esac
