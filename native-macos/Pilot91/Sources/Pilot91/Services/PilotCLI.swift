import Foundation

struct PilotCLI {
    var pilotCommand: String
    var workingDirectory: String

    func run(arguments: [String], onOutput: ((String) -> Void)? = nil) async -> CommandRun {
        let commandLine = ([pilotCommand] + arguments.map(shellQuote)).joined(separator: " ")
        return await ShellCommand(workingDirectory: workingDirectory).run(commandLine, title: arguments.first ?? "pilot", onOutput: onOutput)
    }

    func runRaw(_ rawArguments: String, title: String, onOutput: ((String) -> Void)? = nil) async -> CommandRun {
        await ShellCommand(workingDirectory: workingDirectory).run("\(pilotCommand) \(rawArguments)", title: title, onOutput: onOutput)
    }
}

struct ShellCommand {
    var workingDirectory: String

    func run(
        _ shellCommand: String,
        title: String,
        input: String? = nil,
        includeStderr: Bool = true,
        onOutput: ((String) -> Void)? = nil
    ) async -> CommandRun {
        await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                let process = Process()
                let startedAt = Date()
                process.executableURL = URL(fileURLWithPath: "/bin/zsh")
                process.arguments = ["-lc", shellCommand]
                if !workingDirectory.isEmpty {
                    process.currentDirectoryURL = URL(fileURLWithPath: workingDirectory)
                }

                let outputPipe = Pipe()
                process.standardOutput = outputPipe
                let stderrPipe = includeStderr ? nil : Pipe()
                process.standardError = includeStderr ? outputPipe : stderrPipe
                let outputQueue = DispatchQueue(label: "pilot91.shell.output")
                var outputData = Data()
                outputPipe.fileHandleForReading.readabilityHandler = { handle in
                    let data = handle.availableData
                    guard !data.isEmpty else { return }
                    var snapshot = Data()
                    outputQueue.sync {
                        outputData.append(data)
                        snapshot = outputData
                    }
                    if let onOutput, let text = String(data: snapshot, encoding: .utf8) {
                        DispatchQueue.main.async {
                            onOutput(text)
                        }
                    }
                }
                let stderrQueue = DispatchQueue(label: "pilot91.shell.stderr")
                var stderrData = Data()
                stderrPipe?.fileHandleForReading.readabilityHandler = { handle in
                    let data = handle.availableData
                    guard !data.isEmpty else { return }
                    stderrQueue.sync {
                        stderrData.append(data)
                    }
                }
                if input != nil {
                    process.standardInput = Pipe()
                }

                do {
                    try process.run()
                    if let input, let inputPipe = process.standardInput as? Pipe {
                        inputPipe.fileHandleForWriting.write(Data(input.utf8))
                        try? inputPipe.fileHandleForWriting.close()
                    }
                    process.waitUntilExit()
                    outputPipe.fileHandleForReading.readabilityHandler = nil
                    stderrPipe?.fileHandleForReading.readabilityHandler = nil
                    try? outputPipe.fileHandleForReading.close()
                    try? stderrPipe?.fileHandleForReading.close()
                    let data = outputQueue.sync { outputData }
                    let errorData = stderrQueue.sync { stderrData }
                    let output = String(data: data, encoding: .utf8) ?? ""
                    let stderr = String(data: errorData, encoding: .utf8) ?? ""
                    continuation.resume(returning: CommandRun(
                        title: title,
                        commandLine: shellCommand,
                        output: output,
                        stderr: stderr,
                        exitCode: process.terminationStatus,
                        startedAt: startedAt,
                        finishedAt: Date()
                    ))
                } catch {
                    outputPipe.fileHandleForReading.readabilityHandler = nil
                    stderrPipe?.fileHandleForReading.readabilityHandler = nil
                    continuation.resume(returning: CommandRun(
                        title: title,
                        commandLine: shellCommand,
                        output: error.localizedDescription,
                        exitCode: -1,
                        startedAt: startedAt,
                        finishedAt: Date()
                    ))
                }
            }
        }
    }
}

func shellQuote(_ value: String) -> String {
    if value.isEmpty {
        return "''"
    }
    let escaped = value.replacingOccurrences(of: "'", with: "'\\''")
    return "'\(escaped)'"
}
