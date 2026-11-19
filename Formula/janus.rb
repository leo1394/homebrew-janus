# Generated from signed, notarized native release assets by the shared publisher.
class Janus < Formula
  desc "SSH devices, Keychain credentials and reusable terminal sessions"
  homepage "https://github.com/leo1394/homebrew-janus"
  url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.3/janus_0.1.3_darwin_universal.tar.gz"
  version "0.1.3"
  sha256 "155dd6bc048893330887f5e8a58cea7cb5f665a86b0b0289a8706452076ae0af"
  license "Apache-2.0"

  bottle do
    root_url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.3"
    sha256 cellar: :any_skip_relocation, arm64_golden_gate: "4775bbe41be6f88e6d2937f58acf66bd00791abf6eaefdfb0407a5b1c51c8619"
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
    expected = "janus version 0.1.3 (2026-11-19)\nhttps://github.com/leo1394/homebrew-janus\n"
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
