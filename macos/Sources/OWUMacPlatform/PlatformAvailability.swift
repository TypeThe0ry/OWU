import Foundation
import OWUCore

public enum OWUMacPlatformAvailability {
    public static var summary: String {
#if os(macOS)
        "macOS platform adapters are available."
#else
        "macOS platform adapters require macOS. OWUCore remains portable."
#endif
    }
}
