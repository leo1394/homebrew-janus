# Generated from signed, notarized native release assets by the shared publisher.
class Janus < Formula
  desc "SSH hosts and AI Agents with macOS Keychain authentication"
  homepage "https://github.com/leo1394/homebrew-janus"
  url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.6/janus_0.1.6_darwin_universal.tar.gz"
  version "0.1.6"
  sha256 "d9b9385ae9f9ddd384d3a61a3405ba7b1da19b19d62e2604741b653528be3839"
  license "Apache-2.0"

  bottle do
    root_url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.6"
    sha256 cellar: :any_skip_relocation, arm64_golden_gate: "835eb5525212e28c16a3b49683a4334d80578059527064bb53acdba3b2d6bf48"
  end

  depends_on :macos
  on_macos do
    depends_on macos: :sonoma
  end

  def install
    bin.install Dir["bin/*"]
    libexec.install Dir["libexec/*"]
    generate_completions_from_executable(bin/"janus", shell_parameter_format: :cobra, shells: [:bash, :zsh, :fish])
    man1.mkpath
    (man1/"janus.1").write Utils.safe_popen_read(bin/"janus", "__manpage")
  end

  test do
    expected = "Janus Confidential CLI version 0.1.6 (2026-11-24)\nhttps://github.com/leo1394/homebrew-janus\n"
    assert_equal expected, shell_output("#{bin}/janus version")
    assert_equal expected, shell_output("#{bin}/janus --version")
    assert_match "Usage:", shell_output("#{bin}/janus --help")
    assert_path_exists man1/"janus.1"
    assert_path_exists bash_completion/"janus"
    assert_path_exists zsh_completion/"_janus"
    assert_path_exists fish_completion/"janus.fish"
    libexec.children.each do |helper|
      system "/usr/bin/codesign", "--verify", "--strict", helper
    end
  end
end
