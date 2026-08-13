#if os(macOS)
import Combine
import Foundation
import PermitCore
import PermitMacPlatform

@MainActor
final class AppModel: ObservableObject {
    @Published var serverAddress: String
    @Published var username: String
    @Published var password = ""
    @Published var certificateFingerprint: String
    @Published private(set) var states: [String: OWUTunnelState] = [:]
    @Published var notice: String?

    let presets = OWUTunnelPreset.defaults
    private let defaults = UserDefaults.standard
    private let credentialStore = OWUCredentialStore()
    private var tunnels: [String: WebSocketLoopbackTunnel] = [:]

    init() {
        serverAddress = defaults.string(forKey: "owu.server") ?? "https://8.219.11.175"
        username = defaults.string(forKey: "owu.username") ?? "owu"
        certificateFingerprint = defaults.string(forKey: "owu.certificateSHA256")
            ?? "A4:03:FF:0C:22:E8:E5:03:18:97:D1:53:6E:B7:B8:C0:68:BB:16:15:2E:C6:8B:BF:C4:45:7A:2C:76:A6:EC:E2"
        for preset in presets { states[preset.id] = .stopped }
        loadSavedPassword()
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
        return try OWUServerConfiguration(
            baseURL: url,
            username: username,
            password: password,
            certificateSHA256: certificateFingerprint
        )
    }

    private func save(_ configuration: OWUServerConfiguration) {
        defaults.set(configuration.baseURL.absoluteString, forKey: "owu.server")
        defaults.set(configuration.username, forKey: "owu.username")
        defaults.set(certificateFingerprint, forKey: "owu.certificateSHA256")
        do {
            try credentialStore.save(
                password: configuration.password,
                serverHost: configuration.baseURL.host ?? "",
                username: configuration.username
            )
        } catch {
            notice = "The tunnel started, but the password could not be saved to Keychain."
        }
    }

    private func loadSavedPassword() {
        guard let host = URL(string: serverAddress)?.host else { return }
        do { password = try credentialStore.load(serverHost: host, username: username) ?? "" }
        catch { notice = "The saved password could not be read from Keychain." }
    }
}
#endif
