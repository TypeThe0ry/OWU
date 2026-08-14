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
            let requests = try server.tunnelRequests(resourceID: preset.id)
            let id = UUID()
            let flow = TunnelFlow(
                id: id,
                local: connection,
                session: session,
                requests: requests,
                queue: queue
            ) { [weak self] id in self?.removeFlow(id) }
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
    private let session: URLSession
    private let requests: [URLRequest]
    private let queue: DispatchQueue
    private let onStop: @Sendable (UUID) -> Void
    private let stateLock = NSLock()
    private var stopped = false
    private var connected = false
    private var nextRequestIndex = 0
    private var remote: URLSessionWebSocketTask?

    init(
        id: UUID,
        local: NWConnection,
        session: URLSession,
        requests: [URLRequest],
        queue: DispatchQueue,
        onStop: @escaping @Sendable (UUID) -> Void
    ) {
        self.id = id
        self.local = local
        self.session = session
        self.requests = requests
        self.queue = queue
        self.onStop = onStop
    }

    func start() {
        local.stateUpdateHandler = { [weak self] state in
            switch state {
            case .ready:
                self?.connectNextGateway()
            case .failed, .cancelled:
                self?.stop()
            default:
                break
            }
        }
        local.start(queue: queue)
    }

    func stop() {
        stateLock.lock()
        guard !stopped else { stateLock.unlock(); return }
        stopped = true
        let currentRemote = remote
        remote = nil
        stateLock.unlock()
        local.cancel()
        currentRemote?.cancel(with: .goingAway, reason: nil)
        onStop(id)
    }

    /// Tries each configured WSS endpoint in order. `sendPing` is used as the
    /// handshake probe so local application bytes are not consumed until a
    /// TLS and WebSocket connection is fully usable.
    private func connectNextGateway() {
        stateLock.lock()
        guard !stopped, !connected else { stateLock.unlock(); return }
        guard nextRequestIndex < requests.count else {
            stateLock.unlock()
            stop()
            return
        }
        let request = requests[nextRequestIndex]
        nextRequestIndex += 1
        stateLock.unlock()

        let task = session.webSocketTask(with: request)
        stateLock.lock()
        guard !stopped else {
            stateLock.unlock()
            task.cancel(with: .goingAway, reason: nil)
            return
        }
        remote = task
        stateLock.unlock()
        task.resume()
        task.sendPing { [weak self, task] error in
            guard let self else { return }
            self.queue.async {
                self.finishGatewayProbe(task: task, error: error)
            }
        }
    }

    private func finishGatewayProbe(task: URLSessionWebSocketTask, error: Error?) {
        stateLock.lock()
        guard !stopped, remote === task else {
            stateLock.unlock()
            task.cancel(with: .goingAway, reason: nil)
            return
        }
        if error != nil {
            remote = nil
            stateLock.unlock()
            task.cancel(with: .goingAway, reason: nil)
            connectNextGateway()
            return
        }
        connected = true
        stateLock.unlock()
        receiveLocal()
        receiveRemote()
    }

    private func receiveLocal() {
        local.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, isComplete, error in
            self?.queue.async {
                self?.handleLocal(data: data, isComplete: isComplete, error: error)
            }
        }
    }

    private func handleLocal(data: Data?, isComplete: Bool, error: NWError?) {
        if let data, !data.isEmpty {
            guard let remote = currentRemote() else { stop(); return }
            remote.send(.data(data)) { [weak self] sendError in
                self?.queue.async {
                    guard let self else { return }
                    if sendError != nil || isComplete { self.stop() } else { self.receiveLocal() }
                }
            }
        } else if error != nil || isComplete {
            stop()
        } else {
            receiveLocal()
        }
    }

    private func receiveRemote() {
        guard let remote = currentRemote() else { stop(); return }
        remote.receive { [weak self] result in
            self?.queue.async {
                self?.handleRemote(result)
            }
        }
    }

    private func handleRemote(_ result: Result<URLSessionWebSocketTask.Message, Error>) {
        switch result {
        case let .success(message):
            let data: Data
            switch message {
            case let .data(value): data = value
            case let .string(value): data = Data(value.utf8)
            @unknown default: stop(); return
            }
            local.send(content: data, completion: .contentProcessed { [weak self] error in
                self?.queue.async {
                    if error != nil { self?.stop() } else { self?.receiveRemote() }
                }
            })
        case .failure:
            stop()
        }
    }

    private func currentRemote() -> URLSessionWebSocketTask? {
        stateLock.lock()
        defer { stateLock.unlock() }
        return stopped ? nil : remote
    }
}

private final class PinnedServerDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    private let expectedHost: String
    private let certificateSHA256: String?

    init(expectedHost: String, certificateSHA256: String?) {
        self.expectedHost = expectedHost.lowercased()
        self.certificateSHA256 = certificateSHA256
    }

    /// Tunnel handshakes never follow redirects. In particular, this prevents
    /// `X-OWU-Tunnel-Key` from being replayed to a redirect destination.
    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest,
        completionHandler: @escaping (URLRequest?) -> Void
    ) {
        completionHandler(nil)
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
