#!/usr/bin/env bash
# BootHub one-step installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Srikumar6529/boothub/main/install.sh | bash
set -euo pipefail

REPO="${BOOTHUB_REPO:-github.com/Srikumar6529/boothub}"
BIN_DIR="${BOOTHUB_BIN_DIR:-${GOBIN:-$HOME/go/bin}}"
SKIP_SETUP="${BOOTHUB_SKIP_SETUP:-0}"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  → %s\n' "$*"; }
warn() { printf '  ! %s\n' "$*"; }
die()  { printf 'Error: %s\n' "$*" >&2; exit 1; }

bold "BootHub installer"
echo  "Turn Boot.dev submissions into GitHub contribution greens."
echo

# --- prerequisites -----------------------------------------------------------
need_go=0
command -v go  >/dev/null 2>&1 || need_go=1
command -v git >/dev/null 2>&1 || die "git is required. Install: https://git-scm.com/"
command -v gh  >/dev/null 2>&1 || die "GitHub CLI (gh) is required. Install: https://cli.github.com/ then run: gh auth login"

if [[ "$need_go" -eq 1 ]]; then
  die "Go is required. Install from https://go.dev/dl/ (or: curl -sS https://webi.sh/golang | sh), then reopen your terminal."
fi

if ! gh auth status >/dev/null 2>&1; then
  warn "GitHub CLI is not logged in."
  info "Run: gh auth login"
  die "authenticate gh, then re-run this installer"
fi

if ! command -v bootdev >/dev/null 2>&1; then
  warn "bootdev CLI not found — BootHub needs it to submit lessons."
  info "Install: go install github.com/bootdotdev/bootdev@latest"
  info "Then:    bootdev login"
  echo
fi

# --- install binary ----------------------------------------------------------
mkdir -p "$BIN_DIR"
info "Installing boothub into $BIN_DIR"

install_ok=0
if GOBIN="$BIN_DIR" go install "${REPO}/cmd/boothub@latest" 2>/tmp/boothub-install.err; then
  install_ok=1
elif GOBIN="$BIN_DIR" go install "${REPO}/cmd/boothub@main" 2>/tmp/boothub-install.err; then
  install_ok=1
else
  # Local checkout fallback (when running ./install.sh from a clone)
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [[ -f "$SCRIPT_DIR/go.mod" ]]; then
    info "Installing from local checkout"
    (cd "$SCRIPT_DIR" && GOBIN="$BIN_DIR" go install ./cmd/boothub) && install_ok=1
  fi
fi

[[ "$install_ok" -eq 1 ]] || {
  cat /tmp/boothub-install.err 2>/dev/null || true
  die "could not install boothub. Try: go install ${REPO}/cmd/boothub@latest"
}

# --- PATH --------------------------------------------------------------------
if ! command -v boothub >/dev/null 2>&1; then
  export PATH="$PATH:$BIN_DIR"
fi

if ! command -v boothub >/dev/null 2>&1; then
  warn "boothub installed to $BIN_DIR but that directory is not on your PATH."
  echo
  echo "Add this to your shell config (~/.zshrc or ~/.bashrc), then reopen the terminal:"
  echo "  export PATH=\"\$PATH:$BIN_DIR\""
  echo
  export PATH="$PATH:$BIN_DIR"
fi

bold "Installed: $(command -v boothub)"
boothub --help >/dev/null 2>&1 || true
echo

# --- setup -------------------------------------------------------------------
if [[ "$SKIP_SETUP" == "1" ]]; then
  info "Skipping setup (BOOTHUB_SKIP_SETUP=1). Run later: boothub setup"
  exit 0
fi

bold "Launching setup…"
echo "Defaults are auto-detected from your bootdev login + OS timezone."
echo "Press Enter to accept each prompt."
echo
exec boothub setup "$@"
