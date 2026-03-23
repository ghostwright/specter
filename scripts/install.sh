#!/bin/sh
set -e

REPO="ghostwright/specter"
BINARY="specter"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) OS="darwin" ;;
  linux)  OS="linux" ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release tag
LATEST=$(curl -sfL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST" ]; then
  echo "Could not determine latest release."
  echo "Install from source: git clone https://github.com/$REPO && cd specter && make build"
  exit 1
fi

VERSION="${LATEST#v}"
FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$LATEST/$FILENAME"

echo "Installing $BINARY $LATEST ($OS/$ARCH)..."

# Download
TMPDIR=$(mktemp -d)
curl -sfL "$URL" -o "$TMPDIR/$FILENAME"
if [ ! -s "$TMPDIR/$FILENAME" ]; then
  echo "Download failed: $URL"
  rm -rf "$TMPDIR"
  exit 1
fi

# Extract
tar -xzf "$TMPDIR/$FILENAME" -C "$TMPDIR"

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
else
  echo "Need sudo to install to $INSTALL_DIR"
  sudo mv "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
fi
chmod +x "$INSTALL_DIR/$BINARY"

# Cleanup
rm -rf "$TMPDIR"

echo "Installed $BINARY $LATEST to $INSTALL_DIR/$BINARY"
$INSTALL_DIR/$BINARY version
