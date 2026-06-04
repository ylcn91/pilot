import SwiftUI

@main
struct Pilot91App: App {
    @StateObject private var store = AppStore()

    var body: some Scene {
        WindowGroup("", id: "conductor") {
            ConductorWindow()
                .environmentObject(store)
                .font(store.settings.interfaceFont.baseFont)
                .frame(minWidth: 1180, minHeight: 760)
                .preferredColorScheme(store.settings.appearance.colorScheme)
                .task {
                    await store.refreshAll()
                    store.connectRuntime()
                }
        }
        .commands {
            PilotCommands(store: store)
        }

        Settings {
            SettingsView()
                .environmentObject(store)
                .font(store.settings.interfaceFont.baseFont)
                .frame(width: 560, height: 420)
                .preferredColorScheme(store.settings.appearance.colorScheme)
        }
    }
}
