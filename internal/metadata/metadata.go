package metadata

const Repository = "https://github.com/leo1394/homebrew-janus"
var Version = "0.1.0-dev"
var Date = "2026-11-19"

func VersionText() string { return "janus version " + Version + " (" + Date + ")\n" + Repository + "\n" }
