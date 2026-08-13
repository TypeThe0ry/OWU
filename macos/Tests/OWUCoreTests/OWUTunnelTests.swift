import XCTest
@testable import OWUCore

final class OWUTunnelTests: XCTestCase {
    func testBuildsPinnedWSSRequestWithoutCredentialsInURL() throws {
        let configuration = try OWUServerConfiguration(
            baseURL: XCTUnwrap(URL(string: "https://gateway.example.com")),
            username: "owu",
            browserPassword: "correct-horse-battery-staple",
            tunnelKey: "independent-tunnel-key-value",
            certificateSHA256: "AA:BB:CC"
        )
        let request = try configuration.tunnelRequest(resourceID: "minecraft")
        XCTAssertEqual(request.url?.absoluteString, "wss://gateway.example.com/tunnel/minecraft")
        XCTAssertNil(request.url?.user)
        XCTAssertNil(request.url?.password)
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-OWU-Tunnel-Key"), "independent-tunnel-key-value")
        let authorization = try XCTUnwrap(request.value(forHTTPHeaderField: "Authorization"))
        XCTAssertEqual(
            authorization,
            "Basic " + Data("owu:correct-horse-battery-staple".utf8).base64EncodedString()
        )
        XCTAssertEqual(configuration.certificateSHA256, "aabbcc")
    }

    func testRejectsHTTPCredentialsAndUnsafeResourceIDs() throws {
        XCTAssertThrowsError(
            try OWUServerConfiguration(
                baseURL: XCTUnwrap(URL(string: "http://gateway.example.com")),
                username: "owu",
                browserPassword: "long-enough-password-value",
                tunnelKey: "another-long-tunnel-key-value"
            )
        )
        let configuration = try OWUServerConfiguration(
            baseURL: XCTUnwrap(URL(string: "https://gateway.example.com")),
            username: "owu",
            browserPassword: "long-enough-password-value",
            tunnelKey: "another-long-tunnel-key-value"
        )
        for value in ["", "../ssh", "SSH", "ssh/other", String(repeating: "a", count: 65)] {
            XCTAssertThrowsError(try configuration.tunnelRequest(resourceID: value))
        }
    }

    func testRejectsSharedBrowserPasswordAndTunnelKey() throws {
        XCTAssertThrowsError(
            try OWUServerConfiguration(
                baseURL: XCTUnwrap(URL(string: "https://gateway.example.com")),
                username: "owu",
                browserPassword: "never-reuse-this-secret-value",
                tunnelKey: "never-reuse-this-secret-value"
            )
        ) { error in
            XCTAssertEqual(error as? OWUConfigurationError, .credentialsMustDiffer)
        }
    }

    func testDefaultPresetsUseLoopbackFriendlyPorts() {
        XCTAssertEqual(OWUTunnelPreset.ssh.localPort, 2222)
        XCTAssertEqual(OWUTunnelPreset.minecraft.localPort, 25565)
    }

    func testMinecraftPresetExposesTCPResourceURIAndConnectionState() {
        XCTAssertEqual(
            OWUTunnelPreset.minecraft.localResourceURL.absoluteString,
            "tcp://127.0.0.1:25565"
        )
        XCTAssertFalse(OWUTunnelState.stopped.isActive)
        XCTAssertFalse(OWUTunnelState.starting.isConnected)
        XCTAssertTrue(OWUTunnelState.starting.isActive)
        XCTAssertTrue(OWUTunnelState.ready.isActive)
        XCTAssertTrue(OWUTunnelState.ready.isConnected)
        XCTAssertEqual(OWUTunnelState.failed("connection reset").label, "Failed")
    }
}
