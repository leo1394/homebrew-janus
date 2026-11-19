// In-memory ACL regression: no Keychain items or passwords are read or written.
import Foundation
import Security

func accessForPaths(_ paths: [String]) throws -> SecAccess {
    var apps: [SecTrustedApplication] = []
    for path in paths {
        var app: SecTrustedApplication?
        try checked(SecTrustedApplicationCreateFromPath(path, &app))
        guard let app else { throw NSError(domain: "Test", code: 1) }
        apps.append(app)
    }
    var access: SecAccess?
    try checked(SecAccessCreate("Janus ACL relocation test" as CFString, apps as CFArray, &access))
    guard let access else { throw NSError(domain: "Test", code: 2) }
    return access
}
do {
    guard CommandLine.arguments.count == 4 else { fatalError("expected relocated, foreign, and broad-requirement helper paths") }
    guard policyIsProtected(try makeAccess()) else { fatalError("self ACL rejected") }
    guard policyIsProtected(try accessForPaths([CommandLine.arguments[1]])) else { fatalError("same signed identity at another path rejected") }
    guard !policyIsProtected(try accessForPaths([CommandLine.arguments[2]])) else { fatalError("foreign signed identity accepted") }
    guard !policyIsProtected(try accessForPaths([CommandLine.arguments[3]])) else { fatalError("broad signing requirement accepted") }
    guard !policyIsProtected(try accessForPaths([CommandLine.arguments[1], CommandLine.arguments[2]])) else { fatalError("mixed trusted identities accepted") }
    var approvals = 0
    var checks = 0
    try commitVerifiedAccess(commit: { errSecAuthFailed }, verify: { checks += 1; return true }, reauthorize: { approvals += 1; return true })
    guard approvals == 1 && checks == 2 else { fatalError("recovery skipped approval or readback") }
    for status in [errSecAuthFailed, errSecUserCanceled, errSecInteractionNotAllowed] {
        approvals = 0
        do {
            try commitVerifiedAccess(commit: { status }, verify: { false }, reauthorize: { approvals += 1; return true })
            fatalError("incomplete ACL accepted")
        } catch { guard approvals == 0 else { fatalError("incomplete ACL requested recovery") } }
    }
    do {
        try commitVerifiedAccess(commit: { errSecAuthFailed }, verify: { true }, reauthorize: { false })
        fatalError("cancelled recovery accepted")
    } catch {}
    checks = 0
    do {
        try commitVerifiedAccess(commit: { errSecAuthFailed }, verify: { checks += 1; return checks == 1 }, reauthorize: { true })
        fatalError("changed ACL after approval accepted")
    } catch {}
    do {
        try commitVerifiedAccess(commit: { errSecUserCanceled }, verify: { true }, reauthorize: { fatalError("cancelled commit retried") })
        fatalError("cancelled commit accepted")
    } catch {}
    print("PASS: self, relocation, foreign identity, broad requirement, and mixed ACL policies")
} catch { fatalError("ACL test failed: \(error)") }
