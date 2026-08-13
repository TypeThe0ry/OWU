#if os(macOS)
import SwiftUI

@main
struct OWUMacClientApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 760, minHeight: 640)
        }
        .windowResizability(.contentMinSize)
    }
}
#else
import Foundation

@main
enum OWUMacClientUnsupportedHost {
    static func main() {
        print("OWU requires macOS 14 or later. Run swift test to verify PermitCore on this host.")
    }
}
#endif
