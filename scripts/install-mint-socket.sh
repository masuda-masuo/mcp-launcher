#!/usr/bin/env bash
# install-mint-socket.sh -- installs the on-demand mcp-token mint socket
# (systemd/mcp-token.socket + systemd/mcp-token@.service) into systemd
# user scope and enables it. See docs/architecture/mint-socket.md for
# the design and security boundary.
#
# Usage:
#   scripts/install-mint-socket.sh [--bin PATH] [--config PATH]
#
# Must be run from a checkout of this repo, or from the mint-socket kit
# published with every mcp-token release
# (mcp-token-mint-socket-<version>.tar.gz, which unpacks to the same
# scripts/ + systemd/ layout and is covered by the release's
# checksums.txt). The unit files are copied rather than embedded, so a
# plain `curl | bash` of this script alone will not work -- fetch the
# kit instead. Installing from the kit needs no Go toolchain and no
# checkout: the mcp-token binary itself is downloaded from the same
# release (resolution step 5 below).
#
# mcp-token binary resolution order (first hit wins):
#   1. --bin PATH
#   2. $MCP_TOKEN_EXE
#   3. mcp-token on $PATH
#   4. ~/.local/bin/mcp-token (an earlier run of this installer)
#   5. download a pinned release from GitHub + verify its sha256
#      against the release's checksums.txt (anonymous, no auth
#      required for either file -- see mint-socket.md for why that
#      property matters: a tool that mints tokens must not itself
#      require a token to obtain)
#
# launcher.json config resolution (first hit wins):
#   1. --config PATH
#   2. launcher.json next to the resolved mcp-token binary (see above)
#   3. otherwise: install fails loudly. mcp-token reads its config via
#      internal/config.DefaultPath(), which -- absent an explicit
#      MCP_LAUNCHER_CONFIG -- looks next to *its own binary*. Under
#      socket activation that means "which launcher.json" is decided
#      by wherever step 1-5 above happened to resolve the binary from,
#      which is not something this installer is willing to guess at
#      silently: a socket that activates but can't mint (missing
#      config) fails only at first connection, far from install time.
#      See "Configuration file resolution" in mint-socket.md.
#
# Whatever is resolved is written into mcp-token@.service as
# Environment=MCP_LAUNCHER_CONFIG=<path>, explicitly, every time (see
# the comment in systemd/mcp-token@.service for why this installer
# never leaves it unset even when it matches the exe-adjacent default).
#
# Idempotent: safe to re-run after a repo pull, a mcp-token upgrade, or
# a partial/failed previous run.
set -euo pipefail

REPO="masuda-masuo/mcp-launcher"

# Pin: the mcp-token release this installer downloads when it has to
# fall back to downloading one. It points at the release this copy of
# the script ships in, so a kit unpacked from release X installs the
# binary from release X. Bump it in the commit that is about to be
# tagged, not after.
#
# download_and_verify below treats sha256 verification as mandatory: if
# checksums.txt is missing for the pinned release, it fails loudly
# rather than installing an unverified binary. Every release from
# mcp-token/v1.2.0 on publishes one (.github/workflows/release.yml
# generates it over the whole dist/ directory).
PINNED_VERSION="mcp-token/v1.3.0"

INSTALL_BIN_DIR="${HOME}/.local/bin"
INSTALL_BIN="${INSTALL_BIN_DIR}/mcp-token"
UNIT_DIR="${HOME}/.config/systemd/user"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UNIT_SRC_DIR="${SCRIPT_DIR}/../systemd"

BIN_OVERRIDE=""
CONFIG_OVERRIDE=""

