#!/bin/sh
# Install (or update) the mempak CLI from the latest GitHub release.
#
# The repo is private, so downloads go through the GitHub CLI (gh), which uses
# your GitHub login. From a clone:
#
#   ./install.sh
#
# or without a clone:
#
#   gh api repos/matt-freed/mempak-tools/contents/install.sh -H 'Accept: application/vnd.github.raw' | sh
#
# If gh is not installed or not logged in, plain curl is used, which only works
# if the repo is public.
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
elif command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
	# Authenticated download: works for private repos. With no tag argument,
	# gh downloads from the latest release.
	echo "Downloading $PACKAGE.tar.gz from ${MEMPAK_VERSION:-the latest release} (via gh)"
	# shellcheck disable=SC2086 # MEMPAK_VERSION is intentionally unquoted: empty means latest
	gh release download ${MEMPAK_VERSION:-} --repo "$REPO" --pattern "$PACKAGE.tar.gz" \
		--output "$TMP/mempak.tar.gz" || err "download failed (does the release exist? see 'gh release list --repo $REPO')"
else
	if [ -n "${MEMPAK_VERSION:-}" ]; then
		URL="https://github.com/$REPO/releases/download/$MEMPAK_VERSION/$PACKAGE.tar.gz"
	else
		URL="https://github.com/$REPO/releases/latest/download/$PACKAGE.tar.gz"
	fi
	echo "Downloading $URL"
	curl -fSL --progress-bar "$URL" -o "$TMP/mempak.tar.gz" || \
		err "download failed. The repo is private: install the GitHub CLI and run 'gh auth login', then retry."
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
