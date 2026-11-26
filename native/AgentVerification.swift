import AppKit

// Visible one-time digits stay in memory only. No automatic submission.
final class AgentCodeInput: NSView, NSTextFieldDelegate {
    var fields: [NSTextField] = []
    var onChange: ((Bool) -> Void)?
    var code: String { fields.map { $0.stringValue }.joined() }
    var complete: Bool { fields.allSatisfy { $0.stringValue.utf8.count == 1 && $0.stringValue.utf8.allSatisfy { $0 >= 48 && $0 <= 57 } } }

    init(chinese: Bool) {
        super.init(frame: NSRect(x: 0, y: 0, width: 244, height: 44))
        for index in 0..<6 {
            let field = NSTextField(frame: NSRect(x: CGFloat(index * 42), y: 4, width: 34, height: 36))
            field.font = .monospacedDigitSystemFont(ofSize: 22, weight: .medium)
            field.alignment = .center
            field.bezelStyle = .roundedBezel
            field.focusRingType = .exterior
            field.delegate = self
            field.setAccessibilityLabel(chinese ? "验证码第 \(index + 1) 位，共 6 位" : "Verification digit \(index + 1) of 6")
            fields.append(field)
            addSubview(field)
        }
        for index in 0..<5 { fields[index].nextKeyView = fields[index + 1] }
    }
    required init?(coder: NSCoder) { fatalError("not supported") }

    func clear() {
        for field in fields { field.stringValue = "" }
        onChange?(false)
    }
    func accept(_ text: String, at index: Int) {
        let digits = text.utf8.filter { $0 >= 48 && $0 <= 57 }
        let start = digits.count == 6 ? 0 : index
        fields[index].stringValue = ""
        for (offset, digit) in digits.prefix(6 - start).enumerated() {
            fields[start + offset].stringValue = String(UnicodeScalar(digit))
        }
        let next = min(start + digits.count, 5)
        fields[next].selectText(nil)
        onChange?(complete)
    }
    func controlTextDidChange(_ notification: Notification) {
        guard let field = notification.object as? NSTextField, let index = fields.firstIndex(of: field) else { return }
        accept(field.stringValue, at: index)
    }
    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        guard let field = control as? NSTextField, let index = fields.firstIndex(of: field) else { return false }
        if selector == #selector(NSResponder.deleteBackward(_:)) && field.stringValue.isEmpty && index > 0 {
            fields[index - 1].stringValue = ""
            fields[index - 1].selectText(nil)
            onChange?(false)
            return true
        }
        if selector == #selector(NSResponder.moveLeft(_:)) && index > 0 { fields[index - 1].selectText(nil); return true }
        if selector == #selector(NSResponder.moveRight(_:)) && index < 5 { fields[index + 1].selectText(nil); return true }
        return false
    }
}

func agentDisplayName(_ kind: String, chinese: Bool) -> String {
    switch kind {
    case "codex": return "Codex"
    case "claude", "claude-code": return "Claude Code"
    case "deepseek": return "DeepSeek"
    case "kimi", "kimi-cli": return "Kimi"
    case "pi", "pi-agent": return "Pi Agent"
    case "opencode": return "OpenCode"
    case "": return chinese ? "Agent" : "Agent"
    default: return kind
    }
}

func agentVerificationIcon(_ kind: String) -> NSImage {
    let key: String
    switch kind {
    case "codex": key = "chatgpt"
    case "claude", "claude-code": key = "claude"
    case "kimi", "kimi-cli": key = "kimi"
    case "pi", "pi-agent": key = "pi"
    default: key = kind
    }
    if let encoded = agentIconData[key], let data = Data(base64Encoded: encoded), let image = NSImage(data: data) { return image }
    return NSImage(systemSymbolName: "person.crop.square.badge.sparkles", accessibilityDescription: "Agent") ?? NSImage(systemSymbolName: "brain", accessibilityDescription: "Agent")!
}

// Native controls and semantic colors follow the SSH password approval window.
// A small panel keeps both locales centered without relying on NSAlert's private layout.
final class AgentVerificationDialog: NSObject {
    let window: NSPanel
    let field: AgentCodeInput
    let verify: NSButton
    let content: NSView

