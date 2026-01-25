#!/usr/bin/env bash

# Copyright (c) 2025 kk
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT

set -o errexit
set -o nounset
set -o pipefail

# curl exec:
# curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash

# vars (env overridable)
KKFLY_REPO="${KKFLY_REPO:-kevin197011/kkfly}"
KKFLY_VERSION="${KKFLY_VERSION:-}" # empty => latest
KKFLY_BIN_DIR="${KKFLY_BIN_DIR:-/usr/local/bin}"
KKFLY_DOWNLOAD_PREFIX="${KKFLY_DOWNLOAD_PREFIX:-}"
KKFLY_VERBOSE="${KKFLY_VERBOSE:-0}"

kkfly::install::log() { [[ "${KKFLY_VERBOSE}" == "1" ]] && echo "$@" >&2; }

kkfly::install::die() {
  echo "Error: $*" >&2
  exit 1
}

kkfly::install::need_cmd() {
  command -v "$1" >/dev/null 2>&1 || kkfly::install::die "Missing required command: $1"
}

kkfly::install::download() {
  local url="$1"
  local dest="$2"
  local full_url="${url}"
  [[ -n "${KKFLY_DOWNLOAD_PREFIX}" ]] && full_url="${KKFLY_DOWNLOAD_PREFIX}${url}"

  if command -v curl >/dev/null 2>&1; then
    curl -fL -sS --retry 3 --retry-delay 1 -o "${dest}" "${full_url}" \
      || kkfly::install::die "Download failed: ${full_url}"
    kkfly::install::log "Downloaded $(basename "${dest}")"
    return 0
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -nv -O "${dest}" "${full_url}" 2>/dev/null \
      || kkfly::install::die "Download failed: ${full_url}"
    kkfly::install::log "Downloaded $(basename "${dest}")"
    return 0
  fi

  kkfly::install::die "Missing required command: curl or wget"
}

kkfly::install::sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "${file}" | awk '{print $NF}'
    return 0
  fi
  kkfly::install::die "Missing sha256 tool (need sha256sum or shasum or openssl)"
}

kkfly::install::detect_platform() {
  local os arch
  case "$(uname -s 2>/dev/null || true)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) kkfly::install::die "Unsupported OS" ;;
  esac

  case "$(uname -m 2>/dev/null || true)" in
    x86_64 | amd64) arch="amd64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *) kkfly::install::die "Unsupported CPU arch" ;;
  esac

  echo "${os} ${arch}"
}

kkfly::install::normalize_tag() {
  local tag="$1"
  [[ "${tag}" == v* ]] || tag="v${tag}"
  echo "${tag}"
}

kkfly::install::resolve_latest_tag() {
  local repo="$1"
  local url="https://github.com/${repo}/releases/latest"

  if command -v curl >/dev/null 2>&1; then
    local final
    final="$(curl -fL -sS -o /dev/null -w '%{url_effective}' "${url}")" || return 1
    [[ -n "${final}" ]] || return 1
    echo "${final##*/}"
    return 0
  fi

  if command -v wget >/dev/null 2>&1; then
    local loc
    loc="$(wget -S --spider "${url}" 2>&1 | awk '/^  Location: /{print $2}' | tail -n1 | tr -d '\r')" || true
    [[ -n "${loc}" ]] || return 1
    echo "${loc##*/}"
    return 0
  fi

  return 1
}

kkfly::install::verify_checksum() {
  local archive_path="$1"
  local checksums_path="$2"
  local asset_name expected actual

  asset_name="$(basename "${archive_path}")"
  expected="$(
    awk -v name="${asset_name}" '
      NF >= 2 {
        f = $2
        sub(/^\*/, "", f)
        if (f == name) { print $1; exit }
      }
    ' "${checksums_path}"
  )"
  [[ -n "${expected}" ]] || kkfly::install::die "checksums.txt does not contain ${asset_name}"

  actual="$(kkfly::install::sha256_file "${archive_path}")"
  [[ "${expected}" == "${actual}" ]] || kkfly::install::die "Checksum mismatch for ${asset_name}"
}

kkfly::install::extract_binary_path() {
  local archive_path="$1"
  tar -tzf "${archive_path}" \
    | awk '{n=$0; sub(/.*\//,"",n); if(n=="kkfly"){print $0; exit}}'
}

