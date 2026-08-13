#if os(macOS)
import CryptoKit
import Foundation
import PermitCore
import Security

public actor KeychainDeviceIdentityProvider: DeviceIdentityProviding {
    private let applicationTag: Data
    private let keyTag: String
    private let accessGroup: String?

    public init(
        applicationTag: String = "com.permit.access-client.device-key",
        accessGroup: String? = nil
    ) {
        self.applicationTag = Data(applicationTag.utf8)
        self.keyTag = applicationTag
        self.accessGroup = accessGroup
    }

    public func prepareIdentity() async throws -> DeviceIdentityReference {
        let privateKey: SecKey
        let hardwareBacked: Bool
        if let existing = try loadPrivateKey() {
            privateKey = existing
            hardwareBacked = isSecureEnclaveKey(existing)
        } else if let secureEnclaveKey = try createSecureEnclaveKeyIfAvailable() {
            privateKey = secureEnclaveKey
            hardwareBacked = true
        } else {
            privateKey = try createSoftwareKey()
            hardwareBacked = false
        }

        guard let publicKey = SecKeyCopyPublicKey(privateKey) else {
            throw StableAccessError.deviceUnavailable
        }
        var exportError: Unmanaged<CFError>?
        guard let publicData = SecKeyCopyExternalRepresentation(publicKey, &exportError) as Data? else {
            if let exportError { throw exportError.takeRetainedValue() }
            throw StableAccessError.deviceUnavailable
        }
        let digest = SHA256.hash(data: publicData)
        let thumbprint = digest.map { String(format: "%02x", $0) }.joined()
        return DeviceIdentityReference(
            keyTag: keyTag,
            publicKey: publicData,
            thumbprint: thumbprint,
            isHardwareBacked: hardwareBacked
        )
    }

    public func sign(_ challenge: Data) async throws -> Data {
        guard let key = try loadPrivateKey() else {
            throw StableAccessError.deviceUnavailable
        }
        var signingError: Unmanaged<CFError>?
        guard let signature = SecKeyCreateSignature(
            key,
            .ecdsaSignatureMessageX962SHA256,
            challenge as CFData,
            &signingError
        ) as Data? else {
            if let signingError { throw signingError.takeRetainedValue() }
            throw StableAccessError.deviceUnavailable
        }
        return signature
    }

    public func deleteIdentity() async throws {
        let status = SecItemDelete(baseQuery() as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainIdentityError.status(status)
        }
    }

    private func loadPrivateKey() throws -> SecKey? {
        var query = baseQuery()
        query[kSecReturnRef as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let result else {
            throw KeychainIdentityError.status(status)
        }
        return (result as! SecKey)
    }

    private func createSecureEnclaveKeyIfAvailable() throws -> SecKey? {
        var accessError: Unmanaged<CFError>?
        guard let access = SecAccessControlCreateWithFlags(
            nil,
            kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            [.privateKeyUsage],
            &accessError
        ) else {
            if let accessError { throw accessError.takeRetainedValue() }
            throw StableAccessError.deviceUnavailable
        }

        var privateAttributes: [String: Any] = [
            kSecAttrIsPermanent as String: true,
            kSecAttrApplicationTag as String: applicationTag,
            kSecAttrAccessControl as String: access
        ]
        if let accessGroup {
            privateAttributes[kSecAttrAccessGroup as String] = accessGroup
        }
        let attributes: [String: Any] = [
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecAttrKeySizeInBits as String: 256,
            kSecAttrTokenID as String: kSecAttrTokenIDSecureEnclave,
            kSecPrivateKeyAttrs as String: privateAttributes
        ]
        var creationError: Unmanaged<CFError>?
        let key = SecKeyCreateRandomKey(attributes as CFDictionary, &creationError)
        if key == nil {
            _ = creationError?.takeRetainedValue()
        }
        return key
    }

    private func createSoftwareKey() throws -> SecKey {
        var privateAttributes: [String: Any] = [
            kSecAttrIsPermanent as String: true,
            kSecAttrApplicationTag as String: applicationTag,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        ]
        if let accessGroup {
            privateAttributes[kSecAttrAccessGroup as String] = accessGroup
        }
        let attributes: [String: Any] = [
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecAttrKeySizeInBits as String: 256,
            kSecPrivateKeyAttrs as String: privateAttributes
        ]
        var creationError: Unmanaged<CFError>?
        guard let key = SecKeyCreateRandomKey(attributes as CFDictionary, &creationError) else {
            if let creationError { throw creationError.takeRetainedValue() }
            throw StableAccessError.deviceUnavailable
        }
        return key
    }

    private func baseQuery() -> [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassKey,
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecAttrApplicationTag as String: applicationTag
        ]
        if let accessGroup {
            query[kSecAttrAccessGroup as String] = accessGroup
        }
        return query
    }

    private func isSecureEnclaveKey(_ key: SecKey) -> Bool {
        guard let attributes = SecKeyCopyAttributes(key) as? [String: Any] else { return false }
        return (attributes[kSecAttrTokenID as String] as? String) == (kSecAttrTokenIDSecureEnclave as String)
    }
}

public enum KeychainIdentityError: Error, LocalizedError {
    case status(OSStatus)

    public var errorDescription: String? {
        switch self {
        case let .status(status):
            return SecCopyErrorMessageString(status, nil) as String? ?? "Keychain error \(status)."
        }
    }
}
#endif
