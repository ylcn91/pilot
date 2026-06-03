// Single source of truth for raw hex values shared by SVG/canvas code
// (Sparkline, GitGraph) and the Tailwind config. UI built with Tailwind
// classes should use the named tokens (accent, success, ...) instead of
// reaching for these literals.

export const COLORS = {
  bg: '#0d1117',
  card: '#161b22',
  raised: '#1f242d',
  border: '#2b3038',
  fg: '#e6edf3',
  secondary: '#c9d1d9',
  muted: '#9aa5b1',
  faint: '#7d8590',
  accent: '#6cb6ff',
  success: '#6fcf97',
  danger: '#f0879a',
  warning: '#e3b341',
  fill: '#3d4450',
} as const

export type ColorToken = keyof typeof COLORS