usage() {
  cat <<'EOF'
Usage: install-mint-socket.sh [--bin PATH] [--config PATH]

Installs mcp-token.socket + mcp-token@.service into systemd user scope
and runs `systemctl --user enable --now mcp-token.socket`.

  --bin PATH     Use this mcp-token executable instead of resolving
                 one automatically (PATH / ~/.local/bin / pinned
                 download).
  --config PATH  Use this launcher.json instead of looking for one
                 next to the resolved mcp-token binary. Written into
                 mcp-token@.service as
                 Environment=MCP_LAUNCHER_CONFIG=PATH. If omitted and
                 no launcher.json is found next to the binary, the
                 installer fails rather than installing a socket
                 that cannot mint.
  -h, --help     Show this help.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --bin)
      BIN_OVERRIDE="${2:-}"
      if [ -z "${BIN_OVERRIDE}" ]; then
        echo "error: --bin requires a path argument" >&2
        exit 2
      fi
      shift 2
      ;;
    --bin=*)
      BIN_OVERRIDE="${1#--bin=}"
      shift
      ;;
    --config)
      CONFIG_OVERRIDE="${2:-}"
      if [ -z "${CONFIG_OVERRIDE}" ]; then
        echo "error: --config requires a path argument" >&2
        exit 2
      fi
      shift 2
      ;;
    --config=*)
      CONFIG_OVERRIDE="${1#--config=}"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

log() { echo "[install-mint-socket] $*" >&2; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *)
      log "ERROR: unsupported architecture: $(uname -m)"
      exit 1
      ;;
  esac
}

require_linux() {
  case "$(uname -s)" in
    Linux) : ;;
    *)
      log "ERROR: this installer is systemd user-scope only (Linux). Detected: $(uname -s)."
      exit 1
      ;;
  esac
}

download_and_verify() {
  local arch asset url checksums_url tmpdir expected actual encoded_version

  require_linux
  arch="$(detect_arch)"
  asset="mcp-token-linux-${arch}"
  encoded_version="${PINNED_VERSION//\//%2F}"
  url="https://github.com/${REPO}/releases/download/${encoded_version}/${asset}"
  checksums_url="https://github.com/${REPO}/releases/download/${encoded_version}/checksums.txt"

  tmpdir="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "rm -rf '${tmpdir}'" RETURN

  log "downloading ${asset} from release ${PINNED_VERSION} (anonymous, no token required)..."
  if ! curl -fsSL -o "${tmpdir}/${asset}" "${url}"; then
    log "ERROR: failed to download ${url}"
    exit 1
  fi

  if ! curl -fsSL -o "${tmpdir}/checksums.txt" "${checksums_url}"; then
    log "ERROR: checksums.txt not found for release ${PINNED_VERSION}."
    log "  sha256 verification is mandatory -- refusing to install an unverified binary."
    log "  Every mcp-token release is expected to publish checksums.txt alongside its"
    log "  binaries (.github/workflows/release.yml generates it). Point PINNED_VERSION"
    log "  at a release that has one, or attach checksums.txt to this release."
    exit 1
  fi

  expected="$(grep -F " ${asset}" "${tmpdir}/checksums.txt" | awk '{print $1}' | head -n1)"
  if [ -z "${expected}" ]; then
    log "ERROR: ${asset} is not listed in checksums.txt for release ${PINNED_VERSION}"
    exit 1
  fi
  actual="$(sha256sum "${tmpdir}/${asset}" | awk '{print $1}')"
  if [ "${expected}" != "${actual}" ]; then
    log "ERROR: sha256 mismatch for ${asset}"
    log "  expected: ${expected}"
    log "  actual:   ${actual}"
    exit 1
  fi
  log "sha256 verified (${actual})"

  mkdir -p "${INSTALL_BIN_DIR}"
  install -m 0755 "${tmpdir}/${asset}" "${INSTALL_BIN}"
  log "installed ${INSTALL_BIN}"
}

resolve_binary() {
  if [ -n "${BIN_OVERRIDE}" ]; then
    if [ ! -x "${BIN_OVERRIDE}" ]; then
      log "ERROR: --bin ${BIN_OVERRIDE} is not an executable file"
      exit 1
    fi
    printf '%s\n' "${BIN_OVERRIDE}"
    return
  fi
  if [ -n "${MCP_TOKEN_EXE:-}" ] && [ -x "${MCP_TOKEN_EXE}" ]; then
    printf '%s\n' "${MCP_TOKEN_EXE}"
    return
  fi
  if command -v mcp-token >/dev/null 2>&1; then
    command -v mcp-token
    return
  fi
  if [ -x "${INSTALL_BIN}" ]; then
    printf '%s\n' "${INSTALL_BIN}"
    return
  fi
  download_and_verify
  printf '%s\n' "${INSTALL_BIN}"
}

