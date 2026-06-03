import { ImageResponse } from 'next/og'

export const alt = 'Pilot — AI That Ships Your Tickets'
export const size = { width: 1200, height: 630 }
export const contentType = 'image/png'

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: '100%',
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          padding: '80px',
          backgroundColor: '#0d1117',
          color: '#e6edf3',
          fontFamily: 'sans-serif',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 24,
            marginBottom: 40,
          }}
        >
          <div
            style={{
              width: 72,
              height: 72,
              borderRadius: 16,
              backgroundColor: '#6BADD3',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#0b0f14',
              fontSize: 44,
              fontWeight: 700,
            }}
          >
            P
          </div>
          <span style={{ fontSize: 40, fontWeight: 600, color: '#6BADD3' }}>
            Pilot
          </span>
        </div>
        <div style={{ fontSize: 68, fontWeight: 700, lineHeight: 1.1 }}>
          AI That Ships Your Tickets
        </div>
        <div
          style={{
            fontSize: 30,
            color: '#9aa5b1',
            marginTop: 28,
            maxWidth: 900,
            lineHeight: 1.4,
          }}
        >
          Autonomous AI development pipeline. Label a ticket, get a PR.
        </div>
      </div>
    ),
    { ...size },
  )
}
