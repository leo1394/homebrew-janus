# Generated from signed, notarized native release assets by the shared publisher.
class Janus < Formula
  desc "Janus Confidential CLI for SSH devices and macOS Keychain"
  homepage "https://github.com/leo1394/homebrew-janus"
  url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.4/janus_0.1.4_darwin_universal.tar.gz"
  version "0.1.4"
  sha256 "1c816ddb8b3ee6973375dd497f67d2467c90f2e0bb71ccb12d9202e35f7a1722"
  license "Apache-2.0"

  bottle do
    root_url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.4"
    sha256 cellar: :any_skip_relocation, arm64_golden_gate: "d632bd308c1b5b0e94b91251e75b871cfc29176dfdd0f55db04c74b0774f450a"
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
    expected = "janus version 0.1.4 (2026-11-20)\nhttps://github.com/leo1394/homebrew-janus\n"
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
