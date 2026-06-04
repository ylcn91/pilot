import SwiftUI

struct CommandCenterView: View {
    @EnvironmentObject private var store: AppStore
    @State private var selectedCommand = PilotCommandCatalog.all.first!
    @State private var arguments = PilotCommandCatalog.all.first!.defaultArguments
    @State private var prompt = ""

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Panel(title: "Pilot Commands", subtitle: "Full `pilot [command]` surface") {
                List(PilotCommandCatalog.all, selection: $selectedCommand) { command in
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(command.name)
                                .font(.system(.body, design: .monospaced))
                            if command.destructive {
                                Image(systemName: "exclamationmark.triangle.fill")
                                    .foregroundStyle(.orange)
                            }
                        }
                        Text(command.summary)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                    }
                    .tag(command)
                }
                .frame(width: 300)
                .frame(minHeight: 560)
                .onChange(of: selectedCommand) { _, newValue in
                    arguments = newValue.defaultArguments
                    prompt = ""
                }
            }

            VStack(alignment: .leading, spacing: 14) {
                Panel(title: selectedCommand.name, subtitle: selectedCommand.summary) {
                    VStack(alignment: .leading, spacing: 12) {
                        if selectedCommand.inputStyle != .none {
                            TextField("Arguments", text: $arguments)
                                .textFieldStyle(.roundedBorder)
                        }
                        if selectedCommand.inputStyle == .prompt {
                            TextField("Prompt or task description", text: $prompt, axis: .vertical)
                                .textFieldStyle(.roundedBorder)
                                .lineLimit(3...8)
                        }

                        HStack {
                            Button {
                                Task { await store.runCommand(selectedCommand, rawArguments: arguments, prompt: prompt) }
                            } label: {
                                Label(selectedCommand.destructive ? "Run Command" : "Run", systemImage: "play.fill")
                            }
                            .buttonStyle(.borderedProminent)

                            Button {
                                arguments = "--help"
                                Task { await store.runCommand(selectedCommand, rawArguments: "--help", prompt: "") }
                            } label: {
                                Label("Help", systemImage: "questionmark.circle")
                            }
                        }

                        Text("\(store.settings.pilotCommand) \(selectedCommand.name) \(arguments)")
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }

                Panel(title: "Output") {
                    if store.commandRuns.isEmpty {
                        ContentUnavailableView("No command output", systemImage: "terminal")
                            .frame(maxWidth: .infinity, minHeight: 360)
                    } else {
                        ScrollView {
                            LazyVStack(alignment: .leading, spacing: 12) {
                                ForEach(store.commandRuns) { run in
                                    VStack(alignment: .leading, spacing: 7) {
                                        HStack {
                                            Text(run.title)
                                                .font(.headline)
                                            Spacer()
                                            if let exit = run.exitCode {
                                                StatusBadge(text: exit == 0 ? "success" : "failed")
                                            } else {
                                                StatusBadge(text: "running")
                                            }
                                        }
                                        Text(run.commandLine)
                                            .font(.system(.caption, design: .monospaced))
                                            .foregroundStyle(.secondary)
                                            .textSelection(.enabled)
                                        Text(run.output.isEmpty ? "(no output)" : run.output)
                                            .font(.system(.caption, design: .monospaced))
                                            .textSelection(.enabled)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                    }
                                    .padding(10)
                                    .background(Color.secondary.opacity(0.08))
                                    .clipShape(RoundedRectangle(cornerRadius: 8))
                                }
                            }
                        }
                        .frame(minHeight: 380)
                    }
                }
            }
        }
    }
}
