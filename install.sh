#!/bin/sh
# Install codexctl on Linux, macOS, FreeBSD, or WSL.
#
#   curl -fsSL https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.sh | sh
#
# Options may be passed after "sh -s --":
#
#   curl -fsSL .../install.sh | sh -s -- --version 0.2.0 --dir ~/bin
#
#   --version X.Y.Z         install this release instead of the latest one
#   --dir DIR               install into DIR (default: /usr/local/bin when
#                           writable, otherwise ~/.local/bin)
#   --no-verify-signature   skip the Ed25519 check on checksums.txt (the
#                           SHA-256 check is always performed)
#   -h, --help              show this help
#
# The same settings can be given as CODEXCTL_VERSION and CODEXCTL_INSTALL_DIR.
#
# The script downloads the release archive for this platform from GitHub,
# checks its SHA-256 against the release's checksums.txt, verifies the Ed25519
# signature on that file when the local openssl supports it, and places the
# codexctl executable in the install directory. It never uses sudo unless the
# directory was chosen with --dir and is not writable.
#
# Windows users: run install.ps1 from PowerShell instead.

set -eu

REPO="AbdelrhmanSaid/codexctl"
RELEASES_URL="https://github.com/${REPO}/releases"
# Must match releasePublicKey in internal/update/sign.go.
PUBLIC_KEY="FWY509VI6moVjoFy29LLqGE2NhmG7/gwlxXWxt1VoN0="

VERSION="${CODEXCTL_VERSION:-}"
INSTALL_DIR="${CODEXCTL_INSTALL_DIR:-}"
VERIFY_SIGNATURE=1

usage() {
	cat <<'EOF'
Usage: install.sh [--version X.Y.Z] [--dir DIR] [--no-verify-signature]

  --version X.Y.Z         install this release instead of the latest one
  --dir DIR               install into DIR (default: /usr/local/bin when
                          writable, otherwise ~/.local/bin)
  --no-verify-signature   skip the Ed25519 check on checksums.txt; the
                          SHA-256 check is always performed
  -h, --help              show this help

CODEXCTL_VERSION and CODEXCTL_INSTALL_DIR set the same options.
EOF
}

say() { printf '%s\n' "$*"; }
warn() { printf 'install.sh: %s\n' "$*" >&2; }
die() { warn "$*"; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		VERSION="$2"
		shift 2
		;;
	--version=*)
		VERSION="${1#--version=}"
		shift
		;;
	--dir)
		[ $# -ge 2 ] || die "--dir needs a value"
		INSTALL_DIR="$2"
		shift 2
		;;
	--dir=*)
		INSTALL_DIR="${1#--dir=}"
		shift
		;;
	--no-verify-signature)
		VERIFY_SIGNATURE=0
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown option: $1 (try --help)"
		;;
	esac
done

VERSION="${VERSION#v}"

# ---------------------------------------------------------------------------
# Platform

os="$(uname -s)"
case "$os" in
Linux) os=linux ;;
Darwin) os=darwin ;;
FreeBSD) os=freebsd ;;
MINGW* | MSYS* | CYGWIN* | Windows_NT)
	die "on Windows, run install.ps1 from PowerShell instead"
	;;
*) die "unsupported operating system: $os" ;;
esac

arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported CPU architecture: $arch (releases exist for x86-64 and ARM64)" ;;
esac

# A shell running under Rosetta reports x86_64 on Apple silicon.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
	[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
	arch=arm64
fi

# ---------------------------------------------------------------------------
# Tools

have() { command -v "$1" >/dev/null 2>&1; }

if have curl; then
	fetch() { curl -fsSL --retry 3 -o "$2" "$1"; }
elif have wget; then
	fetch() { wget -q -O "$2" "$1"; }
else
	die "curl or wget is required"
fi

have tar || die "tar is required"

if have sha256sum; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif have shasum; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
elif have openssl; then
	sha256() { openssl dgst -sha256 "$1" | sed 's/^.*= *//'; }
else
	die "sha256sum, shasum, or openssl is required to verify the download"
fi

# Ed25519 verification needs OpenSSL 1.1.1 or newer (pkeyutl -rawin). Older
# OpenSSL and most LibreSSL builds cannot do it; then only the SHA-256 check
# protects the download, which still covers corruption and mismatches.
can_verify_signature() {
	have openssl || return 1
	openssl list -public-key-algorithms 2>/dev/null | grep -qi ed25519 || return 1
	openssl pkeyutl -help 2>&1 | grep -q -- '-rawin' || return 1
}

# ---------------------------------------------------------------------------
# Download and verify

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t codexctl)"
trap 'rm -rf "$tmp"' EXIT INT TERM HUP

if [ -n "$VERSION" ]; then
	base="${RELEASES_URL}/download/v${VERSION}"
else
	base="${RELEASES_URL}/latest/download"
fi

