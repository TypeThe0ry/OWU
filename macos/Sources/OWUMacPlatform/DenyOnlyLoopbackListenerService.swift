#if os(macOS)
import Foundation
import Network
import OWUCore

/// A fail-closed listener scaffold. It proves loopback-only binding and lifecycle wiring,
/// but deliberately closes every accepted connection until protocol parsers, rotating
/// authentication, route-grant resolution, and the gateway transport are integrated.
public actor DenyOnlyLoopbackListenerService: LocalProxyServicing {
    private let configurations: [LoopbackListenerConfiguration]
    private var listeners: [NWListener] = []
    private var readyListenerCount = 0
    private var proxyState: LocalProxyState = .stopped
    private let queue = DispatchQueue(label: "app.owu.mac-client.loopback-listeners")

    public init(configurations: [LoopbackListenerConfiguration]) {
        self.configurations = configurations
    }

    public func start() async throws {
        guard proxyState == .stopped else { return }
        guard !configurations.isEmpty else { throw StableAccessError.proxyNotConfigured }
        proxyState = .starting
        readyListenerCount = 0

        do {
            listeners = try configurations.map(makeListener)
            for listener in listeners {
                listener.newConnectionHandler = { connection in
                    // No raw destination can leave the Mac in this scaffold.
                    connection.cancel()
                }
                listener.stateUpdateHandler = { [weak self] state in
                    Task { await self?.observe(state) }
                }
                listener.start(queue: queue)
            }
        } catch {
            listeners.forEach { $0.cancel() }
            listeners.removeAll()
            proxyState = .failed(message: error.localizedDescription)
            throw error
        }
    }

    public func stop() async {
        proxyState = .stopping
        listeners.forEach { $0.cancel() }
        listeners.removeAll()
        readyListenerCount = 0
        proxyState = .stopped
    }

    public func currentState() async -> LocalProxyState {
        proxyState
    }

    private func makeListener(_ configuration: LoopbackListenerConfiguration) throws -> NWListener {
        let parameters = NWParameters.tcp
        guard let port = NWEndpoint.Port(rawValue: configuration.port) else {
            throw StableAccessError.proxyNotConfigured
        }
        parameters.requiredLocalEndpoint = .hostPort(
            host: NWEndpoint.Host(configuration.address.rawValue),
            port: port
        )
        return try NWListener(using: parameters)
    }

    private func observe(_ state: NWListener.State) {
        switch state {
        case .ready:
            readyListenerCount += 1
            if readyListenerCount == listeners.count {
                proxyState = .ready
            }
        case let .failed(error):
            proxyState = .failed(message: error.localizedDescription)
        case .cancelled:
            if listeners.isEmpty { proxyState = .stopped }
        default:
            break
        }
    }
}
#endif