kkfly::install::install_to_bindir() {
  local src="$1"
  local dest="$2"

  if command -v install >/dev/null 2>&1; then
    install -m 0755 "${src}" "${dest}" 2>/dev/null && return 0
  else
    cp "${src}" "${dest}" 2>/dev/null && chmod 0755 "${dest}" 2>/dev/null && return 0
  fi

  if ! command -v sudo >/dev/null 2>&1; then
    kkfly::install::die "Permission denied installing to ${dest} (run as root, or use sudo)"
  fi

  sudo -n bash -c "mkdir -p \"$(dirname "${dest}")\" && (command -v install >/dev/null 2>&1 && install -m 0755 \"${src}\" \"${dest}\" || (cp \"${src}\" \"${dest}\" && chmod 0755 \"${dest}\"))" \
    || kkfly::install::die "sudo failed installing to ${dest} (ensure NOPASSWD or run as root)"
}

kkfly::install::common() {
  kkfly::install::need_cmd tar
  kkfly::install::need_cmd awk

  local os arch tag version asset_name base_url asset_url checksums_url
  read -r os arch < <(kkfly::install::detect_platform)

  if [[ -n "${KKFLY_VERSION}" ]]; then
    tag="$(kkfly::install::normalize_tag "${KKFLY_VERSION}")"
  else
    tag="$(kkfly::install::resolve_latest_tag "${KKFLY_REPO}")" \
      || kkfly::install::die "Unable to resolve latest release tag (check GitHub connectivity)"
  fi

  version="${tag#v}"
  asset_name="kkfly_${version}_${os}_${arch}.tar.gz"
  base_url="https://github.com/${KKFLY_REPO}/releases/download/${tag}"
  asset_url="${base_url}/${asset_name}"
  checksums_url="${base_url}/checksums.txt"

  kkfly::install::log "Release tag: ${tag}"
  kkfly::install::log "Asset: ${asset_url}"
  kkfly::install::log "Bin dir: ${KKFLY_BIN_DIR}"
  [[ -n "${KKFLY_DOWNLOAD_PREFIX}" ]] && kkfly::install::log "Download prefix: ${KKFLY_DOWNLOAD_PREFIX}"

  local tmpdir archive_path checksums_path entry_path extracted_path
  tmpdir="$(mktemp -d -t kkfly-install.XXXXXX)"
  trap 'rm -rf "${tmpdir}"' EXIT

  archive_path="${tmpdir}/${asset_name}"
  checksums_path="${tmpdir}/checksums.txt"

  echo "Installing kkfly ${tag}..."
  echo "Downloading ${asset_name}..."
  kkfly::install::download "${asset_url}" "${archive_path}"
  echo "Downloading checksums.txt..."
  kkfly::install::download "${checksums_url}" "${checksums_path}"
  kkfly::install::verify_checksum "${archive_path}" "${checksums_path}"

  entry_path="$(kkfly::install::extract_binary_path "${archive_path}")"
  [[ -n "${entry_path}" ]] || kkfly::install::die "Binary kkfly not found in archive"

  tar -xzf "${archive_path}" -C "${tmpdir}" "${entry_path}"
  extracted_path="${tmpdir}/${entry_path}"
  [[ -f "${extracted_path}" ]] || kkfly::install::die "Extracted binary not found"

  kkfly::install::install_to_bindir "${extracted_path}" "${KKFLY_BIN_DIR}/kkfly"

  if [[ -x "${KKFLY_BIN_DIR}/kkfly" ]]; then
    echo "Installed: ${KKFLY_BIN_DIR}/kkfly"
    "${KKFLY_BIN_DIR}/kkfly" --version 2>/dev/null || true
  else
    kkfly::install::die "Install verification failed: ${KKFLY_BIN_DIR}/kkfly"
  fi
}

kkfly::install::centos() { kkfly::install::common "$@"; }
kkfly::install::debian() { kkfly::install::common "$@"; }
kkfly::install::mac() { kkfly::install::common "$@"; }

kkfly::install::run() {
  local platform='debian'
  command -v yum >/dev/null && platform='centos'
  command -v dnf >/dev/null && platform='centos'
  command -v brew >/dev/null && platform='mac'
  eval "${FUNCNAME/::run/::${platform}}"
}

# run main
kkfly::install::run "$@"