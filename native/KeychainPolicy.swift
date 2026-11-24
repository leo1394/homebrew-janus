// Extracted from Janus Confidential scripts/janus-confidential.swift.
import Foundation
import Security

func checked(_ status: OSStatus) throws {
    if status != errSecSuccess { throw NSError(domain: NSOSStatusErrorDomain, code: Int(status)) }
}
let label = "Janus SSH password"
// Trust only this helper; LocalAuthentication gates every credential read.
// Signed releases retain their designated identity across installation paths.
// Ad-hoc rebuilds may require renewal after user authentication.
func makeAccess() throws -> SecAccess {
    var application: SecTrustedApplication?
    try checked(SecTrustedApplicationCreateFromPath(nil, &application))
    guard let application else { throw NSError(domain: "Janus", code: 1) }
    var access: SecAccess?
    try checked(SecAccessCreate(label as CFString, [application] as CFArray, &access))
    guard let access else { throw NSError(domain: "Janus", code: 1) }
    return access
}
// Existing entries must retain their ACL handles so Security updates old rules
// instead of appending new rules alongside grants for obsolete helper identities.
func updateHelperAccess(_ access: SecAccess) throws {
    var application: SecTrustedApplication?
    try checked(SecTrustedApplicationCreateFromPath(nil, &application))
    guard let application else { throw NSError(domain: "Janus", code: 1) }
    var list: CFArray?
    try checked(SecAccessCopyACLList(access, &list))
    var found = false
    for acl in (list as? [SecACL] ?? []) {
        let tags = SecACLCopyAuthorizations(acl) as NSArray
        if tags.contains(kSecACLAuthorizationDecrypt) || tags.contains(kSecACLAuthorizationAny) {
            try checked(SecACLSetContents(acl, [application] as CFArray, label as CFString,
                                         SecKeychainPromptSelector(rawValue: 0)))
            found = true
        }
    }
    guard found else { throw NSError(domain: "Janus", code: 3) }
}
func policyIsProtected(_ access: SecAccess) -> Bool {
    var list: CFArray?
    guard SecAccessCopyACLList(access, &list) == errSecSuccess else { return false }
    var found = false
    for acl in (list as? [SecACL] ?? []) {
        let tags = SecACLCopyAuthorizations(acl) as NSArray
        if tags.contains(kSecACLAuthorizationDecrypt) || tags.contains(kSecACLAuthorizationAny) {
            var apps: CFArray?
            var description: CFString?
            var selector = SecKeychainPromptSelector(rawValue: 0)
            guard SecACLCopyContents(acl, &apps, &description, &selector) == errSecSuccess,
                  let apps else { return false }
            // Persisted ACLs include a separate password-confirmation fallback.
            if CFArrayGetCount(apps) == 0 {
                guard selector.contains(SecKeychainPromptSelector(rawValue: 0x0001)) else { return false }
                continue
            }
            found = true
            // macOS may append the same helper again after Always Allow.
            // Check every trusted application rather than requiring one entry.
            for index in 0..<CFArrayGetCount(apps) {
                let trusted = unsafeBitCast(CFArrayGetValueAtIndex(apps, index), to: SecTrustedApplication.self)
                guard trustedApplicationMatchesSelf(trusted) else { return false }
            }
        }
    }
    return found
}
// All stored-password reads require fresh macOS user authentication, regardless
// of how Security.framework normalizes a persisted item's ACL flags.
func authorizedSecret(authorize: () -> Bool, read: () throws -> Data) throws -> Data {
    guard authorize() else { throw NSError(domain: "JanusAuthorization", code: 1) }
    return try read()
}

// Adapted from Confidential's passwordKeychainData: after owner authorization,
// fetch only the selected item and fail promptly if Security needs another UI.
func keychainInteractionAllowed() throws -> Bool {
    var allowed: DarwinBoolean = false
    try checked(SecKeychainGetUserInteractionAllowed(&allowed))
    return allowed.boolValue
}
func setKeychainInteraction(_ allowed: Bool) throws {
    try checked(SecKeychainSetUserInteractionAllowed(allowed))
}
func withoutKeychainUI<T>(_ operation: () throws -> T,
    getInteraction: () throws -> Bool = keychainInteractionAllowed,
    setInteraction: (Bool) throws -> Void = setKeychainInteraction) throws -> T {
    let previous = try getInteraction()
    try setInteraction(false)
    var restored = false
    defer { if !restored { try? setInteraction(previous) } }
    let result = try operation()
    try setInteraction(previous)
    restored = true
    return result
}
func savedPasswordQuery(_ item: Any, keychain: Any) -> [String: Any] {
    [kSecClass as String: kSecClassGenericPassword,
     kSecMatchSearchList as String: [keychain],
     kSecMatchItemList as String: [item], kSecMatchLimit as String: kSecMatchLimitOne,
     kSecReturnData as String: true, kSecUseAuthenticationUI as String: kSecUseAuthenticationUIFail]
}
func readSavedPassword(_ item: SecKeychainItem) throws -> Data {
    return try withoutKeychainUI {
        // Match the exact item's owning keychain, not the process default search list.
        var keychain: SecKeychain?
        try checked(SecKeychainItemCopyKeychain(item, &keychain))
        guard let keychain else { throw NSError(domain: "Janus", code: 2) }
        var result: CFTypeRef?
        try checked(SecItemCopyMatching(savedPasswordQuery(item, keychain: keychain) as CFDictionary, &result))
        guard var data = result as? Data else { throw NSError(domain: "Janus", code: 2) }
        guard !data.isEmpty, data.count <= 16384 else {
            data.resetBytes(in: 0..<data.count)
            throw NSError(domain: "Janus", code: 2)
        }
        return data
    }
}

