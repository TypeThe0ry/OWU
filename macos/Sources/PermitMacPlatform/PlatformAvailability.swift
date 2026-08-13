import Foundation
import PermitCore

public enum PermitMacPlatformAvailability {
    public static var summary: String {
#if os(macOS)
        "macOS platform adapters are available."
#else
        "macOS platform adapters require macOS. PermitCore remains portable."
#endif
    }
}
