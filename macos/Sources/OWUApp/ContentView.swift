#if os(macOS)
import OWUCore
import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        ZStack {
            LinearGradient(
                colors: colorScheme == .dark
                    ? [Color(red: 0.04, green: 0.05, blue: 0.09), Color(red: 0.08, green: 0.11, blue: 0.18)]
                    : [Color(red: 0.92, green: 0.96, blue: 1), Color(red: 0.98, green: 0.95, blue: 1)],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
            .ignoresSafeArea()

            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    header
                    serverCard
                    resources
                    footer
                }
                .padding(32)
                .frame(maxWidth: 940)
            }
        }
        .alert("OWU", isPresented: Binding(
            get: { model.notice != nil },
            set: { if !$0 { model.notice = nil } }
        )) {
            Button("OK") { model.notice = nil }
        } message: {
            Text(model.notice ?? "")
        }
    }

    private var header: some View {
        HStack(spacing: 16) {
            Image(systemName: "network.badge.shield.half.filled")
                .font(.system(size: 34, weight: .semibold))
                .frame(width: 62, height: 62)
                .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                Text("Open Website Unblocker")
                    .font(.system(size: 28, weight: .bold, design: .rounded))
                Text("OWU for macOS")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Circle().fill(.green).frame(width: 8, height: 8)
            Text("Local listeners only").font(.caption).foregroundStyle(.secondary)
        }
    }

    private var serverCard: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Gateway").font(.headline)
            TextField("https://your-owu-server.example", text: $model.serverAddress)
                .textFieldStyle(.roundedBorder)
                .accessibilityLabel("OWU server address")
            HStack {
                TextField("Username", text: $model.username)
                    .textFieldStyle(.roundedBorder)
                SecureField("Browser password", text: $model.browserPassword)
                    .textFieldStyle(.roundedBorder)
            }
            SecureField("Independent tunnel key", text: $model.tunnelKey)
                .textFieldStyle(.roundedBorder)
            TextField("TLS certificate SHA-256 fingerprint (optional for public certificates)", text: $model.certificateFingerprint)
                .textFieldStyle(.roundedBorder)
                .font(.system(.body, design: .monospaced))
            TextField("Additional fallback ports, for example 8443, 9443", text: $model.additionalGatewayPorts)
                .textFieldStyle(.roundedBorder)
                .font(.system(.body, design: .monospaced))
                .accessibilityLabel("Additional OWU gateway fallback ports")
            HStack(spacing: 8) {
                gatewayPortBadge("443", primary: true)
                Image(systemName: "arrow.right")
                gatewayPortBadge("80")
                Image(systemName: "arrow.right")
                gatewayPortBadge("8080")
                Text("then extra ports")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text("Both credentials are stored separately in Keychain. The tunnel key must differ from the browser password. Every gateway attempt uses TLS-protected WSS; the server must expose the same OWU certificate and tunnel route on each enabled port.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .glassCard()
    }

    private var resources: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Local access").font(.title2.bold())
            ForEach(model.presets) { preset in
                resourceCard(preset)
            }
        }
    }

    private func resourceCard(_ preset: OWUTunnelPreset) -> some View {
        let state = model.state(for: preset)
        return HStack(spacing: 18) {
            Image(systemName: preset.symbol)
                .font(.system(size: 24, weight: .semibold))
                .frame(width: 48, height: 48)
                .background(Color.accentColor.opacity(0.12), in: RoundedRectangle(cornerRadius: 14))
            VStack(alignment: .leading, spacing: 5) {
                HStack {
                    Text(preset.name).font(.headline)
                    statusLabel(state)
                }
                Text(preset.localResourceURL.absoluteString)
                    .font(.system(.body, design: .monospaced))
                Text(preset.usage)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
            }
            Spacer()
            Button(buttonTitle(state)) { model.toggle(preset) }
                .buttonStyle(.borderedProminent)
                .tint(state.isActive ? .red : .accentColor)
                .controlSize(.large)
        }
        .glassCard()
    }

    private func statusLabel(_ state: OWUTunnelState) -> some View {
        Text(statusText(state))
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(statusColor(state).opacity(0.15), in: Capsule())
            .foregroundStyle(statusColor(state))
    }

    private var footer: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "info.circle")
            Text("Each local port maps to one server-configured resource ID. The Mac app never sends an arbitrary destination to the gateway.")
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }

    private func buttonTitle(_ state: OWUTunnelState) -> String { state.isActive ? "Stop" : "Start" }

    private func gatewayPortBadge(_ value: String, primary: Bool = false) -> some View {
        Text(value)
            .font(.caption.monospaced().weight(.semibold))
            .padding(.horizontal, 9)
            .padding(.vertical, 5)
            .background((primary ? Color.accentColor : Color.secondary).opacity(0.13), in: Capsule())
            .foregroundStyle(primary ? Color.accentColor : Color.secondary)
    }

    private func statusText(_ state: OWUTunnelState) -> String { state.label }
    private func statusColor(_ state: OWUTunnelState) -> Color {
        switch state {
        case .ready: return .green
        case .starting: return .orange
        case .failed: return .red
        case .stopped: return .secondary
        }
    }
}

private extension View {
    func glassCard() -> some View {
        padding(20)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 24, style: .continuous).stroke(.white.opacity(0.16)))
            .shadow(color: .black.opacity(0.08), radius: 18, y: 8)
    }
}
#endif
