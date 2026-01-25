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

# vars
KKFLY_REPO="${KKFLY_REPO:-kevin197011/kkfly}"
KKFLY_VERSION="${KKFLY_VERSION:-}"
KKFLY_BIN_DIR="${KKFLY_BIN_DIR:-/usr/local/bin}"
KKFLY_VERBOSE="${KKFLY_VERBOSE:-0}"

kkfly::install::usage() {
    cat <<'EOF'
Usage: install.sh [options]

Options:
  --repo REPO        GitHub repo, e.g. owner/name (default: kevin197011/kkfly) (env: KKFLY_REPO)
  --version VERSION  Release version/tag (default: latest). Accepts 1.2.3 or v1.2.3. (env: KKFLY_VERSION)
  --bin-dir DIR      Install directory (default: /usr/local/bin) (env: KKFLY_BIN_DIR)
  --verbose          Verbose output
  -h, --help         Show this help
EOF
}

kkfly::install::log() {
    [[ "${KKFLY_VERBOSE}" == "1" ]] && echo "$@" >&2
}

kkfly::install::die() {
    echo "Error: $*" >&2
    exit 1
}

kkfly::install::need_cmd() {
    command -v "$1" >/dev/null 2>&1 || kkfly::install::die "Missing required command: $1"
}

kkfly::install::sudo_prefix() {
    if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
        echo ""
        return 0
    fi
    command -v sudo >/dev/null 2>&1 || kkfly::install::die "This installer requires root or sudo"
    echo "sudo"
}

kkfly::install::detect_platform() {
    local os arch uos uarch
    uos="$(uname -s 2>/dev/null || true)"
    uarch="$(uname -m 2>/dev/null || true)"

    case "${uos}" in
        Darwin) os="darwin" ;;
        Linux) os="linux" ;;
        MINGW* | MSYS* | CYGWIN* | Windows_NT) os="windows" ;;
        *) kkfly::install::die "Unsupported OS: ${uos}" ;;
    esac

    case "${uarch}" in
        x86_64 | amd64) arch="amd64" ;;
        arm64 | aarch64) arch="arm64" ;;
        *) kkfly::install::die "Unsupported CPU arch: ${uarch}" ;;
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

    # Prefer curl because it can reliably return the final URL after redirects.
    if command -v curl >/dev/null 2>&1; then
        local final
        final="$(curl -fL -sS -o /dev/null -w '%{url_effective}' "${url}")" || return 1
        [[ -n "${final}" ]] || return 1
        echo "${final##*/}"
        return 0
    fi

    # Fallback: parse Location headers from wget spider output.
    if command -v wget >/dev/null 2>&1; then
        local loc
        loc="$(wget -S --spider "${url}" 2>&1 | awk '/^  Location: /{print $2}' | tail -n1 | tr -d '\r')" || true
        [[ -n "${loc}" ]] || return 1
        echo "${loc##*/}"
        return 0
    fi

    return 1
}

kkfly::install::download_file() {
    local url="$1"
    local dest="$2"
    if command -v curl >/dev/null 2>&1; then
        if ! curl -fL -sS --retry 3 --retry-delay 1 -o "${dest}" "${url}"; then
            kkfly::install::die "Download failed: ${url}"
        fi
        kkfly::install::log "Downloaded $(basename "${dest}")"
        return 0
    fi
    if command -v wget >/dev/null 2>&1; then
        if ! wget -nv -O "${dest}" "${url}" 2>/dev/null; then
            kkfly::install::die "Download failed: ${url}"
        fi
        kkfly::install::log "Downloaded $(basename "${dest}")"
        return 0
    fi
    kkfly::install::die "Error: curl or wget is required"
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

kkfly::install::extract_tar_gz_binary() {
    local archive_path="$1"
    local work_dir="$2"
    local binary_name="$3"
    local list_path extracted_path

    # Find the first file path in the archive whose basename matches binary_name.
    list_path="$(
        tar -tzf "${archive_path}" \
            | awk -v b="${binary_name}" '{
                n = $0
                sub(/.*\//, "", n)
                if (n == b) { print $0; exit }
              }'
    )"
    [[ -n "${list_path}" ]] || kkfly::install::die "Binary ${binary_name} not found in archive"

    tar -xzf "${archive_path}" -C "${work_dir}" "${list_path}"
    extracted_path="${work_dir}/${list_path}"
    [[ -f "${extracted_path}" ]] || kkfly::install::die "Extracted binary not found: ${extracted_path}"
    chmod 0755 "${extracted_path}" || true
    echo "${extracted_path}"
}

kkfly::install::install_binary() {
    local src="$1"
    local dest="$2"

    mkdir -p "$(dirname "${dest}")"

    if install -m 0755 "${src}" "${dest}" 2>/dev/null; then
        return 0
    fi

    if ! command -v sudo >/dev/null 2>&1; then
        kkfly::install::die "Permission denied installing to ${dest} (try --bin-dir)"
    fi

    # Non-interactive sudo; fails fast if password is required.
    sudo -n install -m 0755 "${src}" "${dest}" \
        || kkfly::install::die "sudo failed installing to ${dest} (ensure NOPASSWD or use --bin-dir)"
}

kkfly::install::parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --repo)
                [[ $# -ge 2 ]] || kkfly::install::die "--repo requires a value"
                KKFLY_REPO="$2"
                shift 2
                ;;
            --version)
                [[ $# -ge 2 ]] || kkfly::install::die "--version requires a value"
                KKFLY_VERSION="$2"
                shift 2
                ;;
            --bin-dir)
                [[ $# -ge 2 ]] || kkfly::install::die "--bin-dir requires a value"
                KKFLY_BIN_DIR="$2"
                shift 2
                ;;
            --verbose)
                KKFLY_VERBOSE="1"
                shift
                ;;
            -h | --help)
                kkfly::install::usage
                exit 0
                ;;
            *)
                kkfly::install::die "Unknown argument: $1 (use --help)"
                ;;
        esac
    done
}

