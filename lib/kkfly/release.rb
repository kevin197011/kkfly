# frozen_string_literal: true

require 'open3'

module Kkfly
  # Tag + push to trigger .github/workflows/release.yml (GoReleaser).
  module Release
    REPO = 'kevin197011/kkfly'
    TAG_PATTERN = /\Av\d+\.\d+\.\d+\z/

    module_function

    def dry_run?
      ENV['KK_GIT_DRY_RUN'] == '1'
    end

    def remote
      ENV.fetch('KK_GIT_REMOTE', 'origin')
    end

    def run_git!(*args)
      if dry_run?
        puts "[dry-run] git #{args.join(' ')}"
        return true
      end

      stdout, stderr, ok = Open3.capture3('git', *args)
      return true if ok.success?

      msg = +"git #{args.join(' ')} failed"
      msg << "\n#{stderr}" unless stderr.strip.empty?
      msg << "\n#{stdout}" unless stdout.strip.empty?
      raise msg
    end

    def git_output(*args)
      stdout, stderr, ok = Open3.capture3('git', *args)
      raise "git #{args.join(' ')} failed: #{stderr}" unless ok.success?

      stdout.strip
    end

    def semver_tags
      git_output('tag', '-l', 'v*', '--sort=-v:refname').lines.map(&:strip).grep(TAG_PATTERN)
    end

    def tags_at_head
      git_output('tag', '--points-at', 'HEAD').lines.map(&:strip).grep(TAG_PATTERN)
    end

    def tag_exists?(tag)
      _, _, ok = Open3.capture3('git', 'rev-parse', '--verify', "refs/tags/#{tag}")
      ok.success?
    end

    def head_commit
      git_output('rev-parse', 'HEAD')
    end

    def tag_ref_commit(tag)
      git_output('rev-list', '-n', '1', tag)
    end

    def bump_patch(tag)
      major, minor, patch = tag.delete_prefix('v').split('.').map(&:to_i)
      "v#{major}.#{minor}.#{patch + 1}"
    end

    def next_tag
      if (v = ENV['VERSION'].to_s.strip.delete_prefix('v')) && !v.empty?
        tag = "v#{v}"
        raise "invalid VERSION (want semver): #{ENV['VERSION']}" unless tag.match?(TAG_PATTERN)

        return tag
      end

      tag = semver_tags.first ? bump_patch(semver_tags.first) : 'v0.1.0'
      while tag_exists?(tag) && tag_ref_commit(tag) != head_commit
        tag = bump_patch(tag)
      end
      tag
    end

    def tag_and_push!
      at_head = tags_at_head
      if at_head.any? && ENV['VERSION'].to_s.strip.empty?
        puts "==> HEAD already tagged #{at_head.join(', ')} — skip release"
        return :skipped
      end

      tag = next_tag
      if tag_exists?(tag)
        if tag_ref_commit(tag) == head_commit
          puts "==> already released as #{tag} — skip"
          return :skipped
        end
        raise "tag #{tag} already exists on another commit"
      end

      puts "==> creating tag #{tag}"
      run_git!('tag', '-a', tag, '-m', "Release #{tag}")

      puts "==> pushing tag to #{remote}"
      run_git!('push', remote, tag)

      puts "==> release pipeline triggered (GoReleaser on GitHub Actions)"
      puts "    actions  https://github.com/#{REPO}/actions"
      puts "    release  https://github.com/#{REPO}/releases/tag/#{tag}"
      :released
    end
  end
end