// Lookup never requests secret data, so an existing entry can be discovered
// before owner authentication without invoking a legacy decrypt prompt.
func credentialReferenceQuery(_ keychain: Any, _ service: String, _ user: String) -> [String: Any] {
    [kSecClass as String: kSecClassGenericPassword,
     kSecMatchSearchList as String: [keychain],
     kSecAttrService as String: service, kSecAttrAccount as String: user,
     kSecReturnRef as String: true, kSecMatchLimit as String: kSecMatchLimitOne,
     kSecUseAuthenticationUI as String: kSecUseAuthenticationUIFail]
}

// Partition parsing and exact-rule selection extracted from Confidential.
func passwordPartitionValues(_ description: String) throws -> [String] {
    let bytes = Array(description.utf8)
    guard !bytes.isEmpty, bytes.count <= 131072, bytes.count % 2 == 0,
          bytes.allSatisfy({ (48...57).contains($0) || (65...70).contains($0) || (97...102).contains($0) }) else { throw NSError(domain: "JanusPartition", code: 1) }
    var data = Data()
    for index in stride(from: 0, to: bytes.count, by: 2) {
        guard let value = UInt8(String(bytes: bytes[index...index+1], encoding: .utf8) ?? "", radix: 16) else { throw NSError(domain: "JanusPartition", code: 1) }
        data.append(value)
    }
    var format = PropertyListSerialization.PropertyListFormat.xml
    guard let object = try PropertyListSerialization.propertyList(from: data, options: [], format: &format) as? [String: Any],
          format == .xml, Set(object.keys) == ["Partitions"], let values = object["Partitions"] as? [String],
          !values.isEmpty, values.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 1024 && !$0.contains("\0") }) else { throw NSError(domain: "JanusPartition", code: 1) }
    return values
}
func passwordCurrentCodeHash() throws -> Data {
    var code: SecCode?
    try checked(SecCodeCopySelf([], &code))
    guard let code else { throw NSError(domain: "JanusPartition", code: 2) }
    try checked(SecCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSStrictValidate), nil))
    var staticCode: SecStaticCode?
    try checked(SecCodeCopyStaticCode(code, [], &staticCode))
    guard let staticCode else { throw NSError(domain: "JanusPartition", code: 2) }
    var info: CFDictionary?
    try checked(SecCodeCopySigningInformation(staticCode, SecCSFlags(rawValue: kSecCSSigningInformation), &info))
    guard let values = info as? [String: Any], let hash = values[kSecCodeInfoUnique as String] as? Data,
          hash.count == 20 else { throw NSError(domain: "JanusPartition", code: 1) }
    return hash
}
func passwordPartitionACL(_ access: SecAccess) throws -> SecACL {
    var list: CFArray?
    try checked(SecAccessCopyACLList(access, &list))
    let partitions = (list as? [SecACL] ?? []).filter { (SecACLCopyAuthorizations($0) as NSArray).contains(kSecACLAuthorizationPartitionID) }
    // Do not invent an absent rule or reinterpret a mixed-authorization ACL.
    guard partitions.count == 1, CFArrayGetCount(SecACLCopyAuthorizations(partitions[0])) == 1 else { throw NSError(domain: "JanusPartition", code: 1) }
    return partitions[0]
}

