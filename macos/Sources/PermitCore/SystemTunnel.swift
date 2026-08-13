import Foundation

public struct ApprovedTunnelConfiguration: Sendable, Equatable {
    public let overlayCIDRs: [String]
    public let privateDNSSuffixes: [String]

    public init(overlayCIDRs: [String], privateDNSSuffixes: [String]) throws {
        guard !overlayCIDRs.contains("0.0.0.0/0"),
              !overlayCIDRs.contains("::/0"),
              !privateDNSSuffixes.contains("") else {
            throw StableAccessError.resourceNotAuthorized
        }
        self.overlayCIDRs = overlayCIDRs
        self.privateDNSSuffixes = privateDNSSuffixes
    }
}

public enum SystemTunnelState: Equatable, Sendable {
    case unavailable
    case disconnected
    case connecting
    case connected
    case reconnecting
    case failed(message: String)
}

public protocol SystemTunnelManaging: Sendable {
    func state() async -> SystemTunnelState
    func start(configuration: ApprovedTunnelConfiguration) async throws
    func stop() async
}

public actor UnavailableSystemTunnelManager: SystemTunnelManaging {
    public init() {}

    public func state() async -> SystemTunnelState { .unavailable }

    public func start(configuration: ApprovedTunnelConfiguration) async throws {
        throw StableAccessError.systemTunnelNotEntitled
    }

    public func stop() async {}
}
