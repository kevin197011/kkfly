# frozen_string_literal: true

# Copyright (c) 2025 kk
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT

require 'time'
require 'open3'

task default: %w[push]

# 获取并递增语义化版本 tag（vMAJOR.MINOR.PATCH）
def next_semver_tag(bump: ENV.fetch('BUMP', 'patch'))
  # 尽量同步远程 tags，避免本地缺失导致重复
  system('git fetch --tags --quiet')

  tags_output, _stderr, _status = Open3.capture3("git tag --list 'v*' --sort=-v:refname")
  latest = tags_output.lines.map(&:strip).find { |t| t.match?(/\Av\d+\.\d+\.\d+\z/) }

  major, minor, patch =
    if latest
      latest.delete_prefix('v').split('.').map(&:to_i)
    else
      [0, 1, 0] # first tag: v0.1.0
    end

  case bump
  when 'major'
    major += 1
    minor = 0
    patch = 0
  when 'minor'
    minor += 1
    patch = 0
  else # patch
    patch += 1
  end

  "v#{major}.#{minor}.#{patch}"
end

def create_git_tag(tag)
  existing = `git tag -l "#{tag}" 2>&1`.strip
  return false unless existing.empty?

  system(%(git tag -a "#{tag}" -m "Release #{tag}"))
end

# 生成智能 commit message
def generate_commit_message
  # 获取暂存区的变更
  diff_output = `git diff --cached --name-status 2>&1`
  return nil if diff_output.empty? || !$?.success?

  changed_files = diff_output.split("\n")
  return nil if changed_files.empty?

  # 分析变更类型
  types = []
  scopes = []
  file_descriptions = []

  changed_files.each do |line|
    status, file = line.split("\t", 2)
    next unless file

    type, scope, description = analyze_file_change(status, file)
    types << type if type
    scopes << scope if scope
    file_descriptions << description if description
  end

  # 确定主要的 commit type（优先级：feat > fix > docs > refactor > style > perf > test > chore）
  type_priority = {
    'feat' => 1,
    'fix' => 2,
    'docs' => 3,
    'refactor' => 4,
    'style' => 5,
    'perf' => 6,
    'test' => 7,
    'chore' => 8
  }

  main_type = types.min_by { |t| type_priority[t] || 9 } || 'chore'
  main_scope = scopes.compact.uniq.first || 'general'

  # 生成 subject
  subject = generate_subject(main_type, main_scope, file_descriptions)

  # 生成 body（如果有多个文件变更）
  body = generate_body(changed_files) if changed_files.length > 1

  # 组合 commit message
  message = "#{main_type}(#{main_scope}): #{subject}"
  message += "\n\n#{body}" if body

  message
end

# 分析单个文件的变更
def analyze_file_change(status, file)
  type = nil
  scope = nil
  description = nil

  # 根据文件路径和状态判断类型
  case file
  when %r{^rules/}
    type = 'docs'
    scope = 'rules'
    description = "更新规则文档: #{File.basename(file)}"
  when %r{^backend/}
    type = status == 'A' ? 'feat' : 'refactor'
    scope = 'backend'
    description = "#{status == 'A' ? '新增' : '更新'}后端代码: #{File.basename(file)}"
  when %r{^frontend/}
    type = status == 'A' ? 'feat' : 'refactor'
    scope = 'frontend'
    description = "#{status == 'A' ? '新增' : '更新'}前端代码: #{File.basename(file)}"
  when /\.(rb|rake)$/
    type = 'chore'
    scope = 'scripts'
    description = "更新脚本: #{File.basename(file)}"
  when /\.(sh|bash)$/
    type = 'chore'
    scope = 'scripts'
    description = "更新脚本: #{File.basename(file)}"
  when /\.(md|mdx|txt)$/
    type = 'docs'
    scope = 'docs'
    description = "更新文档: #{File.basename(file)}"
  when /\.(yml|yaml)$/
    type = 'ci'
    scope = 'ci'
    description = "更新 CI 配置: #{File.basename(file)}"
  when /\.(json)$/
    type = 'chore'
    scope = 'config'
    description = "更新配置: #{File.basename(file)}"
  when /\.(go)$/
    type = status == 'A' ? 'feat' : (status == 'D' ? 'refactor' : 'fix')
    scope = 'backend'
    description = "#{status == 'A' ? '新增' : status == 'D' ? '删除' : '更新'} Go 文件: #{File.basename(file)}"
  when /\.(ts|tsx|js|jsx)$/
    type = status == 'A' ? 'feat' : (status == 'D' ? 'refactor' : 'fix')
    scope = 'frontend'
    description = "#{status == 'A' ? '新增' : status == 'D' ? '删除' : '更新'} 前端文件: #{File.basename(file)}"
  else
    type = 'chore'
    scope = 'general'
    description = "#{status == 'A' ? '新增' : status == 'D' ? '删除' : '更新'} 文件: #{File.basename(file)}"
  end

  # 根据状态调整类型
  case status
  when 'D'
    type = 'refactor' if type == 'feat'
  when 'M'
    # 检查是否是修复（通过关键词）
    if file.match?(/fix|bug|error|issue/i)
      type = 'fix'
    end
  end

  [type, scope, description]
