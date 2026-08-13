import Foundation

public struct DeviceIdentityReference: Sendable, Equatable {
    public let keyTag: String
    public let publicKey: Data
    public let thumbprint: String
    public let isHardwareBacked: Bool

    public init(keyTag: String, publicKey: Data, thumbprint: String, isHardwareBacked: Bool) {
        self.keyTag = keyTag
        self.publicKey = publicKey
        self.thumbprint = thumbprint
        self.isHardwareBacked = isHardwareBacked
    }
}

public protocol DeviceIdentityProviding: Sendable {
    func prepareIdentity() async throws -> DeviceIdentityReference
    func sign(_ challenge: Data) async throws -> Data
    func deleteIdentity() async throws
}

public enum AuthenticationState: Equatable, Sendable {
    case signedOut
    case authenticating
    case enrolling
    case signedIn(userDisplayName: String)
    case failed(message: String)
}

public enum AuthenticationEvent: Sendable {
    case beginSignIn
    case authorizationSucceeded
    case enrollmentSucceeded(userDisplayName: String)
    case failed(message: String)
    case signOut
}

public struct AuthenticationStateMachine: Sendable {
    public private(set) var state: AuthenticationState = .signedOut

    public init() {}

    @discardableResult
    public mutating func handle(_ event: AuthenticationEvent) -> AuthenticationState {
        switch (state, event) {
        case (_, .signOut):
            state = .signedOut
        case (.signedOut, .beginSignIn):
            state = .authenticating
        case (.authenticating, .authorizationSucceeded):
            state = .enrolling
        case let (.enrolling, .enrollmentSucceeded(name)):
            state = .signedIn(userDisplayName: name)
        case let (_, .failed(message)):
            state = .failed(message: message)
        default:
            break
        }
        return state
    }
}
