package replay

import (
	"strings"
	"testing"
)

// TestWriteHTMLDocumentScaffold locks the exact bytes produced by the shared
// HTML document scaffold helpers for both exporter configurations. The
// expected strings are the literal opening/closing markup that ExportToHTML
// and ExportHTMLReport emitted before the scaffold was extracted, so this
// guards against any drift in the deduplicated boilerplate.
func TestWriteHTMLDocumentScaffold(t *testing.T) {
	t.Run("recording exporter open", func(t *testing.T) {
		var sb strings.Builder
		writeHTMLDocumentOpen(&sb, htmlDocument{
			htmlTag: "<html>",
			title:   "Execution Recording: REC-1",
			styles:  "BODY{}",
		})
		want := "<!DOCTYPE html>\n<html>\n<head>\n" +
			"<meta charset=\"UTF-8\">\n" +
			"<title>Execution Recording: REC-1</title>\n" +
			"<style>\n" +
			"BODY{}" +
			"</style>\n</head>\n<body>\n"
		if got := sb.String(); got != want {
			t.Errorf("recording open mismatch:\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("report exporter open", func(t *testing.T) {
		var sb strings.Builder
		writeHTMLDocumentOpen(&sb, htmlDocument{
			htmlTag:   "<html lang=\"en\">",
			headExtra: []string{"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">"},
			title:     "Execution Report: REC-1",
			styles:    "BODY{}",
		})
		want := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n" +
			"<meta charset=\"UTF-8\">\n" +
			"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n" +
			"<title>Execution Report: REC-1</title>\n" +
			"<style>\n" +
			"BODY{}" +
			"</style>\n</head>\n<body>\n"
		if got := sb.String(); got != want {
			t.Errorf("report open mismatch:\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("close", func(t *testing.T) {
		var sb strings.Builder
		writeHTMLDocumentClose(&sb)
		if got := sb.String(); got != "</body>\n</html>" {
			t.Errorf("close mismatch: %q", got)
		}
	})
}
