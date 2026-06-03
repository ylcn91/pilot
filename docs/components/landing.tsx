import type { CSSProperties, ReactNode } from 'react'

const BRAND_FILL = '#6BADD3'
const BRAND_FILL_HOVER = '#7fbcdd'
const BRAND_FILL_ACTIVE = '#5a9cc2'
const CTA_FG = '#0b0f14'

export function Hero() {
  return (
    <header className="pilot-hero">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src="/logo.svg"
        alt="Pilot"
        width={220}
        height={67}
        className="pilot-hero-logo"
      />
      <h1 className="pilot-hero-title">Pilot — AI That Ships Your Tickets</h1>
      <p className="pilot-hero-subtitle">
        Autonomous AI development pipeline. Label a ticket, get a PR — planned
        with your codebase context, written by Claude Code, gated by your
        quality checks.
      </p>
      <CTARow />
    </header>
  )
}

export function CTARow() {
  const base: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: 44,
    padding: '0 20px',
    borderRadius: 6,
    fontWeight: 500,
    fontSize: 15,
    textDecoration: 'none',
    lineHeight: 1,
    transition: 'background-color 150ms, border-color 150ms',
  }

  return (
    <div className="pilot-cta-row">
      <a
        href="/getting-started/prerequisites"
        className="pilot-cta-primary"
        style={{
          ...base,
          backgroundColor: BRAND_FILL,
          color: CTA_FG,
        }}
      >
        Get Started
      </a>
      <a
        href="https://github.com/ylcn91/pilot"
        target="_blank"
        rel="noreferrer"
        className="pilot-cta-secondary"
        style={{
          ...base,
          backgroundColor: 'transparent',
          color: 'inherit',
          border: '1px solid currentColor',
        }}
      >
        View on GitHub
      </a>
      <style>{`
        .pilot-cta-primary:hover { background-color: ${BRAND_FILL_HOVER} !important; }
        .pilot-cta-primary:active { background-color: ${BRAND_FILL_ACTIVE} !important; }
        .pilot-cta-secondary:hover { background-color: color-mix(in srgb, currentColor 8%, transparent) !important; }
        .pilot-cta-primary:focus-visible,
        .pilot-cta-secondary:focus-visible {
          outline: 2px solid ${BRAND_FILL};
          outline-offset: 2px;
        }
      `}</style>
    </div>
  )
}

type Feature = {
  icon: string
  title: string
  description: string
}

const FEATURES: Feature[] = [
  {
    icon: '🔌',
    title: 'Multi-Platform',
    description: 'GitHub, GitLab, Linear, Jira, Asana, Azure DevOps, Plane, Discord.',
  },
  {
    icon: '🚀',
    title: 'Autopilot',
    description: 'CI monitoring, auto-merge, and a failure feedback loop.',
  },
  {
    icon: '💬',
    title: 'Telegram Bot',
    description: 'Chat, research, plan, and execute tasks from your phone.',
  },
  {
    icon: '✅',
    title: 'Quality Gates',
    description: 'Test, lint, and build gates with automatic retry.',
  },
  {
    icon: '🔍',
    title: 'Self-Review',
    description: 'Code review runs before every PR is pushed.',
  },
  {
    icon: '🧩',
    title: 'Epic Decomposition',
    description: 'Complex tickets are automatically split into subtasks.',
  },
  {
    icon: '🪝',
    title: 'Claude Code Hooks',
    description: 'Inline quality gates and destructive-command blocking.',
  },
  {
    icon: '📦',
    title: 'Multi-repo',
    description: 'Manage many repositories from a single instance.',
  },
  {
    icon: '♻️',
    title: 'Session Resume',
    description: '40% token savings on self-review via context continuation.',
  },
  {
    icon: '🧠',
    title: 'Model Routing',
    description: 'Trivial tasks use fast models; complex ones reason deeply.',
  },
  {
    icon: '📊',
    title: 'Dashboard',
    description: 'Terminal UI with token usage, costs, and task history.',
  },
  {
    icon: '⚡',
    title: 'Hot Upgrade',
    description: 'Self-update without downtime via in-process binary swap.',
  },
  {
    icon: '🔔',
    title: 'Notifications',
    description: 'Slack, Telegram, email, webhooks, and PagerDuty.',
  },
]

export function FeatureGrid() {
  return (
    <section aria-label="Core features" className="pilot-feature-grid">
      {FEATURES.map((f) => (
        <FeatureCard key={f.title} {...f} />
      ))}
      <style>{`
        .pilot-feature-grid {
          display: grid;
          grid-template-columns: 1fr;
          gap: 12px;
          margin-top: 1.5rem;
        }
        @media (min-width: 640px) {
          .pilot-feature-grid { grid-template-columns: repeat(2, 1fr); }
        }
        @media (min-width: 1024px) {
          .pilot-feature-grid { grid-template-columns: repeat(3, 1fr); }
        }
      `}</style>
    </section>
  )
}

function FeatureCard({ icon, title, description }: Feature) {
  const card: CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    padding: 16,
    borderRadius: 8,
    border: '1px solid color-mix(in srgb, currentColor 12%, transparent)',
    background: 'color-mix(in srgb, currentColor 3%, transparent)',
  }

  return (
    <div style={card}>
      <span aria-hidden="true" style={{ fontSize: 22, lineHeight: 1 }}>
        {icon}
      </span>
      <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>{title}</h3>
      <p style={{ margin: 0, fontSize: 14, opacity: 0.8, lineHeight: 1.4 }}>
        {description}
      </p>
    </div>
  )
}

export function HeroImage({
  src,
  alt,
  width,
  height,
}: {
  src: string
  alt: string
  width: number
  height: number
}): ReactNode {
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={src}
      alt={alt}
      width={width}
      height={height}
      // @ts-expect-error fetchpriority is a valid DOM attribute not yet typed
      fetchpriority="high"
      decoding="async"
      style={{
        width: '100%',
        height: 'auto',
        borderRadius: 12,
        marginTop: '1.5rem',
        border: '1px solid color-mix(in srgb, currentColor 12%, transparent)',
      }}
    />
  )
}
