#if os(macOS)
import Foundation
import Security

public enum OWUCredentialStoreError: Error {
    case unhandled(OSStatus)
    case invalidData
}

public enum OWUCredentialKind: String, Sendable {
    case browserPassword = "browser-password"
    case tunnelKey = "tunnel-key"
}

public struct OWUCredentialStore: Sendable {
    private let service = "com.openwebsiteunblocker.credentials"

    public init() {}

    public func save(
        secret: String,
        kind: OWUCredentialKind,
        serverHost: String,
        username: String
    ) throws {
        let account = accountName(kind: kind, serverHost: serverHost, username: username)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecUseDataProtectionKeychain as String: true,
        ]
        let attributes: [String: Any] = [kSecValueData as String: Data(secret.utf8)]
        let status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            var add = query
            add[kSecValueData as String] = Data(secret.utf8)
            add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            let addStatus = SecItemAdd(add as CFDictionary, nil)
            guard addStatus == errSecSuccess else { throw OWUCredentialStoreError.unhandled(addStatus) }
        } else if status != errSecSuccess {
            throw OWUCredentialStoreError.unhandled(status)
        }
    }

    public func load(
        kind: OWUCredentialKind,
        serverHost: String,
        username: String
    ) throws -> String? {
        let account = accountName(kind: kind, serverHost: serverHost, username: username)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecUseDataProtectionKeychain as String: true,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else { throw OWUCredentialStoreError.unhandled(status) }
        guard let data = result as? Data, let secret = String(data: data, encoding: .utf8) else {
            throw OWUCredentialStoreError.invalidData
        }
        return secret
    }

    private func accountName(
        kind: OWUCredentialKind,
        serverHost: String,
        username: String
    ) -> String {
        "\(kind.rawValue):\(username)@\(serverHost.lowercased())"
    }
}
#endif
