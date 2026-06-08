#!/usr/bin/env bash
set -euo pipefail

version="${1:?version required}"
checksums="${2:-dist/checksums.txt}"

sha_for() {
  local name="$1"
  awk -v name="$name" '$2 == name { print $1 }' "$checksums"
}

darwin_amd64="$(sha_for "mj_${version}_darwin_amd64.tar.gz")"
darwin_arm64="$(sha_for "mj_${version}_darwin_arm64.tar.gz")"
linux_amd64="$(sha_for "mj_${version}_linux_amd64.tar.gz")"
linux_arm64="$(sha_for "mj_${version}_linux_arm64.tar.gz")"

test -n "$darwin_amd64"
test -n "$darwin_arm64"
test -n "$linux_amd64"
test -n "$linux_arm64"

mkdir -p Formula
cat > Formula/mj.rb <<EOF
class Mj < Formula
  desc "Unofficial Midjourney web CLI and MCP server"
  homepage "https://github.com/ehmo/mj"
  version "${version}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ehmo/mj/releases/download/v${version}/mj_${version}_darwin_arm64.tar.gz"
      sha256 "${darwin_arm64}"
    else
      url "https://github.com/ehmo/mj/releases/download/v${version}/mj_${version}_darwin_amd64.tar.gz"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ehmo/mj/releases/download/v${version}/mj_${version}_linux_arm64.tar.gz"
      sha256 "${linux_arm64}"
    else
      url "https://github.com/ehmo/mj/releases/download/v${version}/mj_${version}_linux_amd64.tar.gz"
      sha256 "${linux_amd64}"
    end
  end

  def install
    bin.install "mj"
    bin.install "mj-mcp"
  end

  def caveats
    <<~EOS
      mj automates Midjourney, which their Terms of Service prohibit. Use your OWN
      account, at low volume. First run downloads a managed Camoufox browser.

      Get started:
        mj doctor
        mj login --i-understand
    EOS
  end

  test do
    assert_match "mj #{version}", shell_output("#{bin}/mj version")
  end
end
EOF
