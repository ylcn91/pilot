import type { Config } from 'tailwindcss'
import { COLORS } from './src/components/ui/colors'

export default {
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Surfaces
        bg: COLORS.bg,
        card: COLORS.card,
        raised: COLORS.raised,
        border: COLORS.border,
        // Text
        fg: COLORS.fg,
        secondary: COLORS.secondary,
        muted: COLORS.muted,
        faint: COLORS.faint,
        // The one accent + semantic status
        accent: COLORS.accent,
        success: COLORS.success,
        danger: COLORS.danger,
        warning: COLORS.warning,
        // Non-text fill / track
        fill: COLORS.fill,
        // Legacy aliases (kept so existing panels keep compiling) — re-pointed
        // to the AA-verified palette. New code should prefer the tokens above.
        steel: COLORS.accent,
        sage: COLORS.success,
        rose: COLORS.danger,
        amber: COLORS.warning,
        slate: COLORS.fill,
        lightgray: COLORS.secondary,
        midgray: COLORS.muted,
        gray: COLORS.faint,
      },
      fontSize: {
        meta: ['12px', { lineHeight: '17px' }],
        sm: ['13px', { lineHeight: '19px' }],
        base: ['14px', { lineHeight: '21px' }],
        cardTitle: ['13px', { lineHeight: '18px' }],
        metric: ['22px', { lineHeight: '28px' }],
        heading: ['17px', { lineHeight: '24px' }],
      },
      fontFamily: {
        mono: ['SF Mono', 'Menlo', 'Monaco', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
} satisfies Config
