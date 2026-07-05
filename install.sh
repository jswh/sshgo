#!/bin/sh
# sshgo - One-click install script
# Usage: curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh
set -e

REPO="jswh/sshgo"
INSTALL_DIR="${HOME}/.local/bin"

# --- Detect OS and architecture ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "Error: unsupported architecture: $ARCH"
        exit 1
        ;;
esac

case "$OS" in
    linux|darwin)
        ;;
    *)
        echo "Error: unsupported OS: $OS"
        exit 1
        ;;
esac

BINARY_NAME="sshgo-${OS}-${ARCH}"

# --- Fetch latest release tag ---
echo "Fetching latest release information..."
LATEST_URL=$(curl -sIL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null || true)
TAG=$(echo "${LATEST_URL}" | sed 's|.*/tag/||')

if [ -z "${TAG}" ]; then
    echo "Error: failed to determine the latest release tag"
    exit 1
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${BINARY_NAME}"

echo "Latest version: ${TAG}"
echo "Downloading ${BINARY_NAME} ..."

# --- Download and install ---
mkdir -p "${INSTALL_DIR}"

curl -fsSL "${DOWNLOAD_URL}" -o "${INSTALL_DIR}/sshgo"
chmod +x "${INSTALL_DIR}/sshgo"

echo ""
echo "sshgo ${TAG} installed to ${INSTALL_DIR}/sshgo"

# --- Check PATH ---
case ":${PATH}:" in
    *:${INSTALL_DIR}:*)
        echo "Note: ${INSTALL_DIR} is already in your PATH."
        ;;
    *)
        echo ""
        echo "Warning: ${INSTALL_DIR} is not in your PATH."
        echo "To add it, run:"
        echo ""
        # Detect shell and suggest appropriate rc file
        case "${SHELL}" in
            *zsh)
                echo "  echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ~/.zshrc"
                echo "  source ~/.zshrc"
                ;;
            *bash)
                echo "  echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ~/.bashrc"
                echo "  source ~/.bashrc"
                ;;
            *)
                echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
                ;;
        esac
        echo ""
        echo "Alternatively, you can run sshgo directly:"
        echo "  ${INSTALL_DIR}/sshgo --help"
        ;;
esac

echo ""
echo "Done! Run 'sshgo --help' to get started."