    init(target: String, kind: String, chinese: Bool) {
        window = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 360, height: 340), styleMask: [.titled, .fullSizeContentView], backing: .buffered, defer: false)
        field = AgentCodeInput(chinese: chinese)
        verify = NSButton(title: chinese ? "验证" : "Verify", target: nil, action: nil)
        content = NSView(frame: NSRect(x: 0, y: 0, width: 360, height: 340))
        super.init()
        window.title = chinese ? "双因素身份验证" : "Two-factor authentication"
        window.titleVisibility = .hidden
        window.titlebarAppearsTransparent = true
        window.isReleasedWhenClosed = false
        window.contentView = content
        func label(_ text: String, y: CGFloat, height: CGFloat, font: NSFont, color: NSColor = .labelColor) {
            let view = NSTextField(wrappingLabelWithString: text)
            view.frame = NSRect(x: 24, y: y, width: 312, height: height)
            view.alignment = .center
            view.font = font
            view.textColor = color
            view.isSelectable = false
            content.addSubview(view)
        }
        let janus = NSImageView(frame: NSRect(x: 62, y: 260, width: 48, height: 48))
        janus.image = agentVerificationIcon("janus")
        janus.imageScaling = .scaleProportionallyUpOrDown
        janus.setAccessibilityLabel("Janus Confidential")
        content.addSubview(janus)
        let arrow = NSImageView(frame: NSRect(x: 126, y: 274, width: 24, height: 20))
        arrow.image = NSImage(systemSymbolName: "arrow.right", accessibilityDescription: chinese ? "验证" : "Verifies")
        arrow.contentTintColor = .secondaryLabelColor
        content.addSubview(arrow)
        let agent = NSImageView(frame: NSRect(x: 166, y: 260, width: 132, height: 48))
        agent.image = agentVerificationIcon(kind)
        agent.imageScaling = .scaleProportionallyUpOrDown
        agent.setAccessibilityLabel(agentDisplayName(kind, chinese: chinese))
        content.addSubview(agent)
        label(window.title, y: 218, height: 26, font: .systemFont(ofSize: 18, weight: .semibold))
        let name = agentDisplayName(kind, chinese: chinese)
        let agentName = name.hasSuffix("Agent") ? name : name + " Agent"
        label(chinese ? "Janus Confidential 正在验证设备上的" : "Janus Confidential is verifying", y: 188, height: 20, font: .systemFont(ofSize: 13), color: .secondaryLabelColor)
        label(agentName, y: 168, height: 20, font: .systemFont(ofSize: 13, weight: .medium))
        label(target, y: 132, height: 32, font: .monospacedSystemFont(ofSize: 12, weight: .regular), color: .secondaryLabelColor)
        label(chinese ? "请输入一次性 6 位验证码" : "Enter your 6-digit one-time code", y: 109, height: 20, font: .systemFont(ofSize: 13))
        field.frame.origin = NSPoint(x: 58, y: 61)
        content.addSubview(field)
        verify.frame = NSRect(x: 186, y: 18, width: 116, height: 32)
        verify.bezelStyle = .rounded
        verify.bezelColor = .systemBlue
        verify.contentTintColor = .white
        verify.isEnabled = false
        verify.keyEquivalent = "\r"
        verify.target = self
        verify.action = #selector(confirm)
        window.defaultButtonCell = verify.cell as? NSButtonCell
        content.addSubview(verify)
        let cancel = NSButton(title: chinese ? "取消" : "Cancel", target: self, action: #selector(dismiss))
        cancel.frame = NSRect(x: 58, y: 18, width: 116, height: 32)
        cancel.bezelStyle = .rounded
        cancel.keyEquivalent = "\u{1b}"
        content.addSubview(cancel)
        field.onChange = { [weak self] complete in self?.verify.isEnabled = complete }
        field.fields[5].nextKeyView = verify
        verify.nextKeyView = cancel
        cancel.nextKeyView = field.fields[0]
        window.initialFirstResponder = field.fields[0]
    }
    @objc private func confirm() {
        guard field.complete else { return }
        NSApp.stopModal(withCode: .alertFirstButtonReturn)
        window.orderOut(nil)
    }
    @objc private func dismiss() {
        field.clear()
        NSApp.stopModal(withCode: .alertSecondButtonReturn)
        window.orderOut(nil)
    }
    func runModal() -> NSApplication.ModalResponse {
        window.center()
        window.makeKeyAndOrderFront(nil)
        defer { window.orderOut(nil) }
        return NSApp.runModal(for: window)
    }
}

func makeAgentVerificationDialog(target: String, kind: String, chinese: Bool) -> (AgentVerificationDialog, AgentCodeInput) {
    let dialog = AgentVerificationDialog(target: target, kind: kind, chinese: chinese)
    return (dialog, dialog.field)
}

// Standard responder-chain editing works for secure and ordinary field editors.
// The application never reads the clipboard itself.
func installDialogEditingMenu() {
    guard NSApp.mainMenu == nil else { return }
    let menu = NSMenu()
    let item = NSMenuItem()
    let edit = NSMenu(title: "Edit")
    for (title, action, key) in [("Cut", "cut:", "x"), ("Copy", "copy:", "c"), ("Paste", "paste:", "v"), ("Select All", "selectAll:", "a")] {
        let command = NSMenuItem(title: title, action: Selector(action), keyEquivalent: key)
        command.keyEquivalentModifierMask = .command
        edit.addItem(command)
    }
    item.submenu = edit
    menu.addItem(item)
    NSApp.mainMenu = menu
}
