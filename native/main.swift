import AppKit
import Security
import LocalAuthentication
import Darwin

@_silgen_name("proc_pidpath")
func janus_proc_pidpath(_ pid: Int32, _ buffer: UnsafeMutableRawPointer, _ size: UInt32) -> Int32

func fail(_ message: String) -> Never { fputs("janus-keychain: " + message + "\n", stderr); exit(1) }
let started = Double(ProcessInfo.processInfo.environment["JANUS_TRACE_EPOCH"] ?? "") ?? Date().timeIntervalSince1970
func trace(_ phase: String) {
    if ProcessInfo.processInfo.environment["JANUS_TRACE"] == "1" {
        fputs(String(format: "janus-keychain phase=%@ elapsed_ms=%.0f\n", phase, (Date().timeIntervalSince1970-started)*1000), stderr)
    }
}
trace("helper_entered")
trace("helper_revision_requirement_v4")
let useChinese = Locale.preferredLanguages.first?.lowercased().hasPrefix("zh") == true
func localized(_ english: String, _ chinese: String) -> String { useChinese ? chinese : english }
func loginKeychain() throws -> SecKeychain {
    trace("keychain_open_start")
    defer { trace("keychain_open_complete") }
    var keychain: SecKeychain?
    try checked(SecKeychainOpen(NSHomeDirectory()+"/Library/Keychains/login.keychain-db", &keychain))
    guard let keychain else { throw NSError(domain: "Janus", code: 1) }
    return keychain
}
func item(_ keychain: SecKeychain, _ service: String, _ user: String) throws -> SecKeychainItem? {
    trace("keychain_find_start")
    defer { trace("keychain_find_complete") }
    return try withoutKeychainUI {
        var result: CFTypeRef?
        let query = credentialReferenceQuery(keychain, service, user)
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        try checked(status)
        guard let result, CFGetTypeID(result) == SecKeychainItemGetTypeID() else { throw NSError(domain: "Janus", code: 2) }
        return (result as! SecKeychainItem)
    }
}
func authorize(_ reason: String, parent: pid_t) -> Bool {
    trace("authentication_context_start")
    let context = LAContext()
    context.touchIDAuthenticationAllowableReuseDuration = 0
    defer { context.invalidate() }
    var error: NSError?
    trace("authentication_policy_start")
    guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &error) else { return false }
    trace("authentication_policy_complete")
    let lock = NSLock()
    var result: Bool?
    trace("authentication_submitted") // System API has no window-shown callback.
    context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) { allowed, _ in
        lock.lock(); result = allowed; lock.unlock()
    }
    let deadline = Date(timeIntervalSinceNow: 180)
    while Date() < deadline && getppid() == parent {
        lock.lock(); let answer = result; lock.unlock()
        if let answer { trace("authentication_completed"); return answer }
        RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.02))
    }
    return false
}
let args = CommandLine.arguments
if args.count == 2 && args[1] == "--build-info" {
    print("janus-keychain requirement-v4")
    exit(0)
}
if args.count == 2 && args[1] == "--self-test" {
    do { guard policyIsProtected(try makeAccess()) else { fail("unprotected ACL") }; print("ACL policy OK"); exit(0) }
    catch { fail("ACL policy inspection failed") }
}
let deleting = args.count == 5 && args[1] == "--delete"
let diagnosing = args.count == 5 && args[1] == "--diagnose-access"
let env = ProcessInfo.processInfo.environment
let host = (deleting || diagnosing) ? args[2] : env["JANUS_SSH_HOST"] ?? ""
let user = (deleting || diagnosing) ? args[3] : env["JANUS_SSH_USER"] ?? ""
let portText = (deleting || diagnosing) ? args[4] : env["JANUS_SSH_PORT"] ?? ""
guard host.range(of: "^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$", options: .regularExpression) != nil,
      user.range(of: "^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$", options: .regularExpression) != nil,
      let port = Int(portText), (1...65535).contains(port) else { fail("invalid SSH context") }
