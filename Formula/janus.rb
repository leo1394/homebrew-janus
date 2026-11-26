# Generated from signed, notarized native release assets by the shared publisher.
class Janus < Formula
  desc "SSH hosts and AI Agents with macOS Keychain authentication"
  homepage "https://github.com/leo1394/homebrew-janus"
  url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.9/janus_0.1.9_darwin_universal.tar.gz"
  version "0.1.9"
  sha256 "987486e0f34edd2ddd99c91a55854b22d31640d6bb13377fe246a596d520e286"
  license "Apache-2.0"

  bottle do
    root_url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.9"
    sha256 cellar: :any_skip_relocation, arm64_golden_gate: "8e80b708ec3e17d6f567b661c092c446ad8162e7b8ea6da24a233e02ab09c3c4"
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
    expected = "Janus Confidential CLI version 0.1.9 (2026-11-26)\nhttps://github.com/leo1394/homebrew-janus\n"
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
