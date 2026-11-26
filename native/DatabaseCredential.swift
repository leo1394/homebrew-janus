import AppKit
import Foundation
import Security
import Darwin

// Separate entry point: the existing OpenSSH-only consumer check is unchanged.
// Only the signed Janus owner process receives data through its private pipe.
func runDatabaseCredential() -> Never {
    do {
        let parent = getppid()
        let team = try migrationTeam(currentHelperIdentifier)
        _ = try checkedMigrationParent(parent, team, expectedIdentifier: "com.janus.ai.confidential.cli")
        var output = stat()
        guard fstat(STDOUT_FILENO, &output) == 0, output.st_mode & S_IFMT == S_IFIFO else { fail("database credential output requires a private pipe") }
        let data = FileHandle.standardInput.readData(ofLength: 8193)
        guard data.count <= 8192,
              let request = try JSONSerialization.jsonObject(with: data) as? [String: String],
              Set(request.keys).isSubset(of: ["accountId", "host", "user", "port", "operation", "namespace", "tlsMode", "provider", "environment"]),
              let account = request["accountId"], account.range(of: "^account:[a-f0-9]{32}$", options: .regularExpression) != nil,
              let host = request["host"], host.range(of: "^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$", options: .regularExpression) != nil,
              let user = request["user"], user.range(of: "^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$", options: .regularExpression) != nil,
              let port = request["port"], let number = Int(port), (1...65535).contains(number),
              let operation = request["operation"], ["read", "enroll", "delete"].contains(operation) else { fail("invalid database credential request") }
        let provider = request["provider"] ?? "mysql"
        let environment = request["environment"] ?? ""
        guard provider == "mysql" || (provider == "clobotics" && host == "iam.clobotics.cn" && number == 443 && ["dev", "test", "prod", "prod-us"].contains(environment)) else { fail("invalid provider binding") }
        let isCLI = provider == "clobotics"
        let kind = isCLI ? "Clobotics CLI" : "MySQL"
        let tlsMode = request["tlsMode"] ?? "verify"
        guard ["verify", "disabled"].contains(tlsMode) else { fail("invalid TLS mode") }
        let transportNotice = tlsMode == "disabled" ? localized("\nTLS disabled: this connection is not encrypted.", "\nTLS 已禁用：此连接未加密。") : ""
        let namespace = request["namespace"] ?? ""
        guard namespace.isEmpty || namespace.range(of: "^[a-f0-9]{64}$", options: .regularExpression) != nil else { fail("invalid database namespace") }
        let service = (isCLI ? "app.janus.confidential.cli.cliprovider.password.v1:" : "app.janus.confidential.cli.mysql.password.v1:") + (namespace.isEmpty ? "" : "development:" + namespace + ":") + account
        let binding = isCLI ? environment + ":" + user : user + "@" + host.lowercased() + ":" + port
        let keychain = try loginKeychain()
        _ = NSApplication.shared
        installDialogEditingMenu()
        NSApp.setActivationPolicy(.accessory)
        let saved = try item(keychain, service, binding)
        if operation == "delete" {
            guard authorize(localized("Delete the \(kind) credential for \(binding)", "删除 \(binding) 的 \(kind) 凭据"), parent: parent) else { exit(2) }
            _ = try checkedMigrationParent(parent, team, expectedIdentifier: "com.janus.ai.confidential.cli")
            if let saved { try checked(SecKeychainItemDelete(saved)) }
            exit(0)
        }
        var secret = Data()
        defer { secret.resetBytes(in: 0..<secret.count) }
        if let saved {
            guard operation == "read" else { fail("database credential already exists") }
            var access: SecAccess?
            try checked(SecKeychainItemCopyAccess(saved, &access))
            guard let access, policyIsProtected(access), try partitionIsCurrent(access, "teamid:" + team) else { fail("database credential access policy mismatch; explicit recovery required") }
            secret = try authorizedSecret(authorize: { authorize(localized("Connect to \(kind) as \(binding)", "以 \(binding) 连接 \(kind)") + transportNotice, parent: parent) }, read: {
                _ = try checkedMigrationParent(parent, team, expectedIdentifier: "com.janus.ai.confidential.cli")
                return try readSavedPassword(saved)
            })
        } else {
            guard operation == "enroll" else { fail("database credential is missing; explicit recovery required") }
            let dialog = NSAlert()
            dialog.icon = isCLI ? NSWorkspace.shared.icon(forFile: "/System/Applications/Utilities/Terminal.app") : NSImage(data: Data(base64Encoded: mysqlDialogIconData)!)
            dialog.messageText = localized("Janus Confidential CLI - Save confidentially", "Janus Confidential CLI - 安全保存")
            let targetText = isCLI ? "\(kind)\n" + localized("Environment: \(environment) · Account: \(user)", "环境：\(environment) · 账号：\(user)") : "\(kind) · \(binding)"
            dialog.informativeText = targetText + "\n\n" + (isCLI ? localized("The password is saved only in macOS Keychain. The account is registered after sign-in verification.", "密码仅保存在 macOS 钥匙串，登录验证成功后登记账户。") : localized("The password is saved only in macOS Keychain. The device is registered after connection verification.", "密码仅保存在 macOS 钥匙串，连接验证成功后登记设备。"))
            dialog.informativeText += transportNotice
            dialog.addButton(withTitle: localized("Save", "保存"));dialog.addButton(withTitle: localized("Cancel", "取消"))
            dialog.buttons[0].keyEquivalent = "\r";dialog.buttons[0].bezelColor = .systemBlue
            let field = NSSecureTextField(frame: NSRect(x: 0, y: 0, width: 280, height: 24))
            field.placeholderString = localized("Enter password", "密码")
            dialog.accessoryView = field;dialog.window.initialFirstResponder = field
            NSApp.activate(ignoringOtherApps: true)
            let deadline = Date(timeIntervalSinceNow: 180)
            let timer = Timer(timeInterval: 0.1, repeats: true) { _ in
                if getppid() != parent || Date() > deadline { field.stringValue = "";NSApp.abortModal();dialog.window.orderOut(nil) }
            }
            RunLoop.main.add(timer, forMode: .modalPanel);defer { timer.invalidate() }
            guard dialog.runModal() == .alertFirstButtonReturn, getppid() == parent else { field.stringValue = "";exit(2) }
            secret = Data(field.stringValue.utf8);field.stringValue = ""
            guard !secret.isEmpty, secret.count <= 16384, !secret.contains(0) else { fail("invalid database password") }
            _ = try checkedMigrationParent(parent, team, expectedIdentifier: "com.janus.ai.confidential.cli")
            let attributes: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service, kSecAttrAccount as String: binding,
                kSecAttrLabel as String: "Janus \(kind) password", kSecValueData as String: secret,
                kSecAttrAccess as String: try makeAccess(), kSecUseKeychain as String: keychain]
            try checked(SecItemAdd(attributes as CFDictionary, nil))
        }
        _ = try checkedMigrationParent(parent, team, expectedIdentifier: "com.janus.ai.confidential.cli")
        FileHandle.standardOutput.write(secret)
        exit(0)
    } catch {
        fail("database credential operation failed; no credential was returned")
    }
}

// Source: https://www.mysql.com/common/logos/logo-mysql-170x115.png
private let mysqlDialogIconData = "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"