kkfly::install::install_deps_centos() {
    if (command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1) && command -v tar >/dev/null 2>&1; then
        return 0
    fi
    echo "Installing dependencies..."
    local sudo_cmd
    sudo_cmd="$(kkfly::install::sudo_prefix)"
    if command -v dnf >/dev/null 2>&1; then
        ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1 && ${sudo_cmd} dnf install -y curl
        ! command -v tar >/dev/null 2>&1 && ${sudo_cmd} dnf install -y tar
        ! command -v awk >/dev/null 2>&1 && ${sudo_cmd} dnf install -y gawk
        ! command -v install >/dev/null 2>&1 && ${sudo_cmd} dnf install -y coreutils
        ! command -v sha256sum >/dev/null 2>&1 && ${sudo_cmd} dnf install -y coreutils
    else
        ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1 && ${sudo_cmd} yum install -y curl
        ! command -v tar >/dev/null 2>&1 && ${sudo_cmd} yum install -y tar
        ! command -v awk >/dev/null 2>&1 && ${sudo_cmd} yum install -y gawk
        ! command -v install >/dev/null 2>&1 && ${sudo_cmd} yum install -y coreutils
        ! command -v sha256sum >/dev/null 2>&1 && ${sudo_cmd} yum install -y coreutils
    fi
}

kkfly::install::install_deps_debian() {
    if (command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1) && command -v tar >/dev/null 2>&1; then
        return 0
    fi
    echo "Installing dependencies..."
    local sudo_cmd
    sudo_cmd="$(kkfly::install::sudo_prefix)"
    ${sudo_cmd} apt-get update -qq
    ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1 && ${sudo_cmd} apt-get install -y curl
    ! command -v tar >/dev/null 2>&1 && ${sudo_cmd} apt-get install -y tar
    ! command -v awk >/dev/null 2>&1 && ${sudo_cmd} apt-get install -y gawk
    ! command -v install >/dev/null 2>&1 && ${sudo_cmd} apt-get install -y coreutils
    ! command -v sha256sum >/dev/null 2>&1 && ${sudo_cmd} apt-get install -y coreutils
}

kkfly::install::install_deps_mac() {
    if command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        return 0
    fi
    echo "Installing dependencies..."
    if ! command -v brew >/dev/null 2>&1; then
        echo "Installing Homebrew..."
        /bin/bash -c "$(curl -fL -sS https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    fi
    ! command -v curl >/dev/null 2>&1 && brew install curl
    ! command -v gtar >/dev/null 2>&1 && brew install gnu-tar
    ! command -v sha256sum >/dev/null 2>&1 && brew install coreutils
}

kkfly::install::run() {
    local platform='debian'
    command -v yum >/dev/null && platform='centos'
    command -v dnf >/dev/null && platform='centos'
    command -v brew >/dev/null && platform='mac'
    eval "${FUNCNAME/::run/::${platform}}"
}

kkfly::install::centos() {
    kkfly::install::install_deps_centos
    kkfly::install::install
}

kkfly::install::debian() {
    kkfly::install::install_deps_debian
    kkfly::install::install
}

kkfly::install::mac() {
    kkfly::install::install_deps_mac
    kkfly::install::install
}

kkfly::install::install() {
    echo "Installing kkfly..."
    kkfly::install::need_cmd tar
    kkfly::install::need_cmd awk
    kkfly::install::need_cmd install

    read -r OS ARCH < <(kkfly::install::detect_platform)
    [[ "${OS}" != "windows" ]] || kkfly::install::die "Windows install via this script is not supported. Please download the release asset manually."

    local tag version_for_asset asset_name base_url asset_url checksums_url
    if [[ -z "${KKFLY_VERSION}" ]]; then
        tag="$(kkfly::install::resolve_latest_tag "${KKFLY_REPO}")" \
            || kkfly::install::die "Unable to resolve latest release tag (check GitHub connectivity)"
    else
        tag="$(kkfly::install::normalize_tag "${KKFLY_VERSION}")"
    fi

    version_for_asset="${tag#v}"
    asset_name="kkfly_${version_for_asset}_${OS}_${ARCH}.tar.gz"
    base_url="https://github.com/${KKFLY_REPO}/releases/download/${tag}"
    asset_url="${base_url}/${asset_name}"
    checksums_url="${base_url}/checksums.txt"

    kkfly::install::log "Release tag: ${tag}"
    kkfly::install::log "Asset: ${asset_url}"

    local tmpdir archive_path checksums_path extracted_bin
    tmpdir="$(mktemp -d -t kkfly-install.XXXXXX)"
    trap 'rm -rf "${tmpdir}"' EXIT

    archive_path="${tmpdir}/${asset_name}"
    checksums_path="${tmpdir}/checksums.txt"

    echo "Downloading ${asset_name}..."
    kkfly::install::download_file "${asset_url}" "${archive_path}"
    echo "Downloading checksums.txt..."
    kkfly::install::download_file "${checksums_url}" "${checksums_path}"

    kkfly::install::verify_checksum "${archive_path}" "${checksums_path}"

    extracted_bin="$(kkfly::install::extract_tar_gz_binary "${archive_path}" "${tmpdir}" "kkfly")"
    kkfly::install::install_binary "${extracted_bin}" "${KKFLY_BIN_DIR}/kkfly"

    echo "Installed kkfly to ${KKFLY_BIN_DIR}/kkfly"
}

kkfly::install::parse_args "$@"
kkfly::install::run "$@"
