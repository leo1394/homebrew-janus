# Generated from signed, notarized native release assets by the shared publisher.
class Janus < Formula
  desc "SSH hosts and AI Agents with macOS Keychain authentication"
  homepage "https://github.com/leo1394/homebrew-janus"
  url "https://github.com/leo1394/homebrew-janus/releases/download/v0.2.2/janus_0.2.2_darwin_universal.tar.gz"
  version "0.2.2"
  sha256 "a6831d8438eb153fea31a75447b054eb7017ac4c20d8e961c384c617e83087f6"
  license "Apache-2.0"

  bottle do
    root_url "https://github.com/leo1394/homebrew-janus/releases/download/v0.2.2"
    sha256 cellar: :any_skip_relocation, arm64_golden_gate: "f66e0d3d2643fe28d6f1ad7bfc58a64e05675658194303c03d7267c7cdc2115f"
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
    expected = "Janus Confidential CLI version 0.2.2 (2026-11-27)\nhttps://github.com/leo1394/homebrew-janus\n"
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
