#if os(macOS)
import Combine
import Foundation
import PermitCore

@MainActor
final class AppModel: ObservableObject {
    enum Section: String, CaseIterable, Identifiable {
        case home = "Home"
        case proxy = "Proxy"
        case resources = "Resources"
        case device = "Local Security"
        case settings = "Settings"

        var id: String { rawValue }

        var systemImage: String {
            switch self {
            case .home: return "house"
            case .proxy: return "point.3.connected.trianglepath.dotted"
            case .resources: return "server.rack"
            case .device: return "laptopcomputer"
            case .settings: return "gearshape"
            }
        }
    }

    @Published var selectedSection: Section? = .home
    @Published var destinationInput = ""
    @Published var resources: [AuthorizedResource] = []
    @Published var proxyState: LocalProxyState = .stopped
    @Published var notice: String?

    let socksAddress = "127.0.0.1:1080"
    let connectAddress = "127.0.0.1:8080"

    func refreshResources() {
        notice = "The public resource catalog client is not configured in this scaffold."
    }

    func continueToResource() {
        do {
            _ = try DestinationParser().parse(destinationInput)
            notice = "Public catalog lookup and route-grant resolution are not configured in this scaffold."
        } catch {
            notice = error.localizedDescription
        }
    }

    func startProxy() {
        notice = "The listener remains disabled until authenticated parsers and the gateway transport are connected."
    }

    func dismissNotice() {
        notice = nil
    }
}
#endif
