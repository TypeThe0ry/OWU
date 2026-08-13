import XCTest
@testable import PermitCore

final class OWUTunnelTests: XCTestCase {
    func testBuildsPinnedWSSRequestWithoutCredentialsInURL() throws {
        let configuration = try OWUServerConfiguration(
            baseURL: XCTUnwrap(URL(string: "https://gateway.example.com")),
            username: "owu",
            password: "correct-horse-battery-staple",
            certificateSHA256: "AA:BB:CC"
        )
        let request = try configuration.tunnelRequest(resourceID: "minecraft")
        XCTAssertEqual(request.url?.absoluteString, "wss://gateway.example.com/tunnel/minecraft")
        XCTAssertNil(request.url?.user)
        XCTAssertNil(request.url?.password)
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-OWU-Tunnel-Key"), "correct-horse-battery-staple")
        XCTAssertTrue(request.value(forHTTPHeaderField: "Authorization")?.hasPrefix("Basic ") == true)
        XCTAssertEqual(configuration.certificateSHA256, "aabbcc")
    }

    func testRejectsHTTPCredentialsAndUnsafeResourceIDs() throws {
        XCTAssertThrowsError(
            try OWUServerConfiguration(
                baseURL: XCTUnwrap(URL(string: "http://gateway.example.com")),
                username: "owu",
                password: "long-enough-password-value"
            )
        )
        let configuration = try OWUServerConfiguration(
            baseURL: XCTUnwrap(URL(string: "https://gateway.example.com")),
            username: "owu",
            password: "long-enough-password-value"
        )
        for value in ["", "../ssh", "SSH", "ssh/other", String(repeating: "a", count: 65)] {
            XCTAssertThrowsError(try configuration.tunnelRequest(resourceID: value))
        }
    }

    func testDefaultPresetsUseLoopbackFriendlyPorts() {
        XCTAssertEqual(OWUTunnelPreset.ssh.localPort, 2222)
        XCTAssertEqual(OWUTunnelPreset.minecraft.localPort, 25565)
    }
}
