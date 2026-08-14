import Foundation
import XCTest
@testable import OWUCore

final class OWUGatewayFailoverTests: XCTestCase {
    private let browserPassword = "correct-horse-battery-staple"
    private let tunnelKey = "independent-tunnel-key-value"

    func testStandardPortsAreOrderedAndAlwaysUseTLS() throws {
        let plan = try OWUGatewayPortPlan()

        XCTAssertEqual(OWUGatewayPortPlan.standardPorts, [443, 80, 8080])
        XCTAssertEqual(OWUGatewayPortPlan.recommendedAdditionalPorts, [8443, 9443])
        XCTAssertEqual(plan.endpoints.map(\.port), [443, 80, 8080])
        XCTAssertEqual(plan.endpoints.map(\.scheme), ["wss", "wss", "wss"])
    }

    func testAdditionalPortsAreStableDeduplicatedFallbacks() throws {
        let plan = try OWUGatewayPortPlan(
            additionalPorts: [8443, 8080, 9443, 8443, 443]
        )

        XCTAssertEqual(plan.additionalPorts, [8443, 9443])
        XCTAssertEqual(plan.endpoints.map(\.port), [443, 80, 8080, 8443, 9443])
        XCTAssertTrue(plan.endpoints.allSatisfy { $0.scheme == "wss" })
    }

    func testParsesAdditionalPortsAndRejectsInvalidInput() throws {
        XCTAssertEqual(
            try OWUGatewayPortPlan.parseAdditionalPorts("8443, 9443;10443  11443"),
            [8443, 9443, 10443, 11443]
        )
        for invalid in ["0", "65536", "not-a-port", "8443,tcp"] {
            XCTAssertThrowsError(try OWUGatewayPortPlan.parseAdditionalPorts(invalid)) { error in
                XCTAssertEqual(error as? OWUConfigurationError, .invalidGatewayPort)
            }
        }
    }

    func testTunnelCandidatesKeepSecretsOutOfURLsAcrossRetries() throws {
        let configuration = try OWUServerConfiguration(
            baseURL: XCTUnwrap(URL(string: "https://gateway.example.com")),
            username: "owu",
            browserPassword: browserPassword,
            tunnelKey: tunnelKey,
            additionalGatewayPorts: [8443]
        )

        let requests = try configuration.tunnelRequests(resourceID: "minecraft")
        XCTAssertEqual(
            requests.compactMap(\.url?.absoluteString),
            [
                "wss://gateway.example.com/tunnel/minecraft",
                "wss://gateway.example.com:80/tunnel/minecraft",
                "wss://gateway.example.com:8080/tunnel/minecraft",
                "wss://gateway.example.com:8443/tunnel/minecraft",
            ]
        )

        let expectedBasic = "Basic " + Data("owu:\(browserPassword)".utf8).base64EncodedString()
        for request in requests {
            let url = try XCTUnwrap(request.url)
            let visibleURL = url.absoluteString
            XCTAssertEqual(url.scheme, "wss")
            XCTAssertNil(url.user)
            XCTAssertNil(url.password)
            XCTAssertNil(url.query)
            XCTAssertNil(url.fragment)
            XCTAssertFalse(visibleURL.contains(browserPassword))
            XCTAssertFalse(visibleURL.contains(tunnelKey))
            XCTAssertFalse(visibleURL.contains(expectedBasic))
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), expectedBasic)
            XCTAssertEqual(request.value(forHTTPHeaderField: "X-OWU-Tunnel-Key"), tunnelKey)
            XCTAssertEqual(request.value(forHTTPHeaderField: "Cache-Control"), "no-store")
        }

        XCTAssertEqual(try configuration.tunnelRequest(resourceID: "minecraft"), requests[0])
    }

    func testExplicitCustomBasePortRemainsASecondaryFallback() throws {
        let configuration = try OWUServerConfiguration(
            baseURL: XCTUnwrap(URL(string: "https://gateway.example.com:9443/base")),
            username: "owu",
            browserPassword: browserPassword,
            tunnelKey: tunnelKey,
            additionalGatewayPorts: [8443, 9443]
        )

        XCTAssertEqual(
            configuration.gatewayEndpoints.map(\.port),
            [443, 80, 8080, 9443, 8443]
        )
        XCTAssertEqual(configuration.additionalGatewayPorts, [9443, 8443])
        XCTAssertTrue(
            try configuration.tunnelRequests(resourceID: "ssh")
                .allSatisfy { $0.url?.scheme == "wss" }
        )
    }
}
