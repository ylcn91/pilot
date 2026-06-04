import SwiftUI

struct PilotCommands: Commands {
    @ObservedObject var store: AppStore

    var body: some Commands {
        CommandMenu("Pilot") {
            Button("Refresh") {
                Task { await store.refreshAll() }
            }
            .keyboardShortcut("r", modifiers: [.command])

            Button("Connect Agent Runtime") {
                store.connectRuntime()
            }
            .keyboardShortcut("k", modifiers: [.command, .shift])

            Divider()

            Button("Show Workspace") {
                store.selectedSection = .workspaceConsole
            }
            .keyboardShortcut("p", modifiers: [.command, .shift])

            Button("Show Settings") {
                store.selectedSection = .settings
            }
            .keyboardShortcut(",", modifiers: [.command])
        }
    }
}
