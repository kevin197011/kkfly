#!/usr/bin/env ruby
# frozen_string_literal: true

# Copyright (c) 2026 kk — MIT License
#
# One-shot publish:
#   ruby push.rb              # go test → commit/push → tag vX.Y.Z → push tag
#   SKIP_RELEASE=1 ruby push.rb
#   VERSION=0.1.17 ruby push.rb

exec('rake', 'push', *ARGV)