# resolve_config BIN_PATH
#
# Resolution order:
#   1. --config PATH (validated to exist)
#   2. launcher.json next to BIN_PATH
#   3. fail loudly -- see the top-of-file comment and
#      docs/architecture/mint-socket.md's "Configuration file
#      resolution" section for why this does not fall through to
#      installing a socket that can't mint.
#
# "launcher.json" is hardcoded here (not imported -- this is a shell
# script) but must match internal/config.DefaultConfigName in
# internal/config/path.go.
resolve_config() {
  local bin_path="$1" candidate

  if [ -n "${CONFIG_OVERRIDE}" ]; then
    if [ ! -f "${CONFIG_OVERRIDE}" ]; then
      log "ERROR: --config ${CONFIG_OVERRIDE} does not exist or is not a file"
      exit 1
    fi
    printf '%s\n' "${CONFIG_OVERRIDE}"
    return
  fi

  candidate="$(dirname "${bin_path}")/launcher.json"
  if [ -f "${candidate}" ]; then
    printf '%s\n' "${candidate}"
    return
  fi

  log "ERROR: no launcher.json found next to the mcp-token binary (${candidate})"
  log "  and no --config PATH was given."
  log ""
  log "  mcp-token resolves its config via internal/config.DefaultPath(),"
  log "  which -- absent an explicit MCP_LAUNCHER_CONFIG -- looks next to"
  log "  its own binary. Installing the socket without a resolvable config"
  log "  would enable a socket that accepts connections but fails to mint"
  log "  on every one of them (\"loading config: ... launcher.json: no such"
  log "  file\"), discovered only at first use instead of at install time."
  log ""
  log "  Fix: pass --config PATH pointing at your launcher.json, e.g. the"
  log "  one from an existing install (~/shiori/runtime/launcher.json,"
  log "  /opt/mcp-launcher/bin/launcher.json, ...), or place a"
  log "  launcher.json next to the mcp-token binary before re-running."
  exit 1
}

main() {
  require_linux

  if ! command -v systemctl >/dev/null 2>&1; then
    log "ERROR: systemctl not found -- this installer is systemd user-scope only."
    exit 1
  fi
  if [ ! -f "${UNIT_SRC_DIR}/mcp-token.socket" ] || [ ! -f "${UNIT_SRC_DIR}/mcp-token@.service" ]; then
    log "ERROR: could not find systemd/mcp-token.socket and systemd/mcp-token@.service"
    log "  next to this script (looked in ${UNIT_SRC_DIR})."
    log "  Run this script from a checkout of masuda-masuo/mcp-launcher."
    exit 1
  fi

  local bin_path config_path
  bin_path="$(resolve_binary)"
  log "using mcp-token binary: ${bin_path}"

  config_path="$(resolve_config "${bin_path}")"
  log "using launcher.json: ${config_path}"

  mkdir -p "${UNIT_DIR}"
  cp "${UNIT_SRC_DIR}/mcp-token.socket" "${UNIT_DIR}/mcp-token.socket"
  sed -e "s#__MCP_TOKEN_BIN__#${bin_path}#" \
      -e "s#__MCP_LAUNCHER_CONFIG__#${config_path}#" \
    "${UNIT_SRC_DIR}/mcp-token@.service" > "${UNIT_DIR}/mcp-token@.service"
  log "wrote ${UNIT_DIR}/mcp-token.socket and ${UNIT_DIR}/mcp-token@.service"

  systemctl --user daemon-reload
  systemctl --user enable --now mcp-token.socket
  log "enabled mcp-token.socket (user scope)"

  local linger_state
  linger_state="$(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null || echo unknown)"
  if [ "${linger_state}" != "yes" ]; then
    log ""
    log "NOTE: linger is not enabled for $(id -un) (current: ${linger_state})."
    log "  Without it, the socket dies when your last login session ends"
    log "  (SSH disconnect, headless box, etc). To keep it alive across logout:"
    log "    sudo loginctl enable-linger $(id -un)"
  fi

  log ""
  log "done. Socket: \$XDG_RUNTIME_DIR/mcp-token/mint.sock"
  log "  Verify with: systemctl --user status mcp-token.socket"
  log "  Consumers should read GITHUB_TOKEN_SOCKET=\$XDG_RUNTIME_DIR/mcp-token/mint.sock"
}

main "$@"