end

# 生成 subject
def generate_subject(type, scope, descriptions)
  return '更新项目文件' if descriptions.empty?

  # 如果只有一个文件，使用更具体的描述
  if descriptions.length == 1
    desc = descriptions.first
    # 提取关键信息
    case desc
    when /更新规则文档/
      '更新开发规范'
    when /新增.*后端/
      '新增后端功能'
    when /更新.*后端/
      '更新后端代码'
    when /新增.*前端/
      '新增前端功能'
    when /更新.*前端/
      '更新前端代码'
    when /更新脚本/
      '更新构建脚本'
    when /更新文档/
      '更新项目文档'
    else
      desc.split(':').last&.strip || '更新项目文件'
    end
  else
    # 多个文件，生成通用描述
    case type
    when 'feat'
      '添加新功能'
    when 'fix'
      '修复问题'
    when 'docs'
      '更新文档'
    when 'refactor'
      '重构代码'
    when 'style'
      '代码格式调整'
    when 'perf'
      '性能优化'
    when 'test'
      '更新测试'
    when 'chore'
      '项目维护'
    else
      '更新项目文件'
    end
  end
end

# 生成 body
def generate_body(changed_files)
  lines = ['变更文件:']
  changed_files.each do |line|
    status, file = line.split("\t", 2)
    next unless file

    status_icon = case status
                  when 'A' then '✨'
                  when 'D' then '🗑️'
                  when 'M' then '📝'
                  when 'R' then '🔄'
                  else '📄'
                  end

    lines << "  #{status_icon} #{file}"
  end
  lines.join("\n")
end

task :push do
  # 检查是否有变更
  status_output = `git status --porcelain 2>&1`
  if status_output.empty? || !$?.success?
    puts '没有变更需要提交'
    exit 0
  end

  # 添加所有变更
  system 'git add .'

  # 生成智能 commit message
  commit_message = generate_commit_message || "chore: 更新项目文件\n\n#{Time.now}"

  # 创建临时文件存储 commit message
  require 'tempfile'
  temp_file = Tempfile.new('commit_message')
  temp_file.write(commit_message)
  temp_file.close

  # 使用临时文件提交
  success = system("git commit -F #{temp_file.path}")

  temp_file.unlink

  unless success
    puts '提交失败'
    exit 1
  end

  puts "✅ 提交成功: #{commit_message.lines.first.chomp}"

  # 拉取最新代码
  pull_output = `git pull 2>&1`
  unless $?.success?
    if pull_output.include?('conflict') || pull_output.include?('CONFLICT')
      puts '❌ 检测到合并冲突，请手动解决后重试'
      puts pull_output
      exit 1
    else
      puts '⚠️  拉取失败，但继续推送'
      puts pull_output if pull_output.length > 0
    end
  end

  # 自动递增并创建 tag（默认 patch，可用 BUMP=minor|major）
  tag = next_semver_tag
  unless create_git_tag(tag)
    puts "❌ 创建 tag 失败或已存在: #{tag}"
    exit 1
  end
  puts "✅ 创建 tag: #{tag}"

  # 推送到远程
  push_output = `git push origin main "#{tag}" 2>&1`
  unless $?.success?
    puts '❌ 推送失败'
    puts push_output
    exit 1
  end

  puts '✅ 推送成功'
end

task :run do
  system 'docker compose down -v'
  system 'docker compose up -d --build --remove-orphans'
  system 'docker compose logs -f'
end

# task :push do
#   system 'git add .'
#   system "git commit -m 'Update #{Time.now}'"
#   system 'git pull'
#   system 'git push origin main'
# end