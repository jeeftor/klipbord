#!/usr/bin/env bash
# install.sh — installer for Klipbord release binaries.
#
# Usage:
#   curl -fsSL https://github.com/jeeftor/klipbord/releases/latest/download/install.sh | sh
#
# Flags:
#   --version vX.Y.Z      Install a specific release version (default: latest)
#   --component cli|server Install the CLI (default) or server binary
#   --install-dir /path   Override the install directory
#
# The script:
#   1. Detects OS (darwin/linux) and arch (arm64/amd64)
#   2. Resolves the latest release tag (or uses the requested one)
#   3. Downloads the matching component archive
#   4. Downloads `kb_checksums.txt` and verifies the archive SHA256
#   5. Optionally verifies the cosign signature if `cosign` is installed
#   6. Extracts and installs the requested binary
#   7. Warns if the install dir isn't on $PATH

set -eu
# Enable pipefail when the shell supports it (bash/mksh). Under a strict POSIX
# sh (e.g. dash) the option is unavailable, so we fall back gracefully — this
# keeps `curl ... | sh` working everywhere while still catching pipeline
# failures on bash.
if set -o pipefail 2>/dev/null; then
    set -o pipefail
fi

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
OWNER="jeeftor"
REPO="klipbord"
COMPONENT="cli"
BINARY_NAME=""
GITHUB_BASE="https://github.com/${OWNER}/${REPO}"

# Defaults (may be overridden by flags)
VERSION=""
INSTALL_DIR=""

# ---------------------------------------------------------------------------
# Cleanup — remove the temp directory on exit (success or failure)
# ---------------------------------------------------------------------------
TMP_DIR=""
cleanup() {
    if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
        rm -rf "$TMP_DIR"
    fi
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log() {
    printf '== %s\n' "$*"
}
warn() {
    printf '!! %s\n' "$*" >&2
}
die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Parse command-line arguments
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            [ $# -ge 2 ] || die "--version requires a value"
            VERSION="$2"
            shift 2
            ;;
        --version=*)
            VERSION="${1#*=}"
            shift
            ;;
        --install-dir)
            [ $# -ge 2 ] || die "--install-dir requires a value"
            INSTALL_DIR="$2"
            shift 2
            ;;
        --install-dir=*)
            INSTALL_DIR="${1#*=}"
            shift
            ;;
        --component)
            [ $# -ge 2 ] || die "--component requires cli or server"
            COMPONENT="$2"
            shift 2
            ;;
        --component=*)
            COMPONENT="${1#*=}"
            shift
            ;;
        -h|--help)
            cat <<EOF
Usage: install.sh [options]

Options:
  --component cli|server Install the CLI (default) or server binary
  --version vX.Y.Z      Install a specific release version (default: latest)
  --install-dir /path   Override the install directory
  -h, --help            Show this help message
EOF
            exit 0
            ;;
        *)
            die "unknown option: $1"
            ;;
    esac
done

case "$COMPONENT" in
    cli|server) BINARY_NAME="kb-${COMPONENT}" ;;
    *) die "--component must be cli or server" ;;
esac

# ---------------------------------------------------------------------------
# Detect OS and architecture
# ---------------------------------------------------------------------------
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux"  ;;
    *) die "unsupported operating system: $OS (only darwin/linux are supported)" ;;
esac

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) die "unsupported architecture: $ARCH (only amd64/arm64 are supported)" ;;
esac

log "Detected platform: ${OS}/${ARCH}"

# ---------------------------------------------------------------------------
# Resolve the release version
# ---------------------------------------------------------------------------
# If no explicit version was requested, follow redirects on the /releases/latest
# URL to discover the tag name (e.g. "v1.2.3").
if [ -z "$VERSION" ]; then
    log "Resolving latest release version..."
    release_url="${GITHUB_BASE}/releases/latest"
    # -I: headers only, -fsSL: fail silently, follow redirects, show errors.
    # The final Location header ends with .../releases/tag/<tag-name>.
    VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$release_url" \
        | sed 's|.*/releases/tag/||')"
    [ -n "$VERSION" ] || die "could not determine latest release version"
fi

# Normalise: strip a leading 'v' for asset naming only when needed; GitHub
# release assets use the tag as-is, so we keep VERSION verbatim.
log "Installing version: ${VERSION}"

# ---------------------------------------------------------------------------
# Resolve install directory
# ---------------------------------------------------------------------------
if [ -z "$INSTALL_DIR" ]; then
    # Root gets a system-wide location; everyone else gets ~/.local/bin.
    if [ "$(id -u)" -eq 0 ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="${HOME}/.local/bin"
    fi
fi
log "Install directory: ${INSTALL_DIR}"

# ---------------------------------------------------------------------------
# Prepare a temp working directory
# ---------------------------------------------------------------------------
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t kb-install)"
ARCHIVE_NAME="${BINARY_NAME}_${OS}_${ARCH}.tar.gz"
ARCHIVE_PATH="${TMP_DIR}/${ARCHIVE_NAME}"
CHECKSUMS_PATH="${TMP_DIR}/kb_checksums.txt"

