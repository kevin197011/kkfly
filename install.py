#!/usr/bin/env python3
# Copyright (c) 2026 kk — MIT License
# https://opensource.org/licenses/MIT
"""Install kkfly from GitHub Releases.

  curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.py | python3
  VERSION=0.1.19 curl -fsSL .../install.py | python3
  python3 install.py -v 0.1.19
"""

from __future__ import annotations

import argparse
import hashlib
import os
import platform
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

REPO = "kevin197011/kkfly"
BINARY = "kkfly"
DEFAULT_INSTALL_DIR = Path("/usr/local/bin")


def info(msg: str) -> None:
    print(f"==> {msg}")


def die(msg: str, code: int = 1) -> None:
    print(f"error: {msg}", file=sys.stderr)
    raise SystemExit(code)


def detect_os() -> str:
    system = platform.system()
    if system == "Linux":
        return "linux"
    if system == "Darwin":
        return "darwin"
    die(f"unsupported OS: {system}")


def detect_arch() -> str:
    machine = platform.machine().lower()
    if machine in ("x86_64", "amd64"):
        return "amd64"
    if machine in ("aarch64", "arm64"):
        return "arm64"
    die(f"unsupported arch: {platform.machine()}")


def normalize_version(raw: str) -> str:
    version = raw.strip().removeprefix("v")
    if not version:
        die("empty version")
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        die(f"invalid version (want semver): {raw}")
    return version


def latest_version() -> str:
    url = f"https://github.com/{REPO}/releases/latest"
    try:
        with urllib.request.urlopen(url) as resp:
            tag = resp.url.rsplit("/tag/", 1)[-1]
    except urllib.error.URLError as exc:
        die(f"could not resolve latest release: {exc}")
    tag = tag.removeprefix("v")
    if not tag or tag == url:
        die("could not resolve latest release")
    return tag


def resolve_install_dir(requested: Path | None) -> Path:
    install_dir = requested or Path(os.environ.get("INSTALL_DIR", DEFAULT_INSTALL_DIR))

    if install_dir.exists() and os.access(install_dir, os.W_OK):
        return install_dir

    if os.geteuid() == 0:
        install_dir.mkdir(parents=True, exist_ok=True)
        return install_dir

    if platform.system() == "Darwin":
        fallback = Path.home() / ".local" / "bin"
        fallback.mkdir(parents=True, exist_ok=True)
        info(f"using {fallback} ({install_dir} not writable)")
        return fallback

    die(f"cannot write to {install_dir} — re-run with sudo or set INSTALL_DIR")


def download(url: str, dest: Path) -> None:
    try:
        with urllib.request.urlopen(url) as resp, dest.open("wb") as out:
            shutil.copyfileobj(resp, out)
    except urllib.error.URLError as exc:
        die(f"download failed: {exc}")


def expected_sha256(checksums_text: str, archive: str) -> str:
    for line in checksums_text.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == archive:
            return parts[0].lower()
    die(f"checksum entry not found for {archive}")


def verify_sha256(path: Path, digest: str) -> None:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    if h.hexdigest().lower() != digest:
        die("checksum verification failed")


def extract_binary(archive: Path, dest_dir: Path) -> Path:
    with tarfile.open(archive, "r:gz") as tar:
        try:
            member = tar.getmember(BINARY)
        except KeyError:
            die(f"binary {BINARY} not found in archive")
        member.name = BINARY
        if sys.version_info >= (3, 12):
            tar.extract(member, dest_dir, filter="data")
        else:
            tar.extract(member, dest_dir)
    binary = dest_dir / BINARY
    if not binary.is_file():
        die(f"binary {BINARY} not found in archive")
    return binary


def install_binary(src: Path, dest: Path) -> None:
    shutil.copy2(src, dest)
    dest.chmod(dest.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def path_hint(install_dir: Path) -> None:
    path_entries = os.environ.get("PATH", "").split(":")
    if str(install_dir) in path_entries:
        return
    print()
    print(f"note: add {install_dir} to PATH:")
    print(f'  export PATH="{install_dir}:$PATH"')


def installed_version(install_path: Path, fallback: str) -> str:
    try:
        out = subprocess.check_output(
            [str(install_path), "--version"], stderr=subprocess.DEVNULL, text=True
        )
        return out.strip() or fallback
    except (OSError, subprocess.CalledProcessError):
        return fallback


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description="Install kkfly from GitHub Releases")
    parser.add_argument("-v", "--version", help="release version (e.g. 0.1.19 or v0.1.19)")
    args = parser.parse_args(argv)

    version = args.version or os.environ.get("VERSION", "")
    install_dir = resolve_install_dir(
        Path(os.environ["INSTALL_DIR"]) if os.environ.get("INSTALL_DIR") else None
    )
    install_path = install_dir / BINARY

    os_name = detect_os()
    arch = detect_arch()

    if version:
        version = normalize_version(version)
    else:
        info("resolving latest release")
        version = latest_version()

    archive_name = f"{BINARY}_{version}_{os_name}_{arch}.tar.gz"
    archive_url = f"https://github.com/{REPO}/releases/download/v{version}/{archive_name}"
    checksums_url = f"https://github.com/{REPO}/releases/download/v{version}/checksums.txt"

    with tempfile.TemporaryDirectory(prefix="kkfly_install_") as tmp:
        tmp_dir = Path(tmp)
        archive_path = tmp_dir / archive_name
        checksums_path = tmp_dir / "checksums.txt"

        info(f"downloading v{version} ({os_name}/{arch})")
        download(archive_url, archive_path)

        info("verifying checksum")
        download(checksums_url, checksums_path)
        digest = expected_sha256(checksums_path.read_text(), archive_name)
        verify_sha256(archive_path, digest)

        info(f"installing to {install_path}")
        binary = extract_binary(archive_path, tmp_dir)
        install_binary(binary, install_path)

    label = installed_version(install_path, f"v{version}")
    print()
    print(f"installed  {label}  →  {install_path}")
    path_hint(install_dir)


if __name__ == "__main__":
    main()
