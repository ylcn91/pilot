import XCTest
@testable import Pilot91

final class CreatePRCommandTests: XCTestCase {
    func testFallsBackToWebWhenNoInput() {
        XCTAssertEqual(buildCreatePRCommand(title: "", body: ""), "gh pr create --web")
    }

    func testWhitespaceOnlyInputFallsBackToWeb() {
        XCTAssertEqual(buildCreatePRCommand(title: "  ", body: "\n\t"), "gh pr create --web")
    }

    func testTitleAndBodyAreForwarded() {
        let command = buildCreatePRCommand(title: "Add feature", body: "Implements X")
        XCTAssertEqual(command, "gh pr create --title 'Add feature' --body 'Implements X'")
    }

    func testTitleOnlyStillSendsEmptyBody() {
        let command = buildCreatePRCommand(title: "Quick fix", body: "")
        XCTAssertEqual(command, "gh pr create --title 'Quick fix' --body ''")
    }

    func testBodyOnlyDerivesTitleFromFirstLine() {
        let command = buildCreatePRCommand(title: "", body: "Headline\nMore detail")
        XCTAssertEqual(command, "gh pr create --title 'Headline' --body 'Headline\nMore detail'")
    }

    func testInputIsShellQuotedAgainstInjection() {
        let command = buildCreatePRCommand(title: "it's a 'PR'", body: "rm -rf /; echo pwned")
        XCTAssertEqual(
            command,
            "gh pr create --title 'it'\\''s a '\\''PR'\\''' --body 'rm -rf /; echo pwned'"
        )
    }

    func testInputIsTrimmedBeforeQuoting() {
        let command = buildCreatePRCommand(title: "  Trim me  ", body: "  body  ")
        XCTAssertEqual(command, "gh pr create --title 'Trim me' --body 'body'")
    }
}
