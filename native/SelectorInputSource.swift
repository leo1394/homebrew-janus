import AppKit
import Carbon
import Darwin

// No application activation, key interception, credentials, or persistent settings.
func runSelectorInputSource() -> Never {
    func sourceID(_ source: TISInputSource) -> String? {
        guard let value = TISGetInputSourceProperty(source, kTISPropertyInputSourceID) else { return nil }
        return Unmanaged<CFString>.fromOpaque(value).takeUnretainedValue() as String
    }
    func windowID(_ pid: pid_t) -> Int? {
        let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
        return windows.first { ($0[kCGWindowOwnerPID as String] as? Int) == Int(pid) && ($0[kCGWindowLayer as String] as? Int) == 0 }?[kCGWindowNumber as String] as? Int
    }
    guard ProcessInfo.processInfo.environment["SSH_CONNECTION"] == nil,
          let foreground = NSWorkspace.shared.frontmostApplication else { exit(0) }
    // Only a foreground app in this process's ancestry may have its input source changed.
    var pid = getppid()
    var ownsForeground = false
    for _ in 0..<64 {
        if pid == foreground.processIdentifier { ownsForeground = true; break }
        if pid <= 1 { break }
        var info = kinfo_proc()
        var size = MemoryLayout<kinfo_proc>.size
        var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, pid]
        guard sysctl(&mib, 4, &info, &size, nil, 0) == 0, size > 0 else { break }
        pid = info.kp_eproc.e_ppid
    }
    guard ownsForeground, let originalWindow = windowID(foreground.processIdentifier) else { exit(0) }
    let original = TISCopyCurrentKeyboardInputSource().takeRetainedValue()
    let english = TISCopyCurrentASCIICapableKeyboardLayoutInputSource().takeRetainedValue()
    guard let originalID = sourceID(original), let englishID = sourceID(english), originalID != englishID else { exit(0) }
    // Losing focus suspends ownership; it does not mean the user changed this
    // terminal's input source. Restore only when the original window is frontmost.
    var relinquished = false
    func stillOwnsContext() -> Bool {
        NSWorkspace.shared.frontmostApplication?.processIdentifier == foreground.processIdentifier && windowID(foreground.processIdentifier) == originalWindow
    }
    guard stillOwnsContext(), TISSelectInputSource(english) == noErr else { exit(0) }
    let observer = DistributedNotificationCenter.default().addObserver(forName: Notification.Name(kTISNotifySelectedKeyboardInputSourceChanged as String), object: nil, queue: .main) { _ in
        if stillOwnsContext() && sourceID(TISCopyCurrentKeyboardInputSource().takeRetainedValue()) != englishID { relinquished = true }
    }
    let timer = Timer.scheduledTimer(withTimeInterval: 0.05, repeats: true) { _ in
        if stillOwnsContext() && sourceID(TISCopyCurrentKeyboardInputSource().takeRetainedValue()) != englishID { relinquished = true }
    }
    let input = DispatchSource.makeReadSource(fileDescriptor: STDIN_FILENO, queue: .main)
    input.setEventHandler {
        var byte: UInt8 = 0
        _ = read(STDIN_FILENO, &byte, 1)
        timer.invalidate()
        DistributedNotificationCenter.default().removeObserver(observer)
        if !relinquished && stillOwnsContext() && sourceID(TISCopyCurrentKeyboardInputSource().takeRetainedValue()) == englishID {
            _ = TISSelectInputSource(original)
        }
        exit(0)
    }
    input.resume()
    print("ready")
    fflush(stdout)
    RunLoop.main.run()
    exit(0)
}
