import type { Metadata } from 'next'
import { GeistMono } from 'geist/font/mono'
import { GeistSans } from 'geist/font/sans'
import { Footer, Layout, Navbar } from 'nextra-theme-docs'
import { Head } from 'nextra/components'
import { getPageMap } from 'nextra/page-map'
import 'nextra-theme-docs/style.css'
import './globals.css'
import { CURRENT_VERSION } from '../lib/version'

export const metadata: Metadata = {
  metadataBase: new URL('https://pilot.quantflow.studio'),
  title: {
    default: 'Pilot — AI That Ships Your Tickets',
    template: '%s — Pilot Docs',
  },
  description:
    'Autonomous AI development pipeline that turns tickets into pull requests',
  alternates: {
    canonical: './',
  },
  openGraph: {
    type: 'website',
    title: 'Pilot — AI That Ships Your Tickets',
    description:
      'Autonomous AI development pipeline. Label a ticket, get a PR. Self-hosted, source-available.',
    url: 'https://pilot.quantflow.studio',
    siteName: 'Pilot Docs',
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Pilot — AI That Ships Your Tickets',
    description:
      'Autonomous AI development pipeline. Label a ticket, get a PR. Self-hosted, source-available.',
  },
}

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html
      lang="en"
      dir="ltr"
      className={`${GeistSans.variable} ${GeistMono.variable}`}
      suppressHydrationWarning
    >
      <Head color={{ hue: 202, saturation: 53 }} />
      <body>
        <Layout
          navbar={
            <Navbar
              logo={
                <span style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
                  <img
                    src="/logo.svg"
                    alt="Pilot"
                    height={24}
                    style={{ height: 24, width: 'auto', alignSelf: 'center' }}
                  />
                  <span style={{ fontSize: '0.8em', opacity: 0.6, fontWeight: 400 }}>
                    {CURRENT_VERSION}
                  </span>
                </span>
              }
              projectLink="https://github.com/ylcn91/pilot"
              chatLink="https://discord.gg/Hsz63MTB3c"
            />
          }
          pageMap={await getPageMap()}
          footer={
            <Footer>
              <div
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'center',
                  gap: 16,
                  width: '100%',
                  justifyContent: 'space-between',
                }}
              >
                <span>
                  © {new Date().getFullYear()} Pilot · Built by{' '}
                  <a href="https://quantflow.studio" target="_blank" rel="noreferrer">
                    QuantFlow Studio
                  </a>
                </span>
                <span style={{ display: 'flex', gap: 16 }}>
                  <a
                    href="https://github.com/ylcn91/pilot/blob/main/LICENSE"
                    target="_blank"
                    rel="noreferrer"
                  >
                    BSL 1.1 License
                  </a>
                  <a
                    href="https://github.com/ylcn91/pilot"
                    target="_blank"
                    rel="noreferrer"
                  >
                    GitHub
                  </a>
                </span>
              </div>
            </Footer>
          }
        >
          {children}
        </Layout>
      </body>
    </html>
  )
}
