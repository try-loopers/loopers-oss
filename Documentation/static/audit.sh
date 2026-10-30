#!/bin/sh
# File: Documentation/static/audit.sh
# Usage:
#   curl -sSL https://tryloopers.com/audit.sh | sh
#   curl -sSL https://tryloopers.com/audit.sh | sh -s -- ~/.openclaw --fix
set -e

REPO="try-loopers/loopers-oss"
CACHE_DIR="${HOME}/.cache/loopers/bin"
BIN_PATH="${CACHE_DIR}/loopers"

# Detect OS and Architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

case "${OS}" in
  Linux*)   OS_NAME="Linux" ;;
  Darwin*)  OS_NAME="Darwin" ;;
  *)        echo "Unsupported OS: ${OS}. Use 'go install github.com/${REPO}/cmd/loopers@latest'"; exit 1 ;;
esac

case "${ARCH}" in
  x86_64|amd64)  ARCH_NAME="x86_64" ;;
  arm64|aarch64) ARCH_NAME="arm64" ;;
  *)             echo "Unsupported architecture: ${ARCH}"; exit 1 ;;
esac

# Download latest pre-built binary to ~/.cache/loopers/bin if not already cached
if [ ! -x "${BIN_PATH}" ]; then
  mkdir -p "${CACHE_DIR}"
  TMP_DIR="$(mktemp -d /tmp/loopers-audit.XXXXXX)"
  trap 'rm -rf "${TMP_DIR}"' EXIT

  TARBALL_URL="https://github.com/${REPO}/releases/latest/download/loopers_${OS_NAME}_${ARCH_NAME}.tar.gz"
  printf "  \033[90m› Fetching Loopers X-Ray scanner (%s/%s)...\033[0m\n" "${OS_NAME}" "${ARCH_NAME}"
  curl -fsSL "${TARBALL_URL}" -o "${TMP_DIR}/loopers.tar.gz"
  tar -xzf "${TMP_DIR}/loopers.tar.gz" -C "${TMP_DIR}"
  mv "${TMP_DIR}/loopers" "${BIN_PATH}"
  chmod +x "${BIN_PATH}"
fi

# Execute loopers audit against current directory (or passed args)
"${BIN_PATH}" audit "$@"