func helperPartition(expectedIdentifier: String = "com.janus.ai.confidential.cli.keychain") throws -> String {
    var code: SecCode?
    try checked(SecCodeCopySelf([], &code))
    guard let code else { throw NSError(domain: "JanusPartition", code: 2) }
    try checked(SecCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSStrictValidate), nil))
    var staticCode: SecStaticCode?
    try checked(SecCodeCopyStaticCode(code, [], &staticCode))
    guard let staticCode else { throw NSError(domain: "JanusPartition", code: 2) }
    var info: CFDictionary?
    try checked(SecCodeCopySigningInformation(staticCode, SecCSFlags(rawValue: kSecCSSigningInformation), &info))
    guard let values = info as? [String: Any], values[kSecCodeInfoIdentifier as String] as? String == expectedIdentifier else { throw NSError(domain: "JanusPartition", code: 2) }
    // Derive the team from this validated signature, never from configuration or
    // the stored item. Exact trusted-application ACL remains required as well.
    if let team = values[kSecCodeInfoTeamIdentifier as String] as? String, !team.isEmpty {
        return "teamid:" + team
    }
    return "cdhash:" + (try passwordCurrentCodeHash()).map { String(format: "%02x", $0) }.joined()
}
func partitionDescription(_ partition: String) throws -> String {
    let data = try PropertyListSerialization.data(fromPropertyList: ["Partitions": [partition]], format: .xml, options: 0)
    return data.map { String(format: "%02x", $0) }.joined()
}
func partitionIsCurrent(_ access: SecAccess, _ expected: String) throws -> Bool {
    let rule = try passwordPartitionACL(access)
    var apps: CFArray?
    var description: CFString?
    var selector = SecKeychainPromptSelector(rawValue: 0)
    try checked(SecACLCopyContents(rule, &apps, &description, &selector))
    guard let description else { throw NSError(domain: "JanusPartition", code: 1) }
    return try passwordPartitionValues(description as String) == [expected]
}
func updatePartitionAccess(_ access: SecAccess, _ expected: String) throws {
    let rule = try passwordPartitionACL(access)
    var apps: CFArray?
    var description: CFString?
    var selector = SecKeychainPromptSelector(rawValue: 0)
    try checked(SecACLCopyContents(rule, &apps, &description, &selector))
    guard let description else { throw NSError(domain: "JanusPartition", code: 1) }
    _ = try passwordPartitionValues(description as String)
    try checked(SecACLSetContents(rule, apps, try partitionDescription(expected) as CFString, selector))
}

// These exported macOS Security SPIs expose the requirement hidden behind the
// legacy trusted-application reference. CopyData alone returns only its path.
@_silgen_name("SecTrustedApplicationCopyRequirement")
private func copyTrustedRequirement(_ application: SecTrustedApplication, _ requirement: UnsafeMutablePointer<SecRequirement?>) -> OSStatus
@_silgen_name("SecTrustedApplicationValidateWithPath")
private func validateTrustedPath(_ application: SecTrustedApplication, _ path: UnsafePointer<CChar>?) -> OSStatus
func trustedApplicationMatchesSelf(_ application: SecTrustedApplication) -> Bool {
    var requirement: SecRequirement?
    guard copyTrustedRequirement(application, &requirement) == errSecSuccess else { return false }
    if let requirement {
        var code: SecCode?
        guard SecCodeCopySelf([], &code) == errSecSuccess, let code else { return false }
        var staticCode: SecStaticCode?
        guard SecCodeCopyStaticCode(code, [], &staticCode) == errSecSuccess, let staticCode else { return false }
        var expected: SecRequirement?
        guard SecCodeCopyDesignatedRequirement(staticCode, [], &expected) == errSecSuccess,
              let expected else { return false }
        var actualText: CFString?, expectedText: CFString?
        // Installation paths change across Homebrew versions. Compare the exact
        // designated identity instead; a broad requirement that merely accepts
        // this helper must not authorize additional applications.
        guard SecRequirementCopyString(requirement, [], &actualText) == errSecSuccess,
              SecRequirementCopyString(expected, [], &expectedText) == errSecSuccess,
              let actualText, let expectedText, CFEqual(actualText, expectedText) else { return false }
        return SecCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSStrictValidate), requirement) == errSecSuccess
    }
    // Preserve path-bound validation for legacy unsigned trusted applications.
    var expected: SecTrustedApplication?
    var actualData: CFData?, expectedData: CFData?
    guard SecTrustedApplicationCreateFromPath(nil, &expected) == errSecSuccess, let expected,
          SecTrustedApplicationCopyData(application, &actualData) == errSecSuccess,
          SecTrustedApplicationCopyData(expected, &expectedData) == errSecSuccess,
          let actualData, let expectedData, CFEqual(actualData, expectedData) else { return false }
    return validateTrustedPath(application, nil) == errSecSuccess
}

func canEnrollSeparateCredential(hasOwnItem: Bool, stage: String, domain: String, code: Int) -> Bool {
    guard !hasOwnItem, domain == NSOSStatusErrorDomain,
          ["legacy_credential_incompatible", "acl_commit", "credential_read"].contains(stage),
          let status = OSStatus(exactly: code) else { return false }
    return [errSecAuthFailed, errSecInteractionNotAllowed, errSecNoAccessForItem].contains(status)
}

// Some legacy ACL updates report authentication failure after applying changes.
// Recovery requires verified state, a new explicit owner approval, and a second
// state check. The caller must still use the normal Keychain read API.
func commitVerifiedAccess(commit: () -> OSStatus, verify: () -> Bool,
                          reauthorize: () -> Bool) throws {
    let status = commit()
    if status != errSecSuccess {
        guard status == errSecAuthFailed, verify() else { try checked(status); return }
        guard reauthorize() else { throw NSError(domain: "JanusAuthorization", code: 1) }
    }
    guard verify() else { throw NSError(domain: "Janus", code: 4) }
}
