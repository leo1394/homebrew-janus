import AppKit
let app = NSApplication.shared
let input = AgentCodeInput(chinese: false)
assert(!input.complete)
input.accept("1", at: 0)
assert(input.code == "1" && !input.complete)
input.accept("23456", at: 1)
assert(input.code == "123456" && input.complete)
input.accept("654321", at: 4)
assert(input.code == "654321" && input.complete)
input.clear()
input.accept("ab１２34!", at: 0)
assert(input.code == "34" && !input.complete)
input.clear()
input.accept("1234567", at: 0)
assert(input.code == "123456" && input.complete)
input.fields[5].stringValue = ""
let handled = input.control(input.fields[5], textView: NSTextView(), doCommandBy: #selector(NSResponder.deleteBackward(_:)))
assert(handled && input.fields[4].stringValue.isEmpty && !input.complete)
for kind in ["codex", "claude-code", "deepseek", "kimi", "pi-agent", "opencode", "unknown"] {
    assert(agentVerificationIcon(kind).isValid)
}
input.clear()
assert(input.code.isEmpty && !input.complete)
print("MFA six-digit input, paste, deletion, ASCII restriction, clear and icons: PASS")
for chinese in [true, false] {
    let (dialog, field) = makeAgentVerificationDialog(target: "owner@example.com:7263", kind: "opencode", chinese: chinese)
    assert(dialog.window.title == (chinese ? "双因素身份验证" : "Two-factor authentication"))
    assert(!dialog.verify.isEnabled)
    field.accept("123456", at: 0)
    assert(dialog.verify.isEnabled)
    field.clear()
    assert(!dialog.verify.isEnabled)
    assert(dialog.window.initialFirstResponder === field.fields[0])
}
print("Bilingual titles, initial focus and verification gating: PASS")

for chinese in [true, false] {
    for instance in ["", "default", "opencode-main", String(repeating: "a", count: 32)] {
        let (dialog, _) = makeAgentVerificationDialog(target: "owner@example.com:22", kind: "opencode", chinese: chinese, instance: instance)
        let labels = dialog.content.subviews.compactMap { $0 as? NSTextField }.map { $0.stringValue }
        let expected = instance.isEmpty || instance == "default" ? "OpenCode Agent" : "OpenCode Agent (" + instance + ")"
        assert(labels.contains(expected))
    }
}
print("Bilingual named instance labels and default omission: PASS")
