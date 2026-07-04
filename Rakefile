# frozen_string_literal: true

# Copyright (c) 2026 kk — MIT License
#
# push (default): test → commit/push → tag → push tag → GitHub Release
#
# Env:
#   SKIP_TEST=1      skip go test
#   SKIP_RELEASE=1   commit/push only, no tag
#   VERSION=0.1.17   pin release tag (default: bump patch from latest v* tag)
#   KK_GIT_DRY_RUN=1 dry-run git steps (kk-git)

require 'bundler/setup'
require 'kk/git/rake_tasks'

$LOAD_PATH.unshift(File.expand_path('lib', __dir__))
require 'kkfly/release'

def run!(cmd)
  puts "==> #{cmd}"
  system(cmd) || abort("failed: #{cmd}")
end

namespace :test do
  desc 'Run Go tests'
  task :go do
    next if ENV['SKIP_TEST'] == '1'

    run! 'go test ./...'
  end
end

namespace :release do
  desc 'Create and push next semver tag (triggers GoReleaser)'
  task :tag_push do
    next if ENV['SKIP_RELEASE'] == '1'

    Kkfly::Release.tag_and_push!
  end
end

desc 'Test, commit/push, then tag release'
task push: %w[test:go git:auto_commit_push release:tag_push]

task default: [:push]