let legacyService = "app.janus.ssh.password.v1:" + host.lowercased() + ":" + String(port)
let service = "app.janus.cli.ssh.password.v1:" + host.lowercased() + ":" + String(port)
func selectedItem(_ keychain: SecKeychain) throws -> SecKeychainItem? {
    if let current = try item(keychain, service, user) { return current }
    return try item(keychain, legacyService, user)
}
if diagnosing {
    do {
        let keychain = try loginKeychain()
        var status = SecKeychainStatus()
        try checked(SecKeychainGetStatus(keychain, &status))
        var report: [String: Any] = ["unlocked": status & kSecUnlockStateStatus != 0, "credentialRead": false]
        if let value = try selectedItem(keychain) {
            report["exists"] = true
            var access: SecAccess?
            try checked(SecKeychainItemCopyAccess(value, &access))
            guard let access else { fail("access metadata unavailable") }
            report["applicationCurrent"] = policyIsProtected(access)
            report["partitionCurrent"] = try partitionIsCurrent(access, helperPartition())
            let rule = try passwordPartitionACL(access)
            var apps: CFArray?, description: CFString?
            var selector = SecKeychainPromptSelector(rawValue: 0)
            try checked(SecACLCopyContents(rule, &apps, &description, &selector))
            if let description {
                report["partitionKinds"] = try passwordPartitionValues(description as String).map { String($0.prefix(while: { $0 != ":" })) }
            }
        } else { report["exists"] = false }
        let data = try JSONSerialization.data(withJSONObject: report, options: [.sortedKeys])
        FileHandle.standardOutput.write(data); print(""); exit(0)
    } catch { let e = error as NSError; fail("metadata diagnostic domain=\(e.domain) code=\(e.code)") }
}
if deleting {
    do { let keychain = try loginKeychain(); for key in [service, legacyService] { if let value = try item(keychain, key, user) { try checked(SecKeychainItemDelete(value)) } }; exit(0) }
    catch { fail("could not delete Keychain item") }
}
guard args.count == 2 else { fail("missing SSH prompt") }
let parent = getppid()
var parentPath = [CChar](repeating: 0, count: 4096)
let count = parentPath.withUnsafeMutableBytes { janus_proc_pidpath(parent, $0.baseAddress!, UInt32($0.count)) }
guard count > 0, String(cString: parentPath) == "/usr/bin/ssh" else { fail("credential consumer must be system SSH") }
var output = stat()
guard fstat(STDOUT_FILENO, &output) == 0, output.st_mode & S_IFMT == S_IFIFO else { fail("credential output must be an SSH pipe") }
trace("application_init_start")
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
trace("application_init_complete")
let prompt = args[1]
func alert(_ title: String, _ detail: String) -> NSAlert {
    let dialog = NSAlert()
    dialog.messageText = title
    dialog.informativeText = detail
    dialog.window.collectionBehavior = [.moveToActiveSpace, .fullScreenAuxiliary]
    NSApplication.shared.activate(ignoringOtherApps: true)
    dialog.addButton(withTitle: localized("Connect", "连接"))
    dialog.addButton(withTitle: localized("Cancel", "取消"))
    return dialog
}
// OpenSSH asks for host-key consent before any password request.
if prompt.contains("Are you sure you want to continue connecting") {
    let dialog = alert(localized("Trust SSH host key?", "信任此 SSH 主机密钥？"), prompt)
    trace("hostkey_dialog_presenting")
    guard dialog.runModal() == .alertFirstButtonReturn, getppid() == parent else { exit(1) }
    print("yes"); exit(0)
}
guard prompt.lowercased().contains("password:") else { fail("unsupported SSH challenge; only password authentication is supported") }
var credentialStage = "keychain_lookup"
do {
    trace("keychain_lookup")
    let keychain = try loginKeychain()
    let ownItem = try item(keychain, service, user)
    let savedItem = try ownItem ?? item(keychain, legacyService, user)
    if let value = savedItem {
      do {
        trace("saved_credential_found")
        credentialStage = "acl_check"
        trace("acl_copy_start")
        var access: SecAccess?
        try checked(SecKeychainItemCopyAccess(value, &access))
        guard let access else { fail("saved credential access policy unavailable") }
        trace("acl_copy_complete")
        trace("acl_policy_start")
        let applicationCurrent = policyIsProtected(access)
        trace(applicationCurrent ? "application_current" : "application_migration_required")
        trace("partition_check_start")
        credentialStage = "partition_check"
        let expectedPartition = try helperPartition()
        let partitionCurrent = try partitionIsCurrent(access, expectedPartition)
        trace(partitionCurrent ? "partition_current" : "partition_migration_required")
        if ownItem == nil && !partitionCurrent {
            credentialStage = "legacy_credential_incompatible"
            throw NSError(domain: NSOSStatusErrorDomain, code: Int(errSecNoAccessForItem))
        }
        let needsMigration = !applicationCurrent || !partitionCurrent
        trace("acl_policy_complete")
        let reason = needsMigration ? localized("Authorize Janus to update this SSH credential's helper access and connect to \(user)@\(host):\(port)", "授权 Janus 更新此 SSH 凭据的助手权限并连接 \(user)@\(host):\(port)") : localized("Use saved SSH password for \(user)@\(host):\(port)", "使用已保存的 SSH 密码连接 \(user)@\(host):\(port)")
        credentialStage = "owner_authorization"
        var secret = try authorizedSecret(authorize: { authorize(reason, parent: parent) }, read: {
            guard getppid() == parent else { throw NSError(domain: "Janus", code: 3) }
            if needsMigration {
                credentialStage = "acl_migration"
                trace("acl_migration_start")
                try updateHelperAccess(access)
                credentialStage = "partition_migration"
                try updatePartitionAccess(access, expectedPartition)
                trace("acl_commit_start")
                credentialStage = "acl_commit"
                try commitVerifiedAccess(commit: {
                    SecKeychainItemSetAccess(value, access)
                }, verify: {
                    var observed: SecAccess?
                    guard SecKeychainItemCopyAccess(value, &observed) == errSecSuccess,
                          let observed else { trace("acl_readback_unavailable"); return false }
                    let current = policyIsProtected(observed) && ((try? partitionIsCurrent(observed, expectedPartition)) == true)
                    trace(current ? "acl_readback_current" : "acl_readback_incomplete")
                    return current
                }, reauthorize: {
                    credentialStage = "acl_recovery_authorization"
                    trace("acl_recovery_authorization_required")
                    return authorize(localized("macOS reported an error after updating SSH access. Verify again to retry reading the saved password.", "macOS 更新 SSH 访问权限后报告错误。请再次验证身份，以重试读取已保存的密码。"), parent: parent)
                })
                guard getppid() == parent else { throw NSError(domain: "Janus", code: 3) }
                trace("acl_commit_complete")
                trace("credential_helper_migrated")
            }
            credentialStage = "credential_read"
            trace("credential_read_start")
            let data = try readSavedPassword(value)
            trace("credential_read_complete")
            return data
        })
        defer { secret.resetBytes(in: 0..<secret.count) }
        guard getppid() == parent, !secret.contains(10), !secret.contains(13), !secret.contains(0) else { fail("invalid or expired credential") }
        trace("credential_delivery_start")
        FileHandle.standardOutput.write(secret)
        FileHandle.standardOutput.write(Data([10])); trace("credential_delivery_complete"); exit(0)
      } catch {
        let failure = error as NSError
        guard canEnrollSeparateCredential(hasOwnItem: ownItem != nil, stage: credentialStage, domain: failure.domain, code: failure.code),
              getppid() == parent else { throw error }
        trace("legacy_credential_recovery_required")
        let recovery = alert(localized("Janus - Saved credential unavailable", "Janus - 旧凭据无法使用"),
            localized("macOS denied access to the old credential. Re-enter the SSH password to save a separate Janus credential. The old password will not be overwritten or deleted.", "macOS 拒绝访问旧凭据。可重新输入 SSH 密码，保存为独立的 Janus 凭据。旧密码将保留，不会删除或覆盖。"))
        recovery.icon = NSWorkspace.shared.icon(forFile: "/System/Applications/Utilities/Terminal.app")
        recovery.buttons[0].title = localized("Enter SSH password", "重新输入 SSH 密码")
        guard recovery.runModal() == .alertFirstButtonReturn, getppid() == parent else { throw error }
      }
    }
    trace("saved_credential_absent")
    let dialog = alert(localized("Janus - Save confidentially", "Janus - 安全保存"), "\(user)@\(host):\(port)\n" + localized("Password saved in macOS Keychain.", "密码仅保存到 macOS 钥匙串。"))
    dialog.window.title = dialog.messageText
    dialog.icon = NSWorkspace.shared.icon(forFile: "/System/Applications/Utilities/Terminal.app")
    let saveButton = dialog.buttons[0]
    saveButton.title = localized("Save", "保存")
    saveButton.keyEquivalent = "\r"
    saveButton.bezelColor = .systemBlue
    saveButton.contentTintColor = .white
    dialog.window.defaultButtonCell = saveButton.cell as? NSButtonCell
    let field = NSSecureTextField(frame: NSRect(x: 0, y: 0, width: 280, height: 24))
    field.placeholderString = localized("Enter password", "密码")
    dialog.accessoryView = field
    dialog.window.initialFirstResponder = field
    trace("password_dialog_presenting")
    // Our own window is observable; the authentication system window is not.
    let observer = NotificationCenter.default.addObserver(forName: NSWindow.didBecomeKeyNotification, object: dialog.window, queue: .main) { _ in if dialog.window.isVisible { trace("password_dialog_visible") } }
    defer { NotificationCenter.default.removeObserver(observer) }
    let deadline = Date(timeIntervalSinceNow: 180)
    let timer = Timer(timeInterval: 0.1, repeats: true) { _ in
        if getppid() != parent || Date() > deadline { field.stringValue = ""; NSApp.abortModal(); dialog.window.orderOut(nil) }
    }
    RunLoop.main.add(timer, forMode: .modalPanel)
    defer { timer.invalidate() }
    guard dialog.runModal() == .alertFirstButtonReturn, getppid() == parent else { field.stringValue = ""; exit(1) }
    var secret = Data(field.stringValue.utf8)
    field.stringValue = ""
    defer { secret.resetBytes(in: 0..<secret.count) }
    guard !secret.isEmpty, secret.count <= 16384, !secret.contains(0), !secret.contains(10), !secret.contains(13) else { fail("invalid password") }
    credentialStage = "credential_save"
    let attributes: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: service, kSecAttrAccount as String: user,
        kSecAttrLabel as String: label, kSecValueData as String: secret,
        kSecAttrAccess as String: try makeAccess(), kSecUseKeychain as String: keychain]
    trace("credential_save_start")
    try checked(SecItemAdd(attributes as CFDictionary, nil))
    trace("credential_save_complete")
    trace("credential_delivery_start")
        FileHandle.standardOutput.write(secret)
    FileHandle.standardOutput.write(Data([10]))
} catch {
    let failure = error as NSError
    let detail: String
    if failure.domain == NSOSStatusErrorDomain {
        detail = (SecCopyErrorMessageString(OSStatus(failure.code), nil) as String?) ?? "Security operation failed"
    } else { detail = "Operation denied or invalid result" }
    fail("stage=\(credentialStage) domain=\(failure.domain) code=\(failure.code): \(detail). No password was delivered; any completed ACL migration remains applied.")
}
