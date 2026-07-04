# frozen_string_literal: true

require 'minitest/autorun'
require_relative '../../lib/kkfly/release'

class ReleaseTest < Minitest::Test
  def test_next_tag_from_explicit_version
    with_env('VERSION' => '0.2.0') do
      assert_equal 'v0.2.0', Kkfly::Release.next_tag
    end
  end

  def test_bump_patch
    assert_equal 'v0.1.17', Kkfly::Release.bump_patch('v0.1.16')
  end

  private

  def with_env(updates)
    old = updates.keys.to_h { |k| [k, ENV[k]] }
    updates.each { |k, v| v.nil? ? ENV.delete(k) : ENV[k] = v }
    yield
  ensure
    old.each { |k, v| v.nil? ? ENV.delete(k) : ENV[k] = v }
  end
end
