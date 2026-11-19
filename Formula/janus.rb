# Generated from signed, notarized native release assets by the shared publisher.
class Janus < Formula
  desc "SSH devices, Keychain credentials and reusable terminal sessions"
  homepage "https://github.com/leo1394/homebrew-janus"
  url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.1/janus_0.1.1_darwin_arm64.tar.gz"
  version "0.1.1"
  sha256 "4b877eddc44975692845b90781e0788481260acf6193314008e20c79feeaef1d"
  license "MIT"

  bottle do
    root_url "https://github.com/leo1394/homebrew-janus/releases/download/v0.1.1"
    sha256 cellar: :any_skip_relocation, arm64_golden_gate: "d292aef0393a02725980ddc52b9cd6a15ac6e6b67f21906547a552c21ff659e3"
  end

  depends_on :macos
  on_macos do
    depends_on macos: :sonoma
  end
  depends_on arch: :arm64

  def install
    bin.install Dir["bin/*"]
    libexec.install Dir["libexec/*"]
    generate_completions_from_executable(bin/"janus", shell_parameter_format: :cobra, shells: [:bash, :zsh, :fish])
    man1.mkpath
    (man1/"janus.1").write Utils.safe_popen_read(bin/"janus", "__manpage")
  end

  test do
    expected = "janus version 0.1.1 (2026-11-19)\nhttps://github.com/leo1394/homebrew-janus\n"
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
