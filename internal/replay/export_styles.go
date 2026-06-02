package replay

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