say "Fetching release manifest..."
fetch "${base}/checksums.txt" "$tmp/checksums.txt" ||
	die "cannot download ${base}/checksums.txt; check the version and your network"

if [ "$VERIFY_SIGNATURE" = 1 ]; then
	if fetch "${base}/checksums.txt.sig" "$tmp/checksums.txt.sig" 2>/dev/null; then
		if can_verify_signature; then
			# Wrap the raw 32-byte key in the SubjectPublicKeyInfo header
			# openssl expects: 302a300506032b6570032100 || key.
			{
				printf '\060\052\060\005\006\003\053\145\160\003\041\000'
				printf '%s' "$PUBLIC_KEY" | openssl base64 -d -A
			} | openssl base64 -A | {
				printf -- '-----BEGIN PUBLIC KEY-----\n'
				cat
				printf '\n-----END PUBLIC KEY-----\n'
			} >"$tmp/release.pub"
			tr -d ' \n\r' <"$tmp/checksums.txt.sig" | openssl base64 -d -A >"$tmp/checksums.sig.bin"
			if openssl pkeyutl -verify -pubin -inkey "$tmp/release.pub" -rawin \
				-in "$tmp/checksums.txt" -sigfile "$tmp/checksums.sig.bin" >/dev/null 2>&1; then
				say "Release signature verified."
			else
				die "checksums.txt does not carry a valid codexctl release signature; refusing to install"
			fi
		else
			warn "openssl on this system cannot verify Ed25519 signatures; relying on SHA-256 only"
		fi
	else
		die "this release has no checksums.txt.sig (releases before v0.2.0 were unsigned); pass --no-verify-signature to install it anyway"
	fi
fi

if [ -z "$VERSION" ]; then
	VERSION="$(awk '$2 ~ /^codexctl_/ { split($2, p, "_"); print p[2]; exit }' "$tmp/checksums.txt")"
	[ -n "$VERSION" ] || die "checksums.txt lists no codexctl assets"
fi

asset="codexctl_${VERSION}_${os}_${arch}.tar.gz"
expected="$(awk -v n="$asset" '$2 == n { print $1; exit }' "$tmp/checksums.txt")"
[ -n "$expected" ] || die "release ${VERSION} has no asset for ${os}/${arch}"

say "Downloading codexctl ${VERSION} for ${os}/${arch}..."
fetch "${base}/${asset}" "$tmp/$asset" || die "cannot download ${base}/${asset}"

actual="$(sha256 "$tmp/$asset")"
[ "$actual" = "$expected" ] || die "SHA-256 mismatch for ${asset}: expected ${expected}, got ${actual}"
say "Checksum verified."

mkdir "$tmp/extract"
tar -xzf "$tmp/$asset" -C "$tmp/extract"
binary="$(find "$tmp/extract" -type f -name codexctl | head -n 1)"
[ -n "$binary" ] || die "archive ${asset} does not contain a codexctl executable"

# ---------------------------------------------------------------------------
# Install

# writable DIR succeeds when DIR can be written to, or can be created.
writable() {
	if [ -d "$1" ]; then
		[ -w "$1" ]
	else
		[ -w "$(dirname "$1")" ]
	fi
}

use_sudo=0
if [ -z "$INSTALL_DIR" ]; then
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		INSTALL_DIR=/usr/local/bin
	else
		INSTALL_DIR="${HOME}/.local/bin"
	fi
elif ! writable "$INSTALL_DIR" && [ "$(id -u)" != 0 ]; then
	have sudo || die "${INSTALL_DIR} is not writable and sudo is not available"
	use_sudo=1
fi

run() {
	if [ "$use_sudo" = 1 ]; then
		sudo "$@"
	else
		"$@"
	fi
}

[ "$use_sudo" = 1 ] && say "Installing into ${INSTALL_DIR} requires sudo."

target="${INSTALL_DIR}/codexctl"
run mkdir -p "$INSTALL_DIR"
# Copy next to the target and rename over it so a concurrent invocation never
# sees a half-written executable.
staged="${INSTALL_DIR}/.codexctl.new.$$"
run cp "$binary" "$staged"
run chmod 755 "$staged"
run mv -f "$staged" "$target"

say "Installed codexctl ${VERSION} to ${target}"

# ---------------------------------------------------------------------------
# Post-install hints

other="$(command -v codexctl 2>/dev/null || true)"
case ":${PATH}:" in
*":${INSTALL_DIR}:"*)
	if [ -n "$other" ] && [ "$other" != "$target" ]; then
		warn "another codexctl at ${other} comes first on PATH; remove it or reorder PATH"
	fi
	;;
*)
	say ""
	say "${INSTALL_DIR} is not on your PATH. Add it with:"
	say ""
	say "  export PATH=\"${INSTALL_DIR}:\$PATH\""
	say ""
	say "and put that line in your shell profile to make it permanent."
	;;
esac

say "Run 'codexctl update' later to upgrade in place."
