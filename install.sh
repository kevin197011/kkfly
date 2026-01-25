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

kkfly::install::json_tag_name() {
    if command -v jq >/dev/null 2>&1; then
        jq -r '.tag_name // empty'
        return 0
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - <<'PY'
import json, sys
data = json.load(sys.stdin)
print(data.get("tag_name", ""))
PY
        return 0
    fi
    if command -v python >/dev/null 2>&1; then
        python - <<'PY'
import json, sys
data = json.load(sys.stdin)
print(data.get("tag_name", ""))
PY
        return 0
    fi
    kkfly::install::die "Need jq or python3 to parse GitHub API JSON"
}

kkfly::install::json_asset_url_by_name() {
    local asset_name="$1"
    if command -v jq >/dev/null 2>&1; then
        jq -r --arg name "${asset_name}" '.assets[]? | select(.name == $name) | .browser_download_url // empty' | head -n1
        return 0
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - "${asset_name}" <<'PY'
import json, sys
name = sys.argv[1]
data = json.load(sys.stdin)
for a in data.get("assets", []) or []:
  if a.get("name") == name:
    print(a.get("browser_download_url", ""))
    break
PY
        return 0
    fi
    if command -v python >/dev/null 2>&1; then
        python - "${asset_name}" <<'PY'
import json, sys
name = sys.argv[1]
data = json.load(sys.stdin)
for a in data.get("assets", []) or []:
  if a.get("name") == name:
    print(a.get("browser_download_url", ""))
    break
PY
        return 0
    fi
    kkfly::install::die "Need jq or python3 to parse GitHub API JSON"
}

kkfly::install::download_cmd() {
    if command -v curl >/dev/null 2>&1; then
        echo "curl -fsSL --retry 3 --retry-delay 1 -L"
        return 0
    fi
    if command -v wget >/dev/null 2>&1; then
        echo "wget -qO-"
        return 0
    fi
    return 1
}

kkfly::install::github_api_get() {
    local url="$1"
    local cmd
    cmd="$(kkfly::install::download_cmd)" || kkfly::install::die "Error: curl or wget is required"
    # GitHub API requires headers; wget cannot set these easily in a portable way.
    if [[ "${cmd}" == curl* ]]; then
        curl -fsSL --retry 3 --retry-delay 1 -L \
            -H "User-Agent: kkfly-installer" \
            -H "Accept: application/vnd.github+json" \
            "${url}"
        return 0
    fi
    kkfly::install::die "Error: curl is required for GitHub API requests"
}

kkfly::install::download_file() {
    local url="$1"
    local dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 --retry-delay 1 -L -o "${dest}" \
            -H "User-Agent: kkfly-installer" \
            -H "Accept: application/octet-stream" \
            "${url}"
        kkfly::install::log "Downloaded $(basename "${dest}")"
        return 0
    fi
    if command -v wget >/dev/null 2>&1; then
        wget -qO "${dest}" "${url}"
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
    if command -v python3 >/dev/null 2>&1 && command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        return 0
    fi
    echo "Installing dependencies..."
    if command -v dnf >/dev/null 2>&1; then
        ! command -v python3 >/dev/null 2>&1 && sudo dnf install -y python3
        ! command -v curl >/dev/null 2>&1 && sudo dnf install -y curl
        ! command -v tar >/dev/null 2>&1 && sudo dnf install -y tar
        ! command -v awk >/dev/null 2>&1 && sudo dnf install -y gawk
        ! command -v install >/dev/null 2>&1 && sudo dnf install -y coreutils
        ! command -v sha256sum >/dev/null 2>&1 && sudo dnf install -y coreutils
    else
        ! command -v python3 >/dev/null 2>&1 && sudo yum install -y python3
        ! command -v curl >/dev/null 2>&1 && sudo yum install -y curl
        ! command -v tar >/dev/null 2>&1 && sudo yum install -y tar
        ! command -v awk >/dev/null 2>&1 && sudo yum install -y gawk
        ! command -v install >/dev/null 2>&1 && sudo yum install -y coreutils
        ! command -v sha256sum >/dev/null 2>&1 && sudo yum install -y coreutils
    fi
}

