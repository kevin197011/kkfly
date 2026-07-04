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
import warnings
from pathlib import Path

REPO = "kevin197011/kkfly"
BINARY = "kkfly"
INSTALLER_REV = "20260705"
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
    # Read only BINARY from the .tar.gz — no tarfile.extract(), avoids RHEL 3.9 warnings.
    binary = dest_dir / BINARY
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", RuntimeWarning)
        with tarfile.open(archive, "r:gz") as tar:
            try:
                member = tar.getmember(BINARY)
            except KeyError:
                die(f"binary {BINARY} not found in archive")
            if not member.isfile() or member.name != BINARY:
                die(f"unsafe archive entry: {member.name!r}")
            src = tar.extractfile(member)
            if src is None:
                die(f"could not read {BINARY} from archive")
            with binary.open("wb") as out:
                shutil.copyfileobj(src, out)
    return binary


def install_binary(src: Path, dest: Path) -> None:
    shutil.copy2(src, dest)
    dest.chmod(dest.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


PATH_MARKER = "# kkfly installer"


def in_current_path(install_dir: Path) -> bool:
    return str(install_dir) in os.environ.get("PATH", "").split(":")


def path_export_line(install_dir: Path) -> str:
    return f'export PATH="{install_dir}:$PATH"'


def shell_rc_candidates(install_dir: Path) -> list[Path]:
    home = Path.home()
    shell = Path(os.environ.get("SHELL", "")).name
    files: list[Path] = []
    if os.geteuid() == 0 and install_dir == DEFAULT_INSTALL_DIR:
        files.append(Path("/etc/profile.d/kkfly-path.sh"))
    if shell == "zsh":
        files.append(home / ".zshrc")
    files.extend([home / ".bashrc", home / ".bash_profile", home / ".profile"])
    return files


def rc_has_path_entry(rc: Path, install_dir: Path) -> bool:
    if not rc.is_file():
        return False
    text = rc.read_text()
    return PATH_MARKER in text or str(install_dir) in text


def append_path_to_rc(rc: Path, install_dir: Path) -> None:
    line = path_export_line(install_dir)
    rc.parent.mkdir(parents=True, exist_ok=True)
    needs_nl = rc.is_file() and rc.stat().st_size > 0
    with rc.open("a") as f:
        if needs_nl:
            f.write("\n")
        f.write(f"{PATH_MARKER}\n{line}\n")


def prepend_path_now(install_dir: Path) -> None:
    d = str(install_dir.resolve())
    parts = [p for p in os.environ.get("PATH", "").split(":") if p]
    if d in parts:
        return
    os.environ["PATH"] = f"{d}:{os.environ.get('PATH', '')}"


def persist_path(install_dir: Path) -> Path | None:
    line = path_export_line(install_dir)
    body = f"{PATH_MARKER}\n{line}\n"

    for rc in shell_rc_candidates(install_dir):
        try:
            rc.parent.mkdir(parents=True, exist_ok=True)
            if rc.parent == Path("/etc/profile.d"):
                rc.write_text(body)
                rc.chmod(0o644)
            else:
                append_path_to_rc(rc, install_dir)
            return rc
        except OSError:
            continue
    return None


def ensure_path(install_dir: Path) -> None:
    if os.environ.get("SKIP_PATH") == "1":
        return

    install_dir = install_dir.resolve()
    if in_current_path(install_dir):
        return

    for rc in shell_rc_candidates(install_dir):
        if rc_has_path_entry(rc, install_dir):
            prepend_path_now(install_dir)
            info(f"PATH already configured in {rc}")
            return

    rc = persist_path(install_dir)
    prepend_path_now(install_dir)

    if rc:
        info(f"added {install_dir} to PATH ({rc})")
        if str(rc).startswith("/etc/profile.d/"):
            print("    new login shells will pick this up automatically")
        else:
            print(f"    run: source {rc}   # or open a new shell")
    elif in_current_path(install_dir):
        info(f"PATH updated for this session ({install_dir})")
    else:
        die(f"could not configure PATH for {install_dir}")


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

    info(f"installer rev {INSTALLER_REV}")

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
    ensure_path(install_dir)


if __name__ == "__main__":
    main()
