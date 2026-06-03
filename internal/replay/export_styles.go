package replay

import "strings"

// htmlDocument holds the per-exporter pieces that vary between the two
// standalone HTML exporters (ExportToHTML and ExportHTMLReport). The
// surrounding document scaffold (<!DOCTYPE>, <head>, <style>, <body>, and
// the closing tags) is identical and rendered by writeHTMLDocumentOpen /
// writeHTMLDocumentClose.
type htmlDocument struct {
	// htmlTag is the opening <html ...> element, e.g. "<html>" or
	// "<html lang=\"en\">".
	htmlTag string
	// headExtra holds additional <head> lines emitted after the charset
	// meta and before the <title> (e.g. the viewport meta). Each entry is
	// written verbatim followed by a newline.
	headExtra []string
	// title is the full text placed inside the <title> element.
	title string
	// styles is the CSS placed inside the <style> block.
	styles string
}

// writeHTMLDocumentOpen writes the shared opening scaffold (doctype, head,
// style block, body open) for a standalone HTML export.
func writeHTMLDocumentOpen(sb *strings.Builder, doc htmlDocument) {
	sb.WriteString("<!DOCTYPE html>\n")
	sb.WriteString(doc.htmlTag)
	sb.WriteString("\n<head>\n")
	sb.WriteString("<meta charset=\"UTF-8\">\n")
	for _, line := range doc.headExtra {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("<title>")
	sb.WriteString(doc.title)
	sb.WriteString("</title>\n")
	sb.WriteString("<style>\n")
	sb.WriteString(doc.styles)
	sb.WriteString("</style>\n</head>\n<body>\n")
}

// writeHTMLDocumentClose writes the shared closing scaffold for a
// standalone HTML export.
func writeHTMLDocumentClose(sb *strings.Builder) {
	sb.WriteString("</body>\n</html>")
}

// recordingExportStyles returns the CSS used by the compact recording
// exporter (ExportToHTML).
func recordingExportStyles() string {
	return `
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 40px; background: #1a1a2e; color: #eee; }
.header { background: #16213e; padding: 20px; border-radius: 8px; margin-bottom: 20px; }
.header h1 { margin: 0 0 10px 0; color: #0f4c75; }
.meta { display: flex; gap: 20px; flex-wrap: wrap; }
.meta-item { background: #0f3460; padding: 8px 16px; border-radius: 4px; }
.meta-label { color: #888; font-size: 12px; }
.meta-value { font-weight: bold; }
.event { padding: 12px 16px; border-left: 3px solid #333; margin: 8px 0; background: #16213e; border-radius: 0 4px 4px 0; }
.event:hover { background: #1a1a3e; }
.event-tool { border-left-color: #4a9eff; }
.event-text { border-left-color: #50c878; }
.event-result { border-left-color: #ffd700; }
.event-error { border-left-color: #ff4444; background: #2a1a1a; }
.timestamp { color: #666; font-size: 12px; margin-right: 10px; }
.sequence { color: #888; font-size: 11px; }
.tool-name { color: #4a9eff; font-weight: bold; }
.tool-detail { color: #aaa; margin-left: 8px; }
pre { background: #0a0a1a; padding: 12px; border-radius: 4px; overflow-x: auto; font-size: 13px; }
.section { margin: 24px 0; }
.section h2 { color: #0f4c75; border-bottom: 1px solid #333; padding-bottom: 8px; }
`
}

func htmlReportStyles() string {
	return `
:root {
  --bg-primary: #0d1117;
  --bg-secondary: #161b22;
  --bg-tertiary: #21262d;
  --text-primary: #e6edf3;
  --text-secondary: #8b949e;
  --text-muted: #6e7681;
  --border-color: #30363d;
  --accent-blue: #58a6ff;
  --accent-green: #3fb950;
  --accent-yellow: #d29922;
  --accent-red: #f85149;
  --accent-purple: #a371f7;
}

* { box-sizing: border-box; }

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  margin: 0;
  padding: 20px;
  line-height: 1.5;
}

.container {
  max-width: 1200px;
  margin: 0 auto;
}

.header {
  background: linear-gradient(135deg, var(--bg-secondary) 0%, var(--bg-tertiary) 100%);
  padding: 32px;
  border-radius: 12px;
  margin-bottom: 24px;
  border: 1px solid var(--border-color);
}

.header h1 {
  margin: 0 0 8px 0;
  font-size: 28px;
  color: var(--text-primary);
}

.header .subtitle {
  color: var(--text-secondary);
  font-size: 16px;
}

.status-badge {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 16px;
  font-size: 13px;
  font-weight: 500;
  margin-left: 12px;
}

.status-completed { background: rgba(63, 185, 80, 0.2); color: var(--accent-green); }
.status-failed { background: rgba(248, 81, 73, 0.2); color: var(--accent-red); }
.status-cancelled { background: rgba(210, 153, 34, 0.2); color: var(--accent-yellow); }

.summary-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.summary-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 20px;
}

.summary-card .label {
  color: var(--text-secondary);
  font-size: 13px;
  margin-bottom: 4px;
}

.summary-card .value {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
}

.summary-card .subvalue {
  color: var(--text-muted);
  font-size: 13px;
}

.section {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 24px;
  margin-bottom: 24px;
}

.section h2 {
  margin: 0 0 20px 0;
  font-size: 18px;
  color: var(--text-primary);
  display: flex;
  align-items: center;
  gap: 8px;
}

.section h2 .icon { font-size: 20px; }

.chart-bar {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
}

.chart-label {
  width: 120px;
  font-size: 14px;
  color: var(--text-secondary);
}

.chart-bar-container {
  flex: 1;
  height: 24px;
  background: var(--bg-tertiary);
  border-radius: 4px;
  overflow: hidden;
  margin: 0 12px;
}

.chart-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.3s ease;
}

.chart-value {
  width: 80px;
  text-align: right;
  font-size: 13px;
  color: var(--text-muted);
}

.phase-research { background: var(--accent-blue); }
.phase-implementing { background: var(--accent-green); }
.phase-verifying { background: var(--accent-yellow); }
.phase-completing { background: var(--accent-purple); }
.phase-init { background: var(--text-muted); }

.tool-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.tool-card {
  background: var(--bg-tertiary);
  border-radius: 6px;
  padding: 16px;
}

.tool-name {
  font-weight: 500;
  margin-bottom: 4px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.tool-stats {
  font-size: 13px;
  color: var(--text-secondary);
}

.error-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.error-item {
  background: rgba(248, 81, 73, 0.1);
  border: 1px solid rgba(248, 81, 73, 0.3);
  border-radius: 6px;
  padding: 16px;
}

.error-meta {
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 8px;
}

.error-message {
  color: var(--accent-red);
  font-family: 'SFMono-Regular', Consolas, monospace;
  font-size: 13px;
}

.timeline {
  max-height: 600px;
  overflow-y: auto;
  border: 1px solid var(--border-color);
  border-radius: 6px;
}

.timeline-event {
  padding: 12px 16px;
  border-bottom: 1px solid var(--border-color);
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.timeline-event:last-child { border-bottom: none; }

.timeline-event:hover {
  background: var(--bg-tertiary);
}

.timeline-time {
  font-size: 12px;
  color: var(--text-muted);
  font-family: monospace;
  white-space: nowrap;
}

.timeline-seq {
  font-size: 11px;
  color: var(--text-muted);
  width: 40px;
}

.timeline-icon { font-size: 16px; }

.timeline-content {
  flex: 1;
  font-size: 14px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
}

.timeline-event.tool .timeline-content { color: var(--accent-blue); }
.timeline-event.text .timeline-content { color: var(--text-primary); }
.timeline-event.error .timeline-content { color: var(--accent-red); }
.timeline-event.result .timeline-content { color: var(--accent-green); }

.footer {
  text-align: center;
  padding: 24px;
  color: var(--text-muted);
  font-size: 13px;
}
`
}
