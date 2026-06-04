# Frontend Design Audit — web (docs/ Nextra) + desktop (Wails React)

_Generated 2026-06-03 · 10-lens parallel audit + synthesis (read-only). Source: pilot-design-audit workflow._

## Executive summary

Both frontends are functional but visibly under-designed, and several findings are not taste calls — they are verified defects. WEB (Nextra docs): I confirmed the entire custom typography layer is dead code — globals.css sets font-family to var(--font-geist-sans/mono) but geist is not a dependency and the variables are defined nowhere, so the whole site silently renders in the browser UA font; the landing page is a stock markdown dump with an ASCII-art "hero" in a code fence, no H1, plain-link CTAs, a 1.2MB unoptimized hero image, and (worst for reach) zero per-page metadata across ~30 pages so every interior page shares one title and ships descriptionless. Root-level OG/Twitter meta is genuinely good; everything below the root is boilerplate. DESKTOP (Wails React): a coherent GitHub-dark TUI aesthetic undermined by an accessibility-near-zero build — no ARIA, no landmarks, no headings, no keyboard handlers (every clickable row is a bare div+onClick), no focus-visible styles, and a palette that ships sub-AA contrast (gray #6e7681 at ~3.1:1 used 30x for real 10px content, disabled slate #3d4450 at ~1.5:1 effectively invisible). It has no real type scale (≈everything is text-[10px]), no loading state, and a hard-coded two-column layout with no breakpoints. Honest verdict: the content and the underlying palette are good; the design system, accessibility, and SEO execution are not. None of the P0s are large fixes — they are high-impact, low-effort wins being left on the table.

**Web verdict:** A near-stock Nextra v4 theme with the branding work unfinished and the SEO foundation missing. The one piece of custom CSS is inert (undefined Geist vars, geist not installed → UA-font fallback sitewide), the landing page is README-grade markdown (ASCII hero, no H1, no styled CTAs, 1.2MB raw PNG), and — the biggest miss — all ~30 content pages have no frontmatter so they share a single title and have no meta description. Brand color #6BADD3 lives only in the logo and is applied nowhere else; no favicon/robots/sitemap exist. Strengths: solid root OG/Twitter metadata, complete _meta.js nav, well-written copy, disciplined heading hierarchy on interior pages. Fixing the font wiring, adding frontmatter, wiring the version constant, and building a real hero would move this from "template" to "product page" cheaply.

**Desktop verdict:** A tastefully chosen, internally consistent GitHub-dark TUI-in-a-window that fails as an accessible, scalable UI. It scores ~2/5 on accessibility: no ARIA, landmarks, headings, labels, live regions, or keyboard handlers anywhere, and outline-none strips the only focus ring; every interactive row is a non-focusable div+onClick. The palette is coherent (11 well-mapped semantic tokens) but the muted-text token gray #6e7681 (~3.1:1) carries most secondary content and every empty state at 10px, below AA, and disabled slate #3d4450 (~1.5:1) is invisible. There is no type scale (one 10px tier with 9/8px outliers), no loading state (empty states render during fetch so a populated app looks broken on launch), no shared Row/Button/EmptyState primitives, the brand hex #7eb8da is hardcoded in 5 files despite an existing steel token, and the layout is a fixed 50/50 flex with zero breakpoints that starves the focus panels. The aesthetic is the strong part; the system underneath is fragile and inaccessible.

## Cross-cutting themes

- No shared design tokens enforced: desktop hardcodes the steel brand hex #7eb8da inline in 5+ files despite a steel token existing, and re-declares the whole palette in GitGraphPanel; web has zero brand-color token (logo #6BADD3 applied nowhere). One source of truth is missing on both surfaces.
- No type scale on either surface: desktop tailwind.config has no fontSize tokens (≈everything is text-[10px] with 9/8px outliers); web's only custom font rules are dead code, so there is no controlled typography at all.
- No focus-visible styles anywhere on desktop (outline-none actively removes the native ring); keyboard users cannot see focus. Web relies entirely on Nextra defaults, never verified.
- Sub-AA color contrast from muted tokens: desktop gray #6e7681 (~3.1:1) and slate #3d4450 (~1.5:1) on tiny text; web's navbar version (0.5em + opacity:0.5) and #6BADD3-on-white (~1.9:1) also fail. Low-contrast greys are carrying the highest-frequency content.
- Accessibility treated as out-of-scope: desktop has essentially none (no ARIA/landmarks/headings/labels/keyboard); web's landing page has no H1 (product name is ASCII art in a code fence) and ASCII diagrams across 18 pages are unreadable to screen readers.
- Stock/un-branded shells: web is a near-default Nextra theme (no accent color, empty footer, no banner, no themeConfig); desktop is a TUI ported pixel-for-pixel into a webview with no responsive intent.
- Version string drift: the navbar literal v2.166.10 (web) bypasses lib/version.ts CURRENT_VERSION, and ~13 prose locations hardcode already-stale versions (151 vs 166) instead of <CurrentVersion/>.
- Missing/inconsistent component states: no loading skeletons, copy-pasted empty states with divergent padding, and error state tracked but unrendered (desktop); plain-link CTAs with no button/hover/focus/active states (web).
- ASCII-art used as a design primitive on both surfaces (web hero/diagrams, desktop logo/git graph) — looks like a README, breaks accessibility, and depends on a monospace font that on web is itself broken.
- No responsive behavior on desktop (zero breakpoints, fixed 50/50 flex, shrink-0 panels with no max-height) and unoptimized/unsized images on web (1.2MB PNG, no width/height → CLS).

## Prioritized remediation plan

| # | Sev | Surface | Eff | Impact | Title |
|---|-----|---------|-----|--------|-------|
| 1 | P0 | web | M | high | Per-page SEO metadata: add frontmatter title+description to every content page |
| 2 | P0 | web | S | high | Fix dead font wiring — Geist vars are referenced but geist is not installed |
| 3 | P0 | desktop | M | high | Make interactive rows keyboard-operable and screen-reader-named |
| 4 | P0 | desktop | S | high | Add focus-visible styles globally; stop stripping the native ring |
| 5 | P1 | desktop | S | high | Fix sub-AA muted text: lighten the gray token and stop using slate for text |
| 6 | P1 | web | S | high | Give the landing page a real H1 (and convert the ASCII wordmark to an image) |
| 7 | P1 | desktop | M | high | Add a loading state so a populated dashboard does not look broken on launch |
| 8 | P1 | both | M | med | Wire the version constant; kill hardcoded/stale version strings |
| 9 | P1 | desktop | M | med | Add semantic landmarks and heading hierarchy (main/header/section/h2) |
| 10 | P1 | web | M | high | Optimize the 1.2MB hero image and split it from the OG card |
| 11 | P1 | web | M | high | Build a real hero with styled CTA buttons (replace plain markdown links) |
| 12 | P1 | web | S | med | Apply the brand color as a Nextra accent token |
| 13 | P2 | desktop | S | med | Tokenize the steel brand hex; stop hardcoding #7eb8da inline |
| 14 | P2 | desktop | M | med | Define a real type scale and raise the readability floor off 10px |
| 15 | P2 | desktop | M | med | Make the layout responsive and stop shrink-0 panels starving the focus panels |
| 16 | P2 | desktop | M | med | Extract shared EmptyState/Card/Button/status primitives to stop drift |
| 17 | P2 | desktop | S | med | Label form controls and announce streaming/status regions |
| 18 | P2 | desktop | S | med | Add color-scheme:dark and allow text selection on content surfaces |
| 19 | P2 | web | L | med | Replace load-bearing ASCII diagrams with accessible diagrams; fill the empty footer |
| 20 | P2 | web | M | med | Add favicon, robots.txt, sitemap.xml, and metadataBase |
| 21 | P3 | desktop | S | low | Normalize spacing/radii: one Row density step and one corner token for interactive surfaces |
| 22 | P3 | both | S | low | Respect prefers-reduced-motion and add decorative aria-hidden / progress semantics |
| 23 | P3 | desktop | M | low | Disambiguate amber's two meanings (warning vs in-progress) |
| 24 | P3 | web | M | low | Convert the 13-row feature table to a card grid; align nav label with its route |

### #1 [P0] Per-page SEO metadata: add frontmatter title+description to every content page (web, eff M, impact high)
**Files:** docs/content/index.mdx, docs/content/features/autopilot.mdx, docs/content/cli/commands.mdx, docs/app/layout.tsx (title.template)
**Fix:** Verified: 0 of ~30 MDX files have frontmatter, so generateMetadata() returns empty and all interior pages inherit the root title 'Pilot — AI That Ships Your Tickets' with NO meta description. Add a YAML block (title: + description:) to the top of every content/**/*.mdx file; set title.template: '%s — Pilot Docs' in the root metadata so frontmatter only supplies the page-specific part. This unblocks unique titles and descriptions across the whole site.

### #2 [P0] Fix dead font wiring — Geist vars are referenced but geist is not installed (web, eff S, impact high)
**Files:** docs/app/globals.css, docs/app/layout.tsx, docs/package.json
**Fix:** Verified: globals.css sets body→var(--font-geist-sans) and code→var(--font-geist-mono) but geist is absent from package.json deps and the vars are defined nowhere, so the whole site falls back to the UA font. Either: npm i geist, then in layout.tsx `import { GeistSans } from 'geist/font/sans'; import { GeistMono } from 'geist/font/mono'` and add className={`${GeistSans.variable} ${GeistMono.variable}`} to <html> (this emits the vars globals.css expects); OR delete the two globals.css rules and let nextra-theme-docs own typography. Do not ship dangling var() refs.

### #3 [P0] Make interactive rows keyboard-operable and screen-reader-named (desktop, eff M, impact high)
**Files:** desktop/frontend/src/components/QueuePanel.tsx:64-68, desktop/frontend/src/components/HistoryPanel.tsx:35-39, desktop/frontend/src/components/AutopilotPanel.tsx:38-42, desktop/frontend/src/components/RadarPanel.tsx:50-53
**Fix:** Every clickable row is a <div onClick> with no role/tabIndex/onKeyDown (grep finds zero role=/tabIndex/onKeyDown across src). Convert URL-opening rows (QueueRow, HistoryRow, PRRow) to real <button type="button" className="... text-left w-full"> (or <a href>); make the RadarPanel disclosure a <button aria-expanded={expanded} aria-controls={panelId}> with the body carrying id={panelId}. Extract a shared ui/Row.tsx since this repeats 4x. This is the strongest argument for one interactive primitive.

### #4 [P0] Add focus-visible styles globally; stop stripping the native ring (desktop, eff S, impact high)
**Files:** desktop/frontend/src/styles/globals.css, desktop/frontend/src/components/CodexChatPanel.tsx:127,135,149, desktop/frontend/src/components/Header.tsx:38-45
**Fix:** The 3 Codex inputs use outline-none with only a 1px border-color swap; all buttons have no focus style at all. Add to globals.css @layer base: `:where(button,a,input,select,textarea,[tabindex]):focus-visible { outline: 2px solid #7eb8da; outline-offset: 2px; }`. Add focus-visible:ring-2 focus-visible:ring-steel to buttons. Keep focus:border-steel as a secondary cue but do not rely on it alone. Pairs with rank 3 to make keyboard nav usable.

### #5 [P1] Fix sub-AA muted text: lighten the gray token and stop using slate for text (desktop, eff S, impact high)
**Files:** desktop/frontend/tailwind.config.ts:13, desktop/frontend/src/components/MetricsCards.tsx:35-36, desktop/frontend/src/components/LogsPanel.tsx:55, desktop/frontend/src/components/Header.tsx:39, desktop/frontend/src/components/CodexChatPanel.tsx:84
**Fix:** Verified tokens: gray #6e7681 (~3.13:1 on card, ~3.47:1 on bg) carries 30 uses of 10px secondary content + every empty state; disabled slate #3d4450 (~1.47:1) is invisible. Lighten `gray` to ≥#7d8590 (or collapse text uses to the existing midgray #8b949e at 4.67:1). Switch disabled:text-slate → disabled:text-gray disabled:opacity-50 (+cursor-not-allowed) in Header and CodexChat. Reserve slate/#3d4450 for borders/fills only. Re-verify every text-gray text-[10px] pairing after the change.

### #6 [P1] Give the landing page a real H1 (and convert the ASCII wordmark to an image) (web, eff S, impact high)
**Files:** docs/content/index.mdx:1-10, docs/public/logo.svg
**Fix:** Verified: index.mdx opens with a ``` fenced ASCII 'PILOT' banner, has no `# ` heading, and the first heading is ## Quick Start — so the page has zero H1 (SEO + screen-reader loss; the hero is read char-by-char). Add `# Pilot — AI That Ships Your Tickets` as the first node, wrap the ASCII art aria-hidden or replace it with <img src="/logo.svg"> + alt, and demote the line-10 bold lede to a normal subtitle paragraph under the H1.

### #7 [P1] Add a loading state so a populated dashboard does not look broken on launch (desktop, eff M, impact high)
**Files:** desktop/frontend/src/hooks/useDashboard.ts:47-53, desktop/frontend/src/components/QueuePanel.tsx:118-119, desktop/frontend/src/components/LogsPanel.tsx:47-48, desktop/frontend/src/components/HistoryPanel.tsx:83-84
**Fix:** useDashboard inits every collection to []/zeros and only fills after the 1s poll, but no panel distinguishes 'not yet loaded' from 'loaded and empty' — so at startup every panel shows its 'no X' string and metrics show $0.000. Add a `loaded` flag set true after the first successful poll; while !loaded render a shared <Skeleton/> (reuse the existing .shimmer-bar utility) instead of empty copy. Only fall through to 'no active tasks' when loaded && length===0.

### #8 [P1] Wire the version constant; kill hardcoded/stale version strings (both, eff M, impact med)
**Files:** docs/app/layout.tsx:48, docs/lib/version.ts, docs/components/current-version.tsx, docs/content/getting-started/quickstart.mdx:41, docs/content/getting-started/installation.mdx:48, docs/content/cli/commands.mdx:589
**Fix:** Verified: navbar hardcodes v2.166.10 while lib/version.ts exports CURRENT_VERSION (the documented auto-bumped source of truth). Import it in layout.tsx and render {CURRENT_VERSION} in the navbar span. The <CurrentVersion/> component is registered but rendered nowhere — several prose/sample lines hardcode already-stale versions instead of using the current source of truth. Replace prose-facing version mentions with <CurrentVersion/> or CURRENT_VERSION interpolation. Marked 'both' as a cross-cutting source-of-truth fix (web-only files, but same drift class as desktop's hardcoded hexes).

### #9 [P1] Add semantic landmarks and heading hierarchy (main/header/section/h2) (desktop, eff M, impact med)
**Files:** desktop/frontend/src/App.tsx:19-48, desktop/frontend/src/components/Header.tsx:20, desktop/frontend/src/components/ui/Card.tsx:11-19, desktop/frontend/index.html
**Fix:** Entire app is div-soup (grep returns 0 for main/header/section/h1-h3). Wrap the body in <main>, make Header a <header>, render each Card as <section aria-labelledby={titleId}> with its title as <h2 id={titleId}> (keep identical visual classes). Give the ASCII logo block an <h1> (visually-hidden 'Pilot' or aria-label). Low-risk, style-preserving tag swaps with large navigability payoff.

### #10 [P1] Optimize the 1.2MB hero image and split it from the OG card (web, eff M, impact high)
**Files:** docs/public/pilot-preview.png, docs/content/index.mdx:25, docs/next.config.mjs:11-13, docs/app/layout.tsx:16-22
**Fix:** Verified 1,206,921 bytes at 2000x1348, shipped raw via markdown ![] (no width/height → CLS) with images.unoptimized:true, and it's the LCP element. Also the OG metadata DECLARES 1200x630 but the file is 2000x1348 (1.48:1 vs 1.91:1 → scrapers letterbox/crop). Pre-generate a ~1200px WebP/AVIF hero (<200KB) referenced with explicit width/height/fetchpriority="high", and a separate true 1200x630 og-image.png (or app/opengraph-image.png) for social. Add descriptive alt naming what the screenshot shows.

### #11 [P1] Build a real hero with styled CTA buttons (replace plain markdown links) (web, eff M, impact high)
**Files:** docs/content/index.mdx:1-21,49,91,124-127, docs/mdx-components.tsx
**Fix:** Every CTA is an inline markdown link with no button/hierarchy/hover/focus/active state, and the first one appears at line 49 below the install block. Register a Hero + CTA-row component in mdx-components.tsx: large logo.svg, <h1> value prop, a primary filled button (brand #6BADD3 bg, AA-darkened text ≥4.5:1) 'Get Started' and secondary 'View on GitHub' immediately under the value prop, with explicit :hover/:focus-visible/:active. Replace the two ASCII code blocks with a real SVG flow diagram. Demote the scattered → links to in-text links, one primary action per section.

### #12 [P1] Apply the brand color as a Nextra accent token (web, eff S, impact med)
**Files:** docs/app/layout.tsx:40, docs/app/globals.css, docs/public/logo.svg
**Fix:** Logo is #6BADD3 but the theme uses stock Nextra blue for links/active-nav/search/focus — the brand color is applied nowhere. Pass a brand hue to <Head color={{ hue: 202, saturation: 53 }} /> (approx HSL of #6BADD3) so links/active-nav/focus rings track the brand. For any text use of the color, use an AA-darkened variant (#6BADD3 on white is ~1.9:1, fails AA). Establishes one brand accent token instead of the stock default.

### #13 [P2] Tokenize the steel brand hex; stop hardcoding #7eb8da inline (desktop, eff S, impact med)
**Files:** desktop/frontend/src/components/Header.tsx:23, desktop/frontend/src/components/HistoryPanel.tsx:41,64, desktop/frontend/src/components/LogsPanel.tsx:57, desktop/frontend/src/components/MetricsCards.tsx:64, desktop/frontend/src/components/GitGraphPanel.tsx:6-12
**Fix:** steel #7eb8da already exists as a token yet is re-typed as inline style={{ color:'#7eb8da' }} in 5 files and re-declared (with sage/amber) as raw GitGraph consts. Replace inline-styled spans with className="text-steel". For SVG/canvas cases needing a raw value (Sparkline default, sparklineColor props), import from one shared COLORS module that the Tailwind config also consumes. Delete GitGraphPanel's duplicate color consts. One source of truth for the palette.

### #14 [P2] Define a real type scale and raise the readability floor off 10px (desktop, eff M, impact med)
**Files:** desktop/frontend/tailwind.config.ts, desktop/frontend/src/styles/globals.css:18-21, desktop/frontend/src/components/ui/Card.tsx:14, desktop/frontend/src/components/LogsPanel.tsx:54, desktop/frontend/src/components/QueuePanel.tsx:70-84
**Fix:** Verified: tailwind.config has no fontSize tokens; ≈everything is text-[10px] with 9px (RadarPanel/Codex) and 8px (Header logo) outliers, on a 12px base nothing uses. Add theme.fontSize tuples with baked line-heights, e.g. '2xs':['10px','14px'], xs:['11px','16px'], sm:['13px','18px'], base:['14px','20px']. Map roles: card titles→xs uppercase, primary metric values→sm/base bold, body rows→11px floor, meta/timestamps→2xs. Use rem so OS/browser zoom is honored. Replace the 8px ASCII logo with an SVG wordmark. Delete the scattered leading-* overrides once line-height ships in the tuples.

### #15 [P2] Make the layout responsive and stop shrink-0 panels starving the focus panels (desktop, eff M, impact med)
**Files:** desktop/frontend/src/App.tsx:30-45, desktop/frontend/src/components/AutopilotPanel.tsx:69, desktop/frontend/src/components/RadarPanel.tsx:129, desktop/frontend/src/components/HistoryPanel.tsx:81, desktop/main.go:24-27
**Fix:** Fixed two-equal-column flex with zero breakpoints (grep for sm:/md:/lg:/@container = none); at the 480px default each column is ~235px and never collapses. Replace with grid grid-cols-1 lg:grid-cols-2 (or a @container query keyed to window width). Separately, Autopilot/Radar/History are shrink-0 with no max-height so they grow and crush the flex-1 Queue/Logs — give each an explicit height budget (max-h-[22%] or convert all scrollable left panels to flex-1 min-h-0 with sensible flex-basis). Reconsider MinWidth:400 in main.go.

### #16 [P2] Extract shared EmptyState/Card/Button/status primitives to stop drift (desktop, eff M, impact med)
**Files:** desktop/frontend/src/components/ui/EmptyState.tsx (new), desktop/frontend/src/components/ui/Card.tsx:9-20, desktop/frontend/src/components/MetricsCards.tsx:26-44, desktop/frontend/src/components/ui/StatusIcon.tsx:3-24, desktop/frontend/src/components/QueuePanel.tsx:10-26
**Fix:** 7 hand-rolled empty states with divergent padding; a second inline 'card' duplicated in MetricsCards; the status union + status→color map copy-pasted across StatusIcon/QueuePanel/ProgressBar; hover text split between text-white and text-lightgray. Add ui/EmptyState.tsx (canonical text-midgray text-[10px] px-1.5 py-2 text-center) and replace all 7. Extend Card with an optional action slot and have MetricCard use it instead of forking chrome. Hoist the status union + STATUS_TEXT_COLOR into ui/status.ts. Add ui/Button.tsx with one hover (text-lightgray, drop hover:text-white). Removes the structural drift behind several other findings.

### #17 [P2] Label form controls and announce streaming/status regions (desktop, eff S, impact med)
**Files:** desktop/frontend/src/components/CodexChatPanel.tsx:126,134,148, desktop/frontend/src/components/LogsPanel.tsx:38-46, desktop/frontend/src/components/Header.tsx:33-36, desktop/frontend/src/components/ui/ProgressBar.tsx:28-39
**Fix:** cwd input, sandbox <select> (security-relevant read-only vs danger-full-access), and prompt <textarea> have no label/aria-label/placeholder. Add aria-label='Working directory'/'Sandbox mode'/'Codex prompt' + a textarea placeholder. Add role='log' aria-live='polite' to the Logs and Codex transcript scroll containers, role='alert' to the approval queue, role='status' + sr-only text to the Header daemon dot, and role='progressbar' aria-valuenow/min/max to ProgressBar (status is currently color-only).

### #18 [P2] Add color-scheme:dark and allow text selection on content surfaces (desktop, eff S, impact med)
**Files:** desktop/frontend/src/styles/globals.css:13-31, desktop/frontend/index.html, desktop/frontend/src/App.tsx:20
**Fix:** No color-scheme declaration anywhere, so the native Codex <select> popup and scrollbars render in light chrome against the #1e222a UI. Add color-scheme:dark to html/body/#root and <meta name='color-scheme' content='dark'> to index.html (one-line fix). Separately, .wails-mode sets user-select:none on the whole app, blocking copy of logs/SHAs/errors in a text-heavy dev tool — scope user-select:none to the titlebar/drag region only and re-enable user-select:text on Logs, GitGraph, Codex transcript, and error/SHA spans.

### #19 [P2] Replace load-bearing ASCII diagrams with accessible diagrams; fill the empty footer (web, eff L, impact med)
**Files:** docs/content/concepts/architecture.mdx:9-30,45-97,202-249,283-316, docs/content/features/autopilot.mdx:103-129, docs/app/layout.tsx:56
**Fix:** 18 pages render architecture/flow diagrams as Unicode box-drawing art in bare ``` fences — read as pipe/dash noise by screen readers and overflowing horizontally on mobile (their value is alignment, which rides on the broken mono font). Convert load-bearing diagrams to Mermaid (Nextra v4 supports it) or SVG with <title>/<desc>; if ASCII must stay, wrap in a figure with role='img' aria-label and a text summary. Separately, <Footer/> is empty on every page — give it children: copyright, BSL 1.1 license link, GitHub link.

### #20 [P2] Add favicon, robots.txt, sitemap.xml, and metadataBase (web, eff M, impact med)
**Files:** docs/app/icon.svg (new), docs/app/robots.ts (new), docs/app/sitemap.ts (new), docs/app/layout.tsx:8
**Fix:** Verified none exist (no favicon/robots/sitemap/manifest in the project). Add app/icon.svg (derive from logo.svg), app/robots.ts ({ rules:{ userAgent:'*', allow:'/' }, sitemap:'https://pilot.quantflow.studio/sitemap.xml' }), app/sitemap.ts mapping getPageMap() to URLs, and metadataBase:new URL('https://pilot.quantflow.studio') + alternates:{ canonical:'./' } in root metadata so relative URLs and canonicals resolve. Required for crawlability.

### #21 [P3] Normalize spacing/radii: one Row density step and one corner token for interactive surfaces (desktop, eff S, impact low)
**Files:** desktop/frontend/src/components/QueuePanel.tsx:66, desktop/frontend/src/components/AutopilotPanel.tsx:40, desktop/frontend/src/components/RadarPanel.tsx:51, desktop/frontend/src/components/CodexChatPanel.tsx:93,28,127, desktop/frontend/src/components/ui/ProgressBar.tsx:18
**Fix:** Conceptually identical list rows use gap 1/1.5/3 and px 1/2 with no rationale; cards are rounded but inputs/select/textarea/chat surface/approval box/Start button are square, and three radii (rounded, rounded-sm, none) coexist. Once the shared Row primitive exists (rank 16) standardize on gap-2 px-2 py-0.5 rounded. Pick one corner token for interactive surfaces (rounded-sm for inputs/buttons/bars, rounded for cards) and apply to the currently-square elements.

### #22 [P3] Respect prefers-reduced-motion and add decorative aria-hidden / progress semantics (both, eff S, impact low)
**Files:** desktop/frontend/src/styles/globals.css:43-71, desktop/frontend/src/components/ui/Sparkline.tsx:37, desktop/frontend/src/components/GitGraphPanel.tsx:94-103, docs/app/globals.css
**Fix:** Desktop shimmer/pulse animations run infinitely with no reduced-motion guard — add @media (prefers-reduced-motion: reduce){ .shimmer-bar,.pulse{ animation:none!important } } and keep a static high-contrast state. Mark decorative SVGs (Sparkline, ASCII logo, GitGraph glyphs) aria-hidden or give role='img' aria-label summaries. On web, optionally add a project-owned reduced-motion block to globals.css as a guard for future custom JSX. Low priority polish.

### #23 [P3] Disambiguate amber's two meanings (warning vs in-progress) (desktop, eff M, impact low)
**Files:** desktop/frontend/src/components/AutopilotPanel.tsx:20-28, desktop/frontend/src/components/CodexChatPanel.tsx:13-18, desktop/frontend/src/components/RadarPanel.tsx:7-12, desktop/frontend/src/components/QueuePanel.tsx:21
**Fix:** amber #d4a054 means both 'warning/medium-risk' (radar/logs/failure count) and 'in-progress' (waiting_ci, awaiting_approval, codex running), while running is steel-blue in the queue — the same concept reads as two colors. Make in-progress consistently steel (running/waiting_ci/merging/codex-running) and reserve amber for warning/needs-attention only: AutopilotPanel waiting_ci 'text-amber'→'text-steel', CodexChatPanel running spinner bg-amber→bg-steel.

### #24 [P3] Convert the 13-row feature table to a card grid; align nav label with its route (web, eff M, impact low)
**Files:** docs/content/index.mdx:75-91, docs/content/_meta.js:7, docs/content/navigator/_meta.js
**Fix:** The flagship 'Core Features' section is a dense 13-row markdown table that reads as a spec sheet and wraps badly on mobile — convert to Nextra <Cards>/<Card> (2-3 cols collapsing to 1) with icons and headings. Separately, _meta.js labels the /navigator route 'Context Intelligence' while landing copy and CLAUDE.md call it 'Navigator' — three names for one section; pick one canonical name and align the slug, label, and in-page references.

## Per-lens scores

| Surface | Lens | Score |
|---------|------|-------|
| desktop | visual-layout-spacing-typography | 4.5/10 |
| desktop | color-contrast-theming-darkmode | 5.5/10 |
| desktop | component-consistency-states | 4/10 |
| desktop | accessibility | 2/10 |
| desktop | ux-information-architecture-responsive | 3.5/10 |
| web | landing-page-branding-visual | 3/10 |
| web | layout-theme-navigation | 3.5/10 |
| web | typography-content-readability | 4.5/10 |
| web | accessibility | 5.5/10 |
| web | seo-meta-performance | 5.5/10 |
