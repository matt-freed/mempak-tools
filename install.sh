#!/bin/sh
# Install (or update) the mempak CLI from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/matt-freed/mempak-tools/main/install.sh | sh
#
# or from a clone: ./install.sh
#
# Environment variables:
#   PREFIX          Install to $PREFIX/bin (default: $HOME/.local)
#   MEMPAK_VERSION  Install a specific release tag, e.g. v0.1.42 (default: latest)
#   MEMPAK_URL      Install from this tarball URL instead (for testing)
set -eu

REPO="matt-freed/mempak-tools"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"

err() { echo "install.sh: $*" >&2; exit 1; }

case "$(uname -s)" in
	Darwin) PACKAGE="mempak-macos-universal" ;;
	Linux)
		[ "$(uname -m)" = "x86_64" ] || err "no prebuilt Linux binary for $(uname -m); build from source"
		PACKAGE="mempak-linux-x86_64" ;;
	*) err "unsupported OS $(uname -s); on Windows download the .zip from the releases page" ;;
esac

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if [ -n "${MEMPAK_URL:-}" ]; then
	echo "Downloading $MEMPAK_URL"
	curl -fSL --progress-bar "$MEMPAK_URL" -o "$TMP/mempak.tar.gz" || err "download failed"
else
	if [ -n "${MEMPAK_VERSION:-}" ]; then
		URL="https://github.com/$REPO/releases/download/$MEMPAK_VERSION/$PACKAGE.tar.gz"
	else
		URL="https://github.com/$REPO/releases/latest/download/$PACKAGE.tar.gz"
	fi
	echo "Downloading $URL"
	curl -fSL --progress-bar "$URL" -o "$TMP/mempak.tar.gz" || \
		err "download failed (does the release exist? see https://github.com/$REPO/releases)"
fi
tar -xzf "$TMP/mempak.tar.gz" -C "$TMP"
[ -f "$TMP/$PACKAGE/mempak" ] || err "archive does not contain $PACKAGE/mempak"

mkdir -p "$BIN_DIR"
# Install via a temp name + rename so a running copy is never half-written.
cp "$TMP/$PACKAGE/mempak" "$BIN_DIR/.mempak.new"
chmod 755 "$BIN_DIR/.mempak.new"
if [ "$(uname -s)" = "Darwin" ]; then
	# curl doesn't set the quarantine flag, but clear it in case something else did.
	xattr -d com.apple.quarantine "$BIN_DIR/.mempak.new" 2>/dev/null || true
fi
mv -f "$BIN_DIR/.mempak.new" "$BIN_DIR/mempak"

echo "Installed: $("$BIN_DIR/mempak" --version) -> $BIN_DIR/mempak"

case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*)
		echo
		echo "$BIN_DIR is not on your PATH. Add it with:"
		case "${SHELL:-}" in
			*zsh)  echo "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc && exec zsh" ;;
			*bash) echo "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.bashrc && exec bash" ;;
			*)     echo "  export PATH=\"$BIN_DIR:\$PATH\"" ;;
		esac
		;;
esac
