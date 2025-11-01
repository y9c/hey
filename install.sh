#!/bin/bash

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
        echo "Unsupported OS: $OS"
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
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Construct download URL
URL="https://github.com/y9c/hey/releases/download/latest/hey-latest-${OS_NAME}-${ARCH_NAME}.tar.gz"

# Determine installation directory
if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    # Check for ~/.local/bin, then ~/bin
    if [ -d "$HOME/.local/bin" ]; then
        INSTALL_DIR="$HOME/.local/bin"
    elif [ -d "$HOME/bin" ]; then
        INSTALL_DIR="$HOME/bin"
    else
        # Create ~/.local/bin if it doesn't exist
        INSTALL_DIR="$HOME/.local/bin"
        mkdir -p "$INSTALL_DIR"
    fi
fi

# Check if hey is already installed
if [ -f "$INSTALL_DIR/hey" ]; then
    echo "Existing 'hey' binary found at $INSTALL_DIR. Upgrading..."
    IS_UPGRADE=true
else
    echo "Installing 'hey' for the first time."
    IS_UPGRADE=false
fi

# Download and install
echo "Downloading from $URL"
# Use a temporary directory for download and extraction
TMP_DIR=$(mktemp -d)
curl -L "$URL" -o "$TMP_DIR/hey.tar.gz"
tar -xzf "$TMP_DIR/hey.tar.gz" -C "$TMP_DIR"
mv "$TMP_DIR/hey" "$INSTALL_DIR/hey"
rm -rf "$TMP_DIR"


if [ "$IS_UPGRADE" = true ]; then
    echo "Successfully upgraded 'hey' to $INSTALL_DIR"
else
    echo "Successfully installed 'hey' to $INSTALL_DIR"
fi

# Check if INSTALL_DIR is in PATH
if ! [[ ":$PATH:" == *":$INSTALL_DIR:"* ]]; then
    echo "Warning: '$INSTALL_DIR' is not in your PATH."
    echo "Please add the following line to your ~/.bashrc or ~/.zshrc:"
    echo "export PATH=\"$PATH:$INSTALL_DIR\""
fi

echo "Installation complete. You can now use the 'hey' command."
