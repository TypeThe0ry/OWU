#if os(macOS)
import Combine
import Foundation
import OWUCore
import OWUMacPlatform

@MainActor
final class AppModel: ObservableObject {
    @Published var serverAddress: String
    @Published var username: String
    @Published var browserPassword = ""
    @Published var tunnelKey = ""
    @Published var certificateFingerprint: String
    @Published var additionalGatewayPorts: String
    @Published private(set) var states: [String: OWUTunnelState] = [:]
    @Published var notice: String?

    let presets = OWUTunnelPreset.defaults
    private let defaults = UserDefaults.standard
    private let credentialStore = OWUCredentialStore()
    private var tunnels: [String: WebSocketLoopbackTunnel] = [:]

    init() {
        serverAddress = defaults.string(forKey: "owu.server") ?? "https://owu.example.com"
        username = defaults.string(forKey: "owu.username") ?? "owu"
        certificateFingerprint = defaults.string(forKey: "owu.certificateSHA256") ?? ""
        additionalGatewayPorts = defaults.string(forKey: "owu.additionalGatewayPorts")
            ?? OWUGatewayPortPlan.recommendedAdditionalPorts.map(String.init).joined(separator: ", ")
        for preset in presets { states[preset.id] = .stopped }
        loadSavedCredentials()
    }

    func state(for preset: OWUTunnelPreset) -> OWUTunnelState {
        states[preset.id] ?? .stopped
    }

    func toggle(_ preset: OWUTunnelPreset) {
        if tunnels[preset.id] != nil {
            stop(preset)
        } else {
            start(preset)
        }
    }

    func stopAll() {
        for preset in presets { stop(preset) }
    }

    private func start(_ preset: OWUTunnelPreset) {
        do {
            let configuration = try makeConfiguration()
            save(configuration)
            let tunnel = WebSocketLoopbackTunnel(preset: preset, server: configuration) { [weak self] state in
                Task { @MainActor in self?.states[preset.id] = state }
            }
            tunnels[preset.id] = tunnel
            try tunnel.start()
        } catch {
            tunnels[preset.id] = nil
            states[preset.id] = .failed(error.localizedDescription)
            notice = error.localizedDescription
        }
    }

    private func stop(_ preset: OWUTunnelPreset) {
        tunnels.removeValue(forKey: preset.id)?.stop()
        states[preset.id] = .stopped
    }

    private func makeConfiguration() throws -> OWUServerConfiguration {
        guard let url = URL(string: serverAddress.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            throw OWUConfigurationError.invalidServer
        }
        let additionalPorts = try OWUGatewayPortPlan.parseAdditionalPorts(additionalGatewayPorts)
        return try OWUServerConfiguration(
            baseURL: url,
            username: username,
            browserPassword: browserPassword,
            tunnelKey: tunnelKey,
            certificateSHA256: certificateFingerprint,
            additionalGatewayPorts: additionalPorts
        )
    }

    private func save(_ configuration: OWUServerConfiguration) {
        defaults.set(configuration.baseURL.absoluteString, forKey: "owu.server")
        defaults.set(configuration.username, forKey: "owu.username")
        defaults.set(certificateFingerprint, forKey: "owu.certificateSHA256")
        let normalizedPorts = configuration.additionalGatewayPorts.map(String.init).joined(separator: ", ")
        additionalGatewayPorts = normalizedPorts
        defaults.set(normalizedPorts, forKey: "owu.additionalGatewayPorts")
        do {
            try credentialStore.save(
                secret: configuration.browserPassword,
                kind: .browserPassword,
                serverHost: configuration.baseURL.host ?? "",
                username: configuration.username
            )
            try credentialStore.save(
                secret: configuration.tunnelKey,
                kind: .tunnelKey,
                serverHost: configuration.baseURL.host ?? "",
                username: configuration.username
            )
        } catch {
            notice = "The tunnel started, but its credentials could not be saved to Keychain."
        }
    }

    private func loadSavedCredentials() {
        guard let host = URL(string: serverAddress)?.host else { return }
        do {
            browserPassword = try credentialStore.load(
                kind: .browserPassword,
                serverHost: host,
                username: username
            ) ?? ""
            tunnelKey = try credentialStore.load(
                kind: .tunnelKey,
                serverHost: host,
                username: username
            ) ?? ""
        } catch {
            notice = "The saved credentials could not be read from Keychain."
        }
    }
}
#endif
