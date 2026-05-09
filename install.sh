#!/usr/bin/env bash
# Copyright (c) 2026 kk
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT

set -o errexit
set -o nounset
set -o pipefail

# --- vars ---
readonly REPO="kevin197011/kkfly"
readonly BINARY_NAME="kkfly"
readonly INSTALL_DIR="/usr/local/bin"
readonly INSTALL_PATH="${INSTALL_DIR}/${BINARY_NAME}"

kkfly::install::detect_platform() {
    if command -v brew >/dev/null 2>&1; then
        echo "mac"
    elif command -v yum >/dev/null 2>&1 || command -v dnf >/dev/null 2>&1; then
        echo "centos"
    else
        echo "debian"
    fi
}

# --- run code ---
kkfly::install::run() {
    local platform
    platform="$(kkfly::install::detect_platform)"
    
    if [[ "${platform}" != "mac" && "$EUID" -ne 0 ]]; then
        echo "❌ Error: Please run as root (use sudo)"
        exit 1
    fi

    "kkfly::install::${platform}" "$@"
}

kkfly::install::centos() { kkfly::install::common; }
kkfly::install::debian() { kkfly::install::common; }
kkfly::install::mac()    { kkfly::install::common; }

# --- common code ---
kkfly::install::common() {
    local tmp_dir
    tmp_dir=$(mktemp -d -t kkfly_XXXXXX)

    trap '[[ -n "${tmp_dir:-}" ]] && rm -rf "${tmp_dir}"' EXIT
    
    cd "${tmp_dir}"

    echo "🔍 Checking latest version..."
    local version
    version=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"v?([^"]+)".*/\1/')
    
    if [[ -z "${version}" ]]; then
        version="0.1.13"
        echo "⚠️  Fallback to version ${version}"
    else
        echo "✨ Found latest version: v${version}"
    fi

    local arch
    arch=$(uname -m)
    case "${arch}" in
        x86_64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) echo "❌ Unsupported architecture: ${arch}"; exit 1 ;;
    esac

    local filename="${BINARY_NAME}_${version}_linux_${arch}.tar.gz"
    [[ "$(uname)" == "Darwin" ]] && filename="${BINARY_NAME}_${version}_darwin_${arch}.tar.gz"

    local url="https://github.com/${REPO}/releases/download/v${version}/${filename}"

    echo "📥 Downloading from GitHub..."
    curl -fsSL "${url}" -o "${filename}"

    echo "📦 Extracting..."
    tar -zxf "${filename}"

    local target
    target="./${BINARY_NAME}"
    if [[ ! -f "${target}" || ! -x "${target}" ]]; then
        target="$(tar -tzf "${filename}" | awk -v name="${BINARY_NAME}" -F/ '$NF == name { print; exit }')"
        target="./${target}"
    fi

    if [[ ! -f "${target}" || ! -x "${target}" ]]; then
        echo "❌ Error: Binary ${BINARY_NAME} not found"
        exit 1
    fi

    echo "🔧 Installing to ${INSTALL_PATH}..."
    install -m 755 "${target}" "${INSTALL_PATH}"

    echo "--------------------------------------------------"
    if command -v "${BINARY_NAME}" >/dev/null; then
        echo "✅ ${BINARY_NAME} installed successfully!"
        echo "🚀 Version: $(${BINARY_NAME} --version 2>/dev/null || echo "${version}")"
    else
        echo "❌ Installation failed."
        exit 1
    fi
}

kkfly::install::run "$@"