# ---------------------------------------------------------------------------
# Download the archive and checksums file
# ---------------------------------------------------------------------------
download_url="${GITHUB_BASE}/releases/download/${VERSION}/${ARCHIVE_NAME}"
checksums_url="${GITHUB_BASE}/releases/download/${VERSION}/kb_checksums.txt"

log "Downloading archive: ${ARCHIVE_NAME}"
curl -fsSL -o "$ARCHIVE_PATH" "$download_url"

log "Downloading checksums: kb_checksums.txt"
curl -fsSL -o "$CHECKSUMS_PATH" "$checksums_url"

# ---------------------------------------------------------------------------
# Verify the SHA256 checksum of the archive
# ---------------------------------------------------------------------------
# Pick the right checksum tool: macOS ships `shasum`, Linux ships `sha256sum`.
if command -v sha256sum >/dev/null 2>&1; then
    CHECKSUM_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    CHECKSUM_CMD="shasum -a 256"
else
    die "no SHA256 tool found (need sha256sum or shasum)"
fi

log "Verifying SHA256 checksum..."
# The checksums file lists "<hash>  <filename>" lines. Grep for our archive's
# line and verify the computed hash matches the expected one.
expected_line="$(grep "  ${ARCHIVE_NAME}\$" "$CHECKSUMS_PATH" || true)"
[ -n "$expected_line" ] || die "no checksum entry found for ${ARCHIVE_NAME} in kb_checksums.txt"

expected_hash="$(printf '%s' "$expected_line" | awk '{print $1}')"
actual_hash="$(cd "$TMP_DIR" && eval "$CHECKSUM_CMD" "$ARCHIVE_NAME" | awk '{print $1}')"

if [ "$expected_hash" != "$actual_hash" ]; then
    die "checksum mismatch for ${ARCHIVE_NAME}:
  expected: ${expected_hash}
  actual:   ${actual_hash}"
fi
log "Checksum OK (${expected_hash})"

# ---------------------------------------------------------------------------
# Optional: verify the cosign signature
# ---------------------------------------------------------------------------
# If cosign is available and the release publishes a .sig + .pem pair for the
# archive, verify the blob signature. If cosign isn't installed we simply skip
# with a note. If cosign is installed but the signature assets are missing we
# warn and continue rather than failing the install.
if command -v cosign >/dev/null 2>&1; then
    SIG_PATH="${TMP_DIR}/${ARCHIVE_NAME}.sig"
    PEM_PATH="${TMP_DIR}/${ARCHIVE_NAME}.pem"
    sig_url="${GITHUB_BASE}/releases/download/${VERSION}/${ARCHIVE_NAME}.sig"
    pem_url="${GITHUB_BASE}/releases/download/${VERSION}/${ARCHIVE_NAME}.pem"

    log "cosign detected — attempting signature verification..."
    if curl -fsSL -o "$SIG_PATH" "$sig_url" \
        && curl -fsSL -o "$PEM_PATH" "$pem_url"; then
        if cosign verify-blob \
                --signature "$SIG_PATH" \
                --certificate "$PEM_PATH" \
                --certificate-identity-regexp "https://github.com/${OWNER}/${REPO}/.github/.+" \
                --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
                "$ARCHIVE_PATH"; then
            log "cosign signature verified"
        else
            die "cosign signature verification failed for ${ARCHIVE_NAME}"
        fi
    else
        warn "cosign is installed but no .sig/.pem assets were found for this release; skipping signature verification"
    fi
else
    warn "cosign not installed — skipping signature verification (install cosign to enable it)"
fi

# ---------------------------------------------------------------------------
# Extract the archive and install the binary
# ---------------------------------------------------------------------------
log "Extracting archive..."
tar -C "$TMP_DIR" -xzf "$ARCHIVE_PATH"

# The archive should contain the requested top-level binary.
BINARY_SRC="${TMP_DIR}/${BINARY_NAME}"
[ -f "$BINARY_SRC" ] || die "binary '${BINARY_NAME}' not found in archive"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$BINARY_SRC" "${INSTALL_DIR}/${BINARY_NAME}"
log "Installed ${BINARY_NAME} to ${INSTALL_DIR}/${BINARY_NAME}"

# ---------------------------------------------------------------------------
# PATH advice
# ---------------------------------------------------------------------------
# Only advise for the default ~/.local/bin location; a custom --install-dir is
# the user's responsibility.
if [ "$INSTALL_DIR" = "${HOME}/.local/bin" ]; then
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*)
            log "Done. Run '${BINARY_NAME} --help' to get started."
            ;;
        *)
            printf '\n'
            log "${INSTALL_DIR} is not on your PATH."
            log "Add it to your shell profile, e.g.:"
            printf '    export PATH="%s:$PATH"\n' "$INSTALL_DIR"
            printf '\n'
            log "Then start a new shell or run: source ~/.profile (or ~/.zshrc / ~/.bashrc)"
            ;;
    esac
else
    log "Done. Run '${BINARY_NAME} --help' to get started."
fi
