import XCTest
@testable import PermitCore

final class DestinationParserTests: XCTestCase {
    private let parser = DestinationParser()

    func testParsesHTTPSWithNonstandardPort() throws {
        let destination = try parser.parse(" https://EXAMPLE.com.:8443/dashboard ")
        XCTAssertEqual(
            destination,
            RequestedDestination(transport: .https, host: "example.com", port: 8443, path: "/dashboard")
        )
    }

    func testUsesHTTPSDefaultPort() throws {
        let destination = try parser.parse("https://example.com/")
        XCTAssertEqual(destination.port, 443)
    }

    func testParsesHostAndPortAsTCP() throws {
        let destination = try parser.parse("ssh.example.com:22")
        XCTAssertEqual(
            destination,
            RequestedDestination(transport: .tcp, host: "ssh.example.com", port: 22)
        )
    }

    func testParsesBracketedIPv6Authority() throws {
        let destination = try parser.parse("[2001:db8::10]:5432")
        XCTAssertEqual(destination.host, "2001:db8::10")
        XCTAssertEqual(destination.port, 5432)
    }

    func testRejectsURLCredentials() {
        XCTAssertThrowsError(try parser.parse("https://user:secret@example.com")) { error in
            XCTAssertEqual(error as? StableAccessError, .credentialsNotAllowed)
        }
    }

    func testRejectsUnsupportedScheme() {
        XCTAssertThrowsError(try parser.parse("file:///etc/passwd")) { error in
            XCTAssertEqual(error as? StableAccessError, .unsupportedScheme)
        }
    }

    func testRejectsEmptyAndOverflowPorts() {
        for value in ["https://example.com:/", "https://example.com:65536/", "example.com:0"] {
            XCTAssertThrowsError(try parser.parse(value), "Expected rejection for \(value)")
        }
    }

    func testRejectsAmbiguousBackslash() {
        XCTAssertThrowsError(try parser.parse("https://example.com\\@127.0.0.1"))
    }
}
