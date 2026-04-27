#!/bin/sh
# Version: 3

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    printf "${GREEN}%s${NC}\n" "$1"
}

warn() {
    printf "${YELLOW}%s${NC}\n" "$1"
}

fail() {
    printf "${RED}%s${NC}\n" "$1" >&2
}

# Exit on error
set -e

# Detect OS and architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux)
        OS_NAME="linux"
        ;;
    Darwin)
        OS_NAME="darwin"
        ;;
    *)
        warn "Unsupported OS: $OS"
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64)
        ARCH_NAME="amd64"
        ;;
    aarch64 | arm64)
        ARCH_NAME="arm64"
        ;;
    *)
        warn "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Construct download URL
URL="https://github.com/y9c/hey/releases/download/latest/hey-latest-${OS_NAME}-${ARCH_NAME}.tar.gz"

# Determine installation directory
if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    if [ -d "$HOME/.local/bin" ]; then
        INSTALL_DIR="$HOME/.local/bin"
    elif [ -d "$HOME/bin" ]; then
        INSTALL_DIR="$HOME/bin"
    else
        INSTALL_DIR="$HOME/.local/bin"
        mkdir -p "$INSTALL_DIR"
    fi
fi

# Check if hey is already installed
if [ -f "$INSTALL_DIR/hey" ]; then
    info "Existing 'hey' binary found at $INSTALL_DIR. Upgrading..."
    IS_UPGRADE=true
else
    info "Installing 'hey' for the first time."
    IS_UPGRADE=false
fi

# Download and install
info "Downloading from $URL"
TMP_DIR=$(mktemp -d)
curl -L "$URL" -o "$TMP_DIR/hey.tar.gz"
tar -xzf "$TMP_DIR/hey.tar.gz" -C "$TMP_DIR"

if ! "$TMP_DIR/hey" --help >/dev/null 2>"$TMP_DIR/hey.err"; then
    fail "Downloaded 'hey' binary could not run on this machine."
    if grep 'GLIBC_.*not found' "$TMP_DIR/hey.err" >/dev/null 2>&1; then
        fail "Reason: this binary requires a newer GLIBC than your system provides."
        warn "This commonly happens on older Linux distributions such as CentOS 7."
        warn "Install from a newer release after this fix, or build locally with Go:"
        printf "  git clone https://github.com/y9c/hey.git\n"
        printf "  cd hey && CGO_ENABLED=0 go build -tags netgo,osusergo -ldflags '-s -w' -o hey .\n"
    else
        fail "Runtime error:"
        sed 's/^/  /' "$TMP_DIR/hey.err" >&2
    fi
    rm -rf "$TMP_DIR"
    exit 1
fi

mv "$TMP_DIR/hey" "$INSTALL_DIR/hey"
rm -rf "$TMP_DIR"

if [ "$IS_UPGRADE" = true ]; then
    info "Successfully upgraded 'hey' to $INSTALL_DIR"
else
    info "Successfully installed 'hey' to $INSTALL_DIR"
fi

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) 
        # In PATH, do nothing
        ;;
    *)
        warn "Warning: '$INSTALL_DIR' is not in your PATH."
        warn "Please add the following line to your ~/.bashrc or ~/.zshrc:"
        printf "export PATH=\"$INSTALL_DIR:$PATH\"\n"
        ;;
esac

info "Installation complete. You can now use the 'hey' command."