kkfly::install::install_deps_debian() {
    if command -v python3 >/dev/null 2>&1 && command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        return 0
    fi
    echo "Installing dependencies..."
    sudo apt-get update -qq
    ! command -v python3 >/dev/null 2>&1 && sudo apt-get install -y python3
    ! command -v curl >/dev/null 2>&1 && sudo apt-get install -y curl
    ! command -v tar >/dev/null 2>&1 && sudo apt-get install -y tar
    ! command -v awk >/dev/null 2>&1 && sudo apt-get install -y gawk
    ! command -v install >/dev/null 2>&1 && sudo apt-get install -y coreutils
    ! command -v sha256sum >/dev/null 2>&1 && sudo apt-get install -y coreutils
}

kkfly::install::install_deps_mac() {
    if command -v python3 >/dev/null 2>&1 && command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        return 0
    fi
    echo "Installing dependencies..."
    if ! command -v brew >/dev/null 2>&1; then
        echo "Installing Homebrew..."
        /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    fi
    ! command -v python3 >/dev/null 2>&1 && brew install python3
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
    kkfly::install::need_cmd tar
    kkfly::install::need_cmd awk
    kkfly::install::need_cmd install

    read -r OS ARCH < <(kkfly::install::detect_platform)
    [[ "${OS}" != "windows" ]] || kkfly::install::die "Windows install via this script is not supported. Please download the release asset manually."

    local release_url tag release_json tag_name version_for_asset asset_name asset_url checksums_url
    if [[ -z "${KKFLY_VERSION}" ]]; then
        release_url="https://api.github.com/repos/${KKFLY_REPO}/releases/latest"
    else
        tag="${KKFLY_VERSION}"
        [[ "${tag}" == v* ]] || tag="v${tag}"
        release_url="https://api.github.com/repos/${KKFLY_REPO}/releases/tags/${tag}"
    fi

    release_json="$(kkfly::install::github_api_get "${release_url}")"
    tag_name="$(printf '%s' "${release_json}" | kkfly::install::json_tag_name)"
    [[ -n "${tag_name}" ]] || kkfly::install::die "Unable to read tag_name from GitHub API response"
    version_for_asset="${tag_name#v}"

    asset_name="kkfly_${version_for_asset}_${OS}_${ARCH}.tar.gz"
    asset_url="$(printf '%s' "${release_json}" | kkfly::install::json_asset_url_by_name "${asset_name}")"
    [[ -n "${asset_url}" ]] || kkfly::install::die "No asset found for ${OS}/${ARCH}: ${asset_name}"

    checksums_url="$(printf '%s' "${release_json}" | kkfly::install::json_asset_url_by_name "checksums.txt")"
    [[ -n "${checksums_url}" ]] || kkfly::install::die "Missing checksums.txt in release assets"

    local tmpdir archive_path checksums_path extracted_bin
    tmpdir="$(mktemp -d -t kkfly-install.XXXXXX)"
    trap 'rm -rf "${tmpdir}"' EXIT

    archive_path="${tmpdir}/${asset_name}"
    checksums_path="${tmpdir}/checksums.txt"

    kkfly::install::download_file "${asset_url}" "${archive_path}"
    kkfly::install::download_file "${checksums_url}" "${checksums_path}"

    kkfly::install::verify_checksum "${archive_path}" "${checksums_path}"

    extracted_bin="$(kkfly::install::extract_tar_gz_binary "${archive_path}" "${tmpdir}" "kkfly")"
    kkfly::install::install_binary "${extracted_bin}" "${KKFLY_BIN_DIR}/kkfly"

    echo "Installed kkfly to ${KKFLY_BIN_DIR}/kkfly"
}

kkfly::install::parse_args "$@"
kkfly::install::run "$@"
