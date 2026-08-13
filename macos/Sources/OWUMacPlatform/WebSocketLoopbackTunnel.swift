#if os(macOS)
import CryptoKit
import Foundation
import Network
import OWUCore
import Security

public final class WebSocketLoopbackTunnel: @unchecked Sendable {
    public typealias StateHandler = @Sendable (OWUTunnelState) -> Void

    private let preset: OWUTunnelPreset
    private let server: OWUServerConfiguration
    private let queue: DispatchQueue
    private let lock = NSLock()
    private var listener: NWListener?
    private var flows: [UUID: TunnelFlow] = [:]
    private var session: URLSession?
    private var sessionDelegate: PinnedServerDelegate?
    private let stateHandler: StateHandler

    public init(
        preset: OWUTunnelPreset,
        server: OWUServerConfiguration,
        stateHandler: @escaping StateHandler
    ) {
        self.preset = preset
        self.server = server
        self.stateHandler = stateHandler
        self.queue = DispatchQueue(label: "com.openwebsiteunblocker.tunnel.\(preset.id)")
    }

    public func start() throws {
        lock.lock()
        defer { lock.unlock() }
        guard listener == nil else { return }
        stateHandler(.starting)

        guard let port = NWEndpoint.Port(rawValue: preset.localPort) else {
            stateHandler(.failed("Invalid local port."))
            throw OWUConfigurationError.invalidLocalPort
        }
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: port)
        let newListener = try NWListener(using: parameters)
        let delegate = PinnedServerDelegate(
            expectedHost: server.baseURL.host ?? "",
            certificateSHA256: server.certificateSHA256
        )
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 24 * 60 * 60
        configuration.httpCookieStorage = nil
        configuration.urlCache = nil
        let newSession = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        sessionDelegate = delegate
        session = newSession
        listener = newListener

        newListener.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            switch state {
            case .ready:
                self.stateHandler(.ready)
            case let .failed(error):
                self.stop()
                self.stateHandler(.failed(error.localizedDescription))
            case .cancelled:
                break
            default:
                break
            }
        }
        newListener.newConnectionHandler = { [weak self] connection in
            self?.accept(connection)
        }
        newListener.start(queue: queue)
    }

    public func stop() {
        lock.lock()
        let currentListener = listener
        listener = nil
        let currentFlows = Array(flows.values)
        flows.removeAll()
        let currentSession = session
        session = nil
        sessionDelegate = nil
        lock.unlock()

        currentListener?.cancel()
        currentFlows.forEach { $0.stop() }
        currentSession?.invalidateAndCancel()
        stateHandler(.stopped)
    }

    private func accept(_ connection: NWConnection) {
        do {
            guard let session else { throw OWUConfigurationError.invalidServer }
            let request = try server.tunnelRequest(resourceID: preset.id)
            let task = session.webSocketTask(with: request)
            let id = UUID()
            let flow = TunnelFlow(id: id, local: connection, remote: task, queue: queue) { [weak self] id in
                self?.removeFlow(id)
            }
            lock.lock()
            flows[id] = flow
            lock.unlock()
            flow.start()
        } catch {
            connection.cancel()
            stateHandler(.failed(error.localizedDescription))
        }
    }

    private func removeFlow(_ id: UUID) {
        lock.lock()
        flows.removeValue(forKey: id)
        lock.unlock()
    }
}

private final class TunnelFlow: @unchecked Sendable {
    private let id: UUID
    private let local: NWConnection
    private let remote: URLSessionWebSocketTask
    private let queue: DispatchQueue
    private let onStop: @Sendable (UUID) -> Void
    private let stopLock = NSLock()
    private var stopped = false

    init(
        id: UUID,
        local: NWConnection,
        remote: URLSessionWebSocketTask,
        queue: DispatchQueue,
        onStop: @escaping @Sendable (UUID) -> Void
    ) {
        self.id = id
        self.local = local
        self.remote = remote
        self.queue = queue
        self.onStop = onStop
    }

    func start() {
        remote.resume()
        local.stateUpdateHandler = { [weak self] state in
            switch state {
            case .ready:
                self?.receiveLocal()
                self?.receiveRemote()
            case .failed, .cancelled:
                self?.stop()
            default:
                break
            }
        }
        local.start(queue: queue)
    }

    func stop() {
        stopLock.lock()
        guard !stopped else { stopLock.unlock(); return }
        stopped = true
        stopLock.unlock()
        local.cancel()
        remote.cancel(with: .goingAway, reason: nil)
        onStop(id)
    }

    private func receiveLocal() {
        local.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, isComplete, error in
            guard let self else { return }
            if let data, !data.isEmpty {
                self.remote.send(.data(data)) { [weak self] sendError in
                    if sendError != nil { self?.stop(); return }
                    if isComplete { self?.stop() } else { self?.receiveLocal() }
                }
            } else if error != nil || isComplete {
                self.stop()
            } else {
                self.receiveLocal()
            }
        }
    }

    private func receiveRemote() {
        remote.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case let .success(message):
                let data: Data
                switch message {
                case let .data(value): data = value
                case let .string(value): data = Data(value.utf8)
                @unknown default: self.stop(); return
                }
                self.local.send(content: data, completion: .contentProcessed { [weak self] error in
                    if error != nil { self?.stop() } else { self?.receiveRemote() }
                })
            case .failure:
                self.stop()
            }
        }
    }
}

private final class PinnedServerDelegate: NSObject, URLSessionDelegate, @unchecked Sendable {
    private let expectedHost: String
    private let certificateSHA256: String?

    init(expectedHost: String, certificateSHA256: String?) {
        self.expectedHost = expectedHost.lowercased()
        self.certificateSHA256 = certificateSHA256
    }

    func urlSession(
        _ session: URLSession,
        didReceive challenge: URLAuthenticationChallenge,
        completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void
    ) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              challenge.protectionSpace.host.lowercased() == expectedHost,
              let trust = challenge.protectionSpace.serverTrust else {
            completionHandler(.performDefaultHandling, nil)
            return
        }
        guard let certificateSHA256 else {
            completionHandler(.performDefaultHandling, nil)
            return
        }
        guard let leaf = SecTrustGetCertificateAtIndex(trust, 0) else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        let data = SecCertificateCopyData(leaf) as Data
        let digest = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
        guard constantTimeEqual(digest, certificateSHA256) else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        completionHandler(.useCredential, URLCredential(trust: trust))
    }

    private func constantTimeEqual(_ left: String, _ right: String) -> Bool {
        guard left.utf8.count == right.utf8.count else { return false }
        return zip(left.utf8, right.utf8).reduce(UInt8(0)) { $0 | ($1.0 ^ $1.1) } == 0
    }
}
#endif
