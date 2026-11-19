import Foundation
import Security

func require(_ condition: Bool, _ message: String) {
    if !condition { fatalError(message) }
}
var interaction = true
var history: [Bool] = []
let result = try withoutKeychainUI({
    require(!interaction, "read must not allow implicit Keychain UI")
    return 42
}, getInteraction: { interaction }, setInteraction: { interaction = $0; history.append($0) })
require(result == 42 && interaction && history == [false, true], "successful read must restore UI state")
history = []
do {
    let _: Int = try withoutKeychainUI({ throw NSError(domain: "test", code: 1) },
        getInteraction: { interaction }, setInteraction: { interaction = $0; history.append($0) })
    fatalError("failure must propagate")
} catch {}
require(interaction && history == [false, true], "failed read must restore UI state")
interaction = false
let _: Int = try withoutKeychainUI({ 1 }, getInteraction: { interaction }, setInteraction: { interaction = $0 })
require(!interaction, "do not enable UI when it was originally disabled")
var read = false
do {
    _ = try authorizedSecret(authorize: { false }, read: { read = true; return Data([1]) })
    fatalError("unauthorized read succeeded")
} catch {}
require(!read, "cancelled owner authentication must never reach credential access")
let query = savedPasswordQuery("fixture-item")
require((query[kSecMatchItemList as String] as? [String]) == ["fixture-item"], "query must target the exact selected item")
require((query[kSecUseAuthenticationUI as String] as? String) == (kSecUseAuthenticationUIFail as String), "query must fail rather than open another UI")
require(query[kSecAttrService as String] == nil, "never broaden read to a service lookup")
print("PASS: scoped UI suppression, restoration on failure, exact item query, authorization gate")
let lookup = credentialReferenceQuery("fixture-keychain", "fixture-service", "fixture-user")
require(lookup[kSecReturnData as String] == nil, "lookup must not request a secret")
require((lookup[kSecReturnRef as String] as? Bool) == true, "lookup must return an item reference")
require((lookup[kSecMatchSearchList as String] as? [String]) == ["fixture-keychain"], "lookup must use the login keychain only")
require((lookup[kSecAttrAccount as String] as? String) == "fixture-user", "lookup must target the exact account")
require((lookup[kSecUseAuthenticationUI as String] as? String) == (kSecUseAuthenticationUIFail as String), "lookup must not prompt")
let expectedPartition = "cdhash:" + String(repeating: "12", count: 20)
let encodedPartition = try partitionDescription(expectedPartition)
require(try passwordPartitionValues(encodedPartition) == [expectedPartition], "partition must authorize only the current helper hash")
for malformed in ["", "not-hex", "0", "0000"] {
    do { _ = try passwordPartitionValues(malformed); fatalError("malformed partition accepted") } catch {}
}
@_silgen_name("SecTrustedApplicationCreateFromRequirement")
func fixtureTrustedApplication(_ description: UnsafePointer<CChar>, _ requirement: SecRequirement, _ application: UnsafeMutablePointer<SecTrustedApplication?>) -> OSStatus
var wrongRequirement: SecRequirement?
try checked(SecRequirementCreateWithString("identifier \"janus.test.nonmatching-identity\"" as CFString, [], &wrongRequirement))
var wrongApplication: SecTrustedApplication?
try CommandLine.arguments[0].withCString { path in
    try checked(fixtureTrustedApplication(path, wrongRequirement!, &wrongApplication))
}
require(!trustedApplicationMatchesSelf(wrongApplication!), "same path must not make a stale signature requirement valid")
var ownApplication: SecTrustedApplication?
try checked(SecTrustedApplicationCreateFromPath(nil, &ownApplication))
require(trustedApplicationMatchesSelf(ownApplication!), "current helper identity must validate")
print("PASS: trusted application signature requirement, not just path, is enforced")
require(canEnrollSeparateCredential(hasOwnItem: false, stage: "legacy_credential_incompatible", domain: NSOSStatusErrorDomain, code: Int(errSecNoAccessForItem)), "incompatible legacy item may offer explicit separate enrollment")
require(!canEnrollSeparateCredential(hasOwnItem: true, stage: "credential_read", domain: NSOSStatusErrorDomain, code: Int(errSecAuthFailed)), "never overwrite an existing base credential through recovery")
require(!canEnrollSeparateCredential(hasOwnItem: false, stage: "owner_authorization", domain: "JanusAuthorization", code: 1), "owner cancellation must not fall back to enrollment")
require(!canEnrollSeparateCredential(hasOwnItem: false, stage: "credential_read", domain: NSOSStatusErrorDomain, code: Int(errSecUserCanceled)), "system cancellation must not fall back to enrollment")
print("PASS: legacy recovery is explicit and cannot override existing base credentials or cancelled authorization")
