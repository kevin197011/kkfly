#!/usr/bin/env bash
# Copyright (c) 2026 kk — MIT License
# https://opensource.org/licenses/MIT
#
# Install kkfly from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
#   VERSION=0.1.13 curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash -s -- -v 0.1.13
#
# Environment:
#   VERSION      Pin release (with or without leading v)
#   INSTALL_DIR  Target directory (default: /usr/local/bin)

set -euo pipefail

readonly REPO="kevin197011/kkfly"
readonly BINARY="kkfly"

install_dir="${INSTALL_DIR:-/usr/local/bin}"
install_path=""
version="${VERSION:-}"

usage() {
	cat <<EOF
usage: install.sh [-v VERSION] [-h]

  -v VERSION   install this release (e.g. 0.1.13 or v0.1.13)
  -h           show help

env: VERSION, INSTALL_DIR
EOF
}

err() {
	echo "error: $*" >&2
	exit 1
}

info() {
	echo "==> $*"
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || err "missing required command: $1"
}

normalize_version() {
	local v="${1#v}"
	[[ -n "$v" ]] || err "empty version"
	echo "$v"
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) err "unsupported OS: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) err "unsupported arch: $(uname -m)" ;;
	esac
}

latest_version() {
	local url tag
	url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")
	tag="${url##*/tag/}"
	tag="${tag#v}"
	[[ -n "$tag" && "$tag" != "$url" ]] || err "could not resolve latest release"
	echo "$tag"
}

ensure_install_dir() {
	if [[ -w "$install_dir" ]]; then
		install_path="${install_dir}/${BINARY}"
		return
	fi

	if [[ "$EUID" -eq 0 ]]; then
		mkdir -p "$install_dir"
		install_path="${install_dir}/${BINARY}"
		return
	fi

	if [[ "$(uname -s)" == "Darwin" ]]; then
		install_dir="${HOME}/.local/bin"
		install_path="${install_dir}/${BINARY}"
		mkdir -p "$install_dir"
		info "using ${install_dir} (/usr/local/bin not writable)"
		return
	fi

	err "cannot write to ${install_dir} — re-run with sudo or set INSTALL_DIR"
}

verify_checksum() {
	local tmp="$1" archive="$2" ver="$3"
	local sum_file="${tmp}/checksums.txt" verify_file="${tmp}/checksum.verify"

	curl -fsSL "https://github.com/${REPO}/releases/download/v${ver}/checksums.txt" -o "$sum_file"
	grep -F " ${archive}" "$sum_file" >"$verify_file" || err "checksum entry not found for ${archive}"

	if command -v sha256sum >/dev/null 2>&1; then
		( cd "$tmp" && sha256sum -c checksum.verify )
	elif command -v shasum >/dev/null 2>&1; then
		( cd "$tmp" && shasum -a 256 -c checksum.verify )
	else
		err "sha256sum or shasum required for checksum verification"
	fi
}

path_hint() {
	case ":${PATH}:" in
	*":${install_dir}:"*) return ;;
	esac
	echo
	echo "note: add ${install_dir} to PATH:"
	echo "  export PATH=\"${install_dir}:\$PATH\""
}

main() {
	while getopts ":v:h" opt; do
		case "$opt" in
		v) version="$OPTARG" ;;
		h)
			usage
			exit 0
			;;
		*) usage >&2; exit 2 ;;
		esac
	done

	need_cmd curl
	need_cmd tar
	need_cmd install

	local os arch archive url tmp bin
	os=$(detect_os)
	arch=$(detect_arch)
	ensure_install_dir

	if [[ -n "$version" ]]; then
		version=$(normalize_version "$version")
	else
		info "resolving latest release"
		version=$(latest_version)
	fi

	archive="${BINARY}_${version}_${os}_${arch}.tar.gz"
	url="https://github.com/${REPO}/releases/download/v${version}/${archive}"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

	info "downloading v${version} (${os}/${arch})"
	curl -fsSL "$url" -o "${tmp}/${archive}"

	info "verifying checksum"
	verify_checksum "$tmp" "$archive" "$version"

	info "installing to ${install_path}"
	tar -xzf "${tmp}/${archive}" -C "$tmp"
	bin="${tmp}/${BINARY}"
	[[ -x "$bin" ]] || err "binary not found in archive"
	install -m 755 "$bin" "$install_path"

	echo
	echo "installed  $("$install_path" --version 2>/dev/null || echo "v${version}")  →  ${install_path}"
	path_hint
}

main "$@"
