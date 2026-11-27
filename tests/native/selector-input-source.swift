// Run in a dedicated foreground Terminal window; requires enabled Pinyin.
// swiftc tests/native/selector-input-source.swift -o /tmp/janus-input-source-test
// /tmp/janus-input-source-test /path/to/libexec/janus-keychain
import AppKit
import Carbon

func sourceID(_ source: TISInputSource) -> String {
    Unmanaged<CFString>.fromOpaque(TISGetInputSourceProperty(source, kTISPropertyInputSourceID)).takeUnretainedValue() as String
}
func currentID() -> String { sourceID(TISCopyCurrentKeyboardInputSource().takeRetainedValue()) }
func settle() { RunLoop.current.run(until: Date().addingTimeInterval(0.4)) }
func require(_ condition: Bool, _ message: String) {
    if !condition { fputs("FAIL: \(message)\n", stderr); exit(1) }
}
let sources = TISCreateInputSourceList(nil, false).takeRetainedValue() as! [TISInputSource]
guard CommandLine.arguments.count == 2,
      let terminal = NSWorkspace.shared.frontmostApplication,
      terminal.bundleIdentifier == "com.apple.Terminal",
      let finder = NSRunningApplication.runningApplications(withBundleIdentifier: "com.apple.finder").first,
      let chinese = sources.first(where: { sourceID($0) == "com.apple.inputmethod.SCIM.ITABC" }) else {
    fputs("Run in foreground Terminal with Pinyin enabled.\n", stderr); exit(2)
}
let original = TISCopyCurrentKeyboardInputSource().takeRetainedValue()
defer { _ = TISSelectInputSource(original); settle() }
for scenario in ["cancel", "focus-return", "manual-change", "background-exit"] {
    _ = terminal.activate(options: [])
    settle()
    require(TISSelectInputSource(chinese) == noErr, "select initial source")
    settle()
    let process = Process()
    process.executableURL = URL(fileURLWithPath: CommandLine.arguments[1])
    process.arguments = ["selector-input-source"]
    let input = Pipe(), output = Pipe()
    process.standardInput = input
    process.standardOutput = output
    try process.run()
    let ready = output.fileHandleForReading.availableData
    require(String(data: ready, encoding: .utf8) == "ready\n", "guard did not switch")
    settle()
    let selected = currentID()
    require(selected != sourceID(chinese), "English source not selected")
    if scenario == "focus-return" || scenario == "background-exit" {
        _ = finder.activate(options: [])
        settle()
        if scenario == "focus-return" { _ = terminal.activate(options: []); settle() }
    }
    if scenario == "manual-change" {
        _ = TISSelectInputSource(chinese)
        settle()
        if let english = sources.first(where: { sourceID($0) == selected }) {
            _ = TISSelectInputSource(english)
            settle()
        }
    }
    let beforeExit = currentID()
    input.fileHandleForWriting.closeFile()
    process.waitUntilExit()
    settle()
    let expected = scenario == "manual-change" || scenario == "background-exit" ? beforeExit : sourceID(chinese)
    require(currentID() == expected, "\(scenario): input source not preserved/restored")
    print("PASS: \(scenario)")
    fflush(stdout)
}
_ = terminal.activate(options: [])
settle()
