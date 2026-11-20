import AppKit
import Security
import Foundation

let currentHelperIdentifier = "com.janus.ai.confidential.cli.keychain"
let previousHelperIdentifier = "com.janus.ai.cli.keychain"
let currentCredentialPrefix = "app.janus.confidential.cli.ssh.password.v1:"
let previousCredentialPrefix = "app.janus.cli.ssh.password.v1:"

func migrationRequirement(_ identifier: String, _ team: String) throws -> SecRequirement {
    guard [currentHelperIdentifier, previousHelperIdentifier].contains(identifier),
          team.range(of: "^[A-Z0-9]{10}$", options: .regularExpression) != nil else { throw NSError(domain: "JanusMigration", code: 1) }
    let text = "anchor apple generic and identifier \"\(identifier)\" and certificate 1[field.1.2.840.113635.100.6.2.6] exists and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = \"\(team)\""
    var requirement: SecRequirement?
    try checked(SecRequirementCreateWithString(text as CFString, [], &requirement))
    guard let requirement else { throw NSError(domain: "JanusMigration", code: 1) }
    return requirement
}
func migrationTeam(_ expectedIdentifier: String) throws -> String {
    let partition = try helperPartition(expectedIdentifier: expectedIdentifier)
    guard partition.hasPrefix("teamid:") else { throw NSError(domain: "JanusMigration", code: 1) }
    return String(partition.dropFirst(7))
}
func checkedMigrationFile(_ url: URL, _ identifier: String, _ team: String) throws -> SecStaticCode {
    var code: SecStaticCode?
    try checked(SecStaticCodeCreateWithPath(url as CFURL, [], &code))
    guard let code else { throw NSError(domain: "JanusMigration", code: 1) }
    try checked(SecStaticCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSStrictValidate), migrationRequirement(identifier, team)))
    return code
}
func checkedMigrationParent(_ pid: pid_t, _ team: String) throws -> URL {
    guard getppid() == pid else { throw NSError(domain: "JanusMigration", code: 2) }
    var code: SecCode?
    try checked(SecCodeCopyGuestWithAttributes(nil, [kSecGuestAttributePid as String: NSNumber(value: pid)] as CFDictionary, [], &code))
    guard let code else { throw NSError(domain: "JanusMigration", code: 2) }
    try checked(SecCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSStrictValidate), migrationRequirement(currentHelperIdentifier, team)))
    var staticCode: SecStaticCode?
    try checked(SecCodeCopyStaticCode(code, [], &staticCode))
    guard let staticCode else { throw NSError(domain: "JanusMigration", code: 2) }
    var path: CFURL?
    try checked(SecCodeCopyPath(staticCode, [], &path))
    guard let path else { throw NSError(domain: "JanusMigration", code: 2) }
    let url = path as URL
    _ = try checkedMigrationFile(url, currentHelperIdentifier, team)
    return url
}
func migratePreviousIdentity(host: String, user: String, port: Int) throws {
    let team = try migrationTeam(currentHelperIdentifier)
    let executable = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
    let bridge = executable.deletingLastPathComponent().appendingPathComponent("janus-keychain-migrate")
    _ = try checkedMigrationFile(bridge, previousHelperIdentifier, team)
    let process = Process()
    process.executableURL = bridge
    process.arguments = ["--migrate-identity", host, user, String(port)]
    process.standardInput = FileHandle.nullDevice
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.standardError
    trace("identity_migration_start")
    try process.run()
    process.waitUntilExit()
    guard process.terminationReason == .exit, process.terminationStatus == 0 else {
        throw NSError(domain: "JanusMigration", code: Int(process.terminationStatus == 0 ? 1 : process.terminationStatus))
    }
    trace("identity_migration_complete")
}
// Compiled into an old-identity bridge. It never exports a password, modifies an
// old ACL, overwrites a target item, or accepts an unsigned/unrelated caller.
func runIdentityMigration() -> Never {
    do {
        let args = CommandLine.arguments
        guard args.count == 5, args[1] == "--migrate-identity",
              args[2].range(of: "^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$", options: .regularExpression) != nil,
              args[3].range(of: "^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$", options: .regularExpression) != nil,
              let port = Int(args[4]), (1...65535).contains(port) else { throw NSError(domain: "JanusMigration", code: 1) }
        let team = try migrationTeam(previousHelperIdentifier)
        let parent = getppid()
        let target = try checkedMigrationParent(parent, team)
        let suffix = args[2].lowercased() + ":" + String(port)
        let keychain = try loginKeychain()
        guard try item(keychain, currentCredentialPrefix + suffix, args[3]) == nil else { throw NSError(domain: "JanusMigration", code: 3) }
        guard let source = try item(keychain, previousCredentialPrefix + suffix, args[3]) else { throw NSError(domain: "JanusMigration", code: 4) }
        var access: SecAccess?
        try checked(SecKeychainItemCopyAccess(source, &access))
        guard let access, policyIsProtected(access), try partitionIsCurrent(access, "teamid:" + team) else { throw NSError(domain: "JanusMigration", code: 5) }
        _ = NSApplication.shared
        trace("identity_migration_authorization_start")
        let reason = localized("Migrate the saved SSH credential for \(args[3])@\(args[2]) to the new Janus Confidential CLI identity. Keep the old credential for rollback.", "将 \(args[3])@\(args[2]) 的 SSH 凭据迁移到 Janus Confidential CLI 新签名身份，保留旧凭据供回滚。")
        guard authorize(reason, parent: parent) else { trace("identity_migration_cancelled"); exit(2) }
        _ = try checkedMigrationParent(parent, team)
        trace("identity_migration_read_start")
        var secret = try readSavedPassword(source)
        defer { secret.resetBytes(in: 0..<secret.count) }
        trace("identity_migration_read_complete")
        var trusted: SecTrustedApplication?
        try checked(SecTrustedApplicationCreateFromPath(target.path, &trusted))
        guard let trusted else { throw NSError(domain: "JanusMigration", code: 1) }
        var newAccess: SecAccess?
        try checked(SecAccessCreate(label as CFString, [trusted] as CFArray, &newAccess))
        guard let newAccess else { throw NSError(domain: "JanusMigration", code: 1) }
        _ = try checkedMigrationParent(parent, team)
        let attributes: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: currentCredentialPrefix + suffix, kSecAttrAccount as String: args[3],
            kSecAttrLabel as String: label, kSecValueData as String: secret,
            kSecAttrAccess as String: newAccess, kSecUseKeychain as String: keychain]
        trace("identity_migration_save_start")
        try withoutKeychainUI { try checked(SecItemAdd(attributes as CFDictionary, nil)) }
        trace("identity_migration_save_complete")
        secret.resetBytes(in: 0..<secret.count)
        exit(0)
    } catch {
        let failure = error as NSError
        fputs("janus-keychain-migrate: migration stopped domain=\(failure.domain) code=\(failure.code); old credential preserved, no password exported.\n", stderr)
        exit(1)
    }
}
