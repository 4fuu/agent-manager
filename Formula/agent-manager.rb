class AgentManager < Formula
  desc "Persistent sandbox workspaces for terminal coding agents"
  homepage "https://github.com/4fuu/agent-manager"
  license "AGPL-3.0-only"
  url "https://github.com/4fuu/agent-manager/releases/download/v2026.906.1/agent-manager-2026.906.1-darwin-arm64.tar.gz"
  sha256 "d79299857ca50010e05bcecb8379d7e332487547bf697f602db6cbd26da74236"
  version "2026.906.1"

  depends_on :macos
  depends_on arch: :arm64

  def install
    bin.install "agent-manager"
    doc.install "LICENSE", "README.md", "docs", "images"
  end

  def caveats
    <<~EOS
      Run agent-manager runtime-install, then agent-manager doctor.
      Stop the supervisor before upgrading. Guest boot requires Apple Silicon.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/agent-manager --version")
  end
end
