#if os(macOS)
import AppKit
import PermitCore
import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        NavigationSplitView {
            List(AppModel.Section.allCases, selection: $model.selectedSection) { section in
                Label(section.rawValue, systemImage: section.systemImage)
                    .tag(section)
            }
            .navigationTitle("Permit")
        } detail: {
            Group {
                switch model.selectedSection ?? .home {
                case .home: HomeView()
                case .proxy: ProxyView()
                case .resources: ResourcesView()
                case .device: DeviceView()
                case .settings: SettingsView()
                }
            }
            .padding(28)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
        .alert("Permit", isPresented: noticeBinding) {
            Button("OK", action: model.dismissNotice)
        } message: {
            Text(model.notice ?? "")
        }
    }

    private var noticeBinding: Binding<Bool> {
        Binding(
            get: { model.notice != nil },
            set: { if !$0 { model.dismissNotice() } }
        )
    }
}

private struct HomeView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text("Access Client")
                        .font(.largeTitle.bold())
                    Text("Open public resources that have been pre-registered with Permit.")
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Label("Catalog offline", systemImage: "circle.fill")
                    .foregroundStyle(.secondary)
                    .accessibilityLabel("Status: Public resource catalog offline")
            }

            GroupBox("Access an approved resource") {
                VStack(alignment: .leading, spacing: 12) {
                    TextField("Enter a URL or host:port", text: $model.destinationInput)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityLabel("Authorized resource URL or host and port")
                        .onSubmit(model.continueToResource)
                    HStack {
                        Text("No account is required. Unknown destinations are denied.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Spacer()
                        Button("Continue", action: model.continueToResource)
                            .keyboardShortcut(.defaultAction)
                    }
                }
                .padding(.top, 8)
            }

            GroupBox("Before you continue") {
                VStack(alignment: .leading, spacing: 8) {
                    Label("Connections are encrypted to the access gateway.", systemImage: "lock")
                    Label("Only pre-registered public resources can receive a route grant.", systemImage: "checkmark.shield")
                    Label("Connection metadata may be recorded for security.", systemImage: "list.bullet.clipboard")
                    Label("Passwords and request bodies are not recorded.", systemImage: "eye.slash")
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 8)
            }

            Button("Refresh public resources", action: model.refreshResources)
                .controlSize(.large)
        }
    }
}

private struct ProxyView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 22) {
            Text("Local Proxy").font(.largeTitle.bold())
            Text("Configure an app to use a loopback proxy for a pre-registered public resource. Every connection still requires a server-issued route grant.")
                .foregroundStyle(.secondary)
            addressRow(name: "SOCKS5", address: model.socksAddress)
            addressRow(name: "HTTP CONNECT", address: model.connectAddress)
            Label("Stopped", systemImage: "stop.circle")
                .accessibilityLabel("Local proxy status: Stopped")
            Button("Start local proxy", action: model.startProxy)
                .disabled(true)
            Text("Disabled in this scaffold until rotating proxy authentication, protocol parsing, and the gateway transport are integrated.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private func addressRow(name: String, address: String) -> some View {
        HStack {
            Text(name).frame(width: 120, alignment: .leading)
            Text(address).font(.system(.body, design: .monospaced))
            Spacer()
            Button("Copy") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(address, forType: .string)
            }
            .accessibilityLabel("Copy \(name) address")
        }
    }
}

private struct ResourcesView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("Resources").font(.largeTitle.bold())
            if model.resources.isEmpty {
                ContentUnavailableView(
                    "No approved resources",
                    systemImage: "server.rack",
                    description: Text("Connect the catalog client to see public services registered with Permit.")
                )
            }
        }
    }
}

private struct DeviceView: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("Local Security").font(.largeTitle.bold())
            LabeledContent("Installation key", value: "Created when the gateway is configured")
            LabeledContent("Private material", value: "Keychain / Secure Enclave when available")
            LabeledContent("Policy sync", value: "Not yet synced")
            Text("No account is required. The installation key can bind short-lived grants to this Mac without identifying a user.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

private struct SettingsView: View {
    @State private var launchAtLogin = false

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("Settings").font(.largeTitle.bold())
            Toggle("Launch Permit at login", isOn: $launchAtLogin)
                .disabled(true)
            GroupBox("System tunnel") {
                VStack(alignment: .leading, spacing: 8) {
                    Label("Not available in this build", systemImage: "network.slash")
                    Text("Network Extension support requires approved entitlements, provisioning, signing, and testing on physical Macs. Only approved resource routes will be eligible for split tunneling.")
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 8)
            }
        }
    }
}
#endif
