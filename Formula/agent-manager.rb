class AgentManager < Formula
  desc "Persistent sandbox workspaces for terminal coding agents"
  homepage "https://github.com/4fuu/agent-manager"
  url "https://github.com/4fuu/agent-manager/releases/download/v2026.906.0/agent-manager-2026.906.0-darwin-arm64.tar.gz"
  sha256 "38c07797363ebbe77b85101c3e63efb2e188703ab2f741d248a3e86f9d0dcae6"
  version "2026.906.0"

  depends_on :macos
  depends_on arch: :arm64

  def install
    bin.install "agent-manager"
    doc.install "README.md", "docs", "images"
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
