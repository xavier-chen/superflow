import SwiftUI
import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}

@main
struct SoundFlowApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var model = AppModel()
    var body: some Scene {
        WindowGroup("SoundFlow", id: "main") {
            Dashboard(model: model)
                .preferredColorScheme(.light)
        }
        .defaultSize(width: 900, height: 770)
        .windowResizability(.contentMinSize)
        .commands {
            CommandGroup(replacing: .newItem) {}
        }
        MenuBarExtra("SoundFlow", systemImage: model.receiving ? "waveform.circle.fill" : "waveform.circle") {
            MenuContent(model: model)
        }
        .menuBarExtraStyle(.menu)
    }
}

private struct MenuContent: View {
    @ObservedObject var model: AppModel
    @Environment(\.openWindow) var openWindow
    var body: some View {
        Text(model.receiving ? "正在汇集 · \(model.onlineCount) 台设备" : "声音接收已暂停")
        Text("输出：\(model.outputName)")
        Divider()
        Button("打开 SoundFlow") { openWindow(id: "main"); NSApp.activate(ignoringOtherApps: true) }
        Button(model.receiving ? "暂停接收" : "开始接收") { model.toggle() }
        Button("复制连接密钥") { model.copyCode() }
        Divider()
        Button("退出 SoundFlow") { model.stop(); NSApp.terminate(nil) }.keyboardShortcut("q")
    }
}
