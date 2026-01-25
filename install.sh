#!/bin/sh
# This installer is a tiny POSIX-sh wrapper that execs Ruby.
# Ruby will start reading the script from the `#!ruby` marker via `-x`.
exec ruby -x "$0" "$@"
#!ruby
# frozen_string_literal: true

require "digest"
require "fileutils"
require "json"
require "net/http"
require "optparse"
require "rbconfig"
require "rubygems/package"
require "tmpdir"
require "uri"
require "zlib"

class Installer
  DEFAULT_REPO = "kevin197011/kkfly"

  def initialize(argv)
    @repo = DEFAULT_REPO
    @version = nil
    @bin_dir = "/usr/local/bin"
    @verbose = false

    OptionParser.new do |o|
      o.banner = "Usage: install.sh [options]"
      o.on("--repo REPO", "GitHub repo, e.g. owner/name (default: #{DEFAULT_REPO})") { |v| @repo = v }
      o.on("--version VERSION", "Release version/tag (default: latest). Accepts 1.2.3 or v1.2.3.") { |v| @version = v }
      o.on("--bin-dir DIR", "Install directory (default: #{@bin_dir})") { |v| @bin_dir = v }
      o.on("--verbose", "Verbose output") { @verbose = true }
    end.parse!(argv)
  end

  def run
    os, arch = detect_platform
    if os == "windows"
      abort "Windows install via this script is not supported. Please download the release asset manually."
    end

    release = fetch_release
    version_for_asset = release.fetch("tag_name").sub(/\Av/, "")
    asset_name = "kkfly_#{version_for_asset}_#{os}_#{arch}.tar.gz"
    asset = release.fetch("assets").find { |a| a["name"] == asset_name }
    abort "No asset found for #{os}/#{arch}: #{asset_name}" unless asset

    checksums_asset = release.fetch("assets").find { |a| a["name"] == "checksums.txt" }
    abort "Missing checksums.txt in release assets" unless checksums_asset

    Dir.mktmpdir("kkfly-install") do |dir|
      archive_path = File.join(dir, asset_name)
      checksums_path = File.join(dir, "checksums.txt")

      download(asset["browser_download_url"], archive_path)
      download(checksums_asset["browser_download_url"], checksums_path)

      verify_checksum!(archive_path, checksums_path)

      extracted_bin = extract_tar_gz_binary!(archive_path, dir, "kkfly")
      install_binary!(extracted_bin, File.join(@bin_dir, "kkfly"))
    end

    puts "Installed kkfly to #{@bin_dir}/kkfly"
  end

  private

  def detect_platform
    host_os = RbConfig::CONFIG["host_os"]
    host_cpu = RbConfig::CONFIG["host_cpu"]

    os =
      case host_os
      when /darwin/ then "darwin"
      when /linux/ then "linux"
      when /mswin|mingw|cygwin/ then "windows"
      else
        abort "Unsupported OS: #{host_os}"
      end

    arch =
      case host_cpu
      when /x86_64|amd64/ then "amd64"
      when /aarch64|arm64/ then "arm64"
      else
        abort "Unsupported CPU arch: #{host_cpu}"
      end

    [os, arch]
  end

  def fetch_release
    if @version.nil? || @version.strip.empty?
      url = "https://api.github.com/repos/#{@repo}/releases/latest"
    else
      tag = @version.strip
      tag = "v#{tag}" unless tag.start_with?("v")
      url = "https://api.github.com/repos/#{@repo}/releases/tags/#{tag}"
    end

    json_get(url)
  end

  def json_get(url)
    uri = URI(url)
    req = Net::HTTP::Get.new(uri)
    req["User-Agent"] = "kkfly-installer"
    req["Accept"] = "application/vnd.github+json"

    res = http_request(uri, req)
    unless res.is_a?(Net::HTTPSuccess)
      abort "GitHub API request failed: #{res.code} #{res.message}\n#{res.body}"
    end
    JSON.parse(res.body)
  end

  def download(url, dest_path)
    uri = URI(url)
    req = Net::HTTP::Get.new(uri)
    req["User-Agent"] = "kkfly-installer"
    req["Accept"] = "application/octet-stream"

    res = http_request(uri, req)
    unless res.is_a?(Net::HTTPSuccess)
      abort "Download failed: #{res.code} #{res.message} (#{url})"
    end

    File.open(dest_path, "wb") { |f| f.write(res.body) }
    puts "Downloaded #{File.basename(dest_path)}" if @verbose
  end

  def http_request(uri, req, limit = 5)
    abort "Too many redirects" if limit <= 0

    Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https") do |http|
      res = http.request(req)
      case res
      when Net::HTTPRedirection
        next_uri = URI(res["location"])
        next_req = Net::HTTP::Get.new(next_uri)
        next_req["User-Agent"] = req["User-Agent"]
        next_req["Accept"] = req["Accept"]
        return http_request(next_uri, next_req, limit - 1)
      else
        return res
      end
    end
  end

  def verify_checksum!(archive_path, checksums_path)
    asset_name = File.basename(archive_path)
    expected = nil
    File.readlines(checksums_path, chomp: true).each do |line|
      next if line.strip.empty?
      sha, name = line.split(/\s+/, 2)
      next unless name
      name = name.strip
      if name == asset_name
        expected = sha
        break
      end
    end
    abort "checksums.txt does not contain #{asset_name}" unless expected

    actual = Digest::SHA256.file(archive_path).hexdigest
    abort "Checksum mismatch for #{asset_name}" unless secure_eq(expected, actual)
  end

  def secure_eq(a, b)
    return false unless a.bytesize == b.bytesize
    acc = 0
    a.bytes.zip(b.bytes).each { |x, y| acc |= (x ^ y) }
    acc.zero?
  end

  def extract_tar_gz_binary!(archive_path, work_dir, binary_name)
    out_path = File.join(work_dir, binary_name)
    found = false

    Zlib::GzipReader.open(archive_path) do |gz|
      Gem::Package::TarReader.new(gz) do |tar|
        tar.each do |entry|
          next unless entry.file?
          next unless File.basename(entry.full_name) == binary_name
          File.open(out_path, "wb") { |f| f.write(entry.read) }
          FileUtils.chmod(0o755, out_path)
          found = true
          break
        end
      end
    end

    abort "Binary #{binary_name} not found in archive" unless found
    out_path
  end

  def install_binary!(src, dest)
    FileUtils.mkdir_p(File.dirname(dest))
    begin
      FileUtils.cp(src, dest)
      FileUtils.chmod(0o755, dest)
      return
    rescue Errno::EACCES
      # fall through to sudo
    end

    sudo = `command -v sudo 2>/dev/null`.strip
    abort "Permission denied installing to #{dest} (try --bin-dir)" if sudo.empty?

    # Non-interactive sudo; will fail fast if password is required.
    ok = system("sudo", "-n", "install", "-m", "0755", src, dest)
    abort "sudo failed installing to #{dest} (ensure NOPASSWD or use --bin-dir)" unless ok
  end
end

Installer.new(ARGV).run

