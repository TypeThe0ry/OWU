#if os(macOS)
import SwiftUI

@main
struct PermitAccessClientApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
                .frame(minWidth: 760, minHeight: 520)
        }
        .windowResizability(.contentMinSize)
    }
}
#else
import Foundation

@main
enum PermitAccessClientUnsupportedHost {
    static func main() {
        print("PermitAccessClient requires macOS 14 or later. Run swift test to verify PermitCore on this host.")
    }
}
#endif
