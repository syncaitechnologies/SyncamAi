# SyncCam AI — UX & Product Design Specification v1.0

**Document:** UX & Product Design v1.0 (Draft for Review)
**Date:** July 31, 2026
**Source:** `PRD-SyncCam-AI.md` (v1.0), `ARCHITECTURE.md` (v1.0), `AI-ARCHITECTURE.md` (v1.0)
**Design posture:** Apple-grade clarity and restraint, Tesla-grade operational focus, Google-grade data density and accessibility. Dark-first monitoring surfaces, light-first document surfaces. "Detection over surveillance" as a UX philosophy, not just an architecture one.

---

## Table of Contents

1. [Design Philosophy](#1-design-philosophy)
2. [Design Language & Tokens](#2-design-language--tokens)
3. [App Strategy & Navigation Shell](#3-app-strategy--navigation-shell)
4. [State System (Empty / Loading / Error / Offline)](#4-state-system)
5. [Web Screens (PWA)](#5-web-screens-pwa)
6. [Mobile, Tablet & Guard Apps](#6-mobile-tablet--guard-apps)
7. [End-to-End Flows](#7-end-to-end-flows)
8. [Accessibility Standard](#8-accessibility-standard)
9. [Cross-Screen Patterns (Privacy, Evidence, Notifications)](#9-cross-screen-patterns)
10. [Design Acceptance Criteria](#10-design-acceptance-criteria)

---

## 1. Design Philosophy

### 1.1 The Product Is a Sentence

> **SyncCam turns CCTV pixels into decisions — the operator's job is to decide, never to hunt.**

Every screen must answer, within 5 seconds of load: *What happened, where, how serious, and what should I do next?* If a screen can't answer those four questions, it is not finished.

### 1.2 Principles (mapped from Apple HIG / Material / WCAG)

| # | Principle | Definition | SyncCam expression |
|---|---|---|---|
| U1 | **Clarity** | Text legible at every size; icons precise; decorations minimal | 16px minimum body; severity = color + icon + word, never color alone |
| U2 | **Deference** | UI gets out of the way; the video and the truth are the content | Video tiles never carry chrome; metadata collapses into hover overlays |
| U3 | **Depth** | Layers of meaning through hierarchy, elevation, motion | Alert cards raise in elevation with severity; life-safety pulses, operational does not |
| U4 | **Direct manipulation** | People act on the thing itself | Draw restricted zones directly on live video; drag tiles to rearrange the wall |
| U5 | **Progressive disclosure** | Complexity on demand | Operator sees 3 actions; supervisor sees 7; admin sees everything behind "More" |
| U6 | **Defaults that work** | Ship tuned defaults, tune per site later | PPE matrix presets per vertical; severity routing defaults from PRD §9 |
| U7 | **3-click response** | Any alert is actionable in ≤3 clicks (PRD §6 usability) | Alert → Acknowledge + Dispatch is a 2-tap path from anywhere in the app |
| U8 | **Trust through transparency** | AI is shown as evidence, never as magic | Every detection shows confidence, model version, and evidence clip |
| U9 | **Privacy by design in the UI** | Pixels people shouldn't see never appear | Masked zones render as solid color + label; raw feeds are role-scoped with logged access |
| U10 | **Calm urgency** | Emergency UI is fast, never frantic | Pulsing only on life-safety; no animation on operational alerts |

### 1.3 Personas → Surfaces

| Persona | Primary surface | Primary job-to-be-done | North-star metric |
|---|---|---|---|
| Rajan — SOC Analyst | Web: Alert Center + Camera Wall | Triage and dispatch in seconds | Alert triage time ≤30s |
| Priya — Site Safety Manager | Web: Dashboard + Incident Dashboard | Prove compliance, prevent accidents | PPE compliance %; incidents/quarter |
| Meera — Operations Director | Web: AI Analytics + Heatmaps | Right-size staff and space | Occupancy accuracy; dwell analytics |
| Vikram — CSO | Web: Dashboard (multi-site) + Reports | Standardize security across sites | Cross-site incident rate; ROI report |
| Dr. Anand — Hospital Admin | Web: Incident Dashboard + Settings | Patient/visitor safety | Fall-to-response time ≤5s |
| Suresh — HR Manager | Web: Attendance + Employees | Accurate payroll | Attendance accuracy; fraud rate |
| Kavya — Compliance Auditor | Web: Reports + Audit trails | Evidence-grade documentation | Tamper-evident report completeness |
| IT Admin | Web: Settings + Camera Health | Zero-touch operations | Camera uptime SLA |
| Guard — Ravi | Guard App | Respond on the ground | Dispatch-to-arrival time |
| On-call Manager | Mobile App | Triage from anywhere | MTTA off-hours |

---

## 2. Design Language & Tokens

### 2.1 Themes

| Theme | Purpose | Used in |
|---|---|---|
| **Night Ops** (default) | Extended monitoring sessions, low-light control rooms | Web app, Camera Wall, Alert Center, Live View, Mobile, Tablet, Guard |
| **Daylight** (optional) | Bright offices, reports review | Web app (settings toggle), Reports preview |
| **Paper** (fixed, light) | Documents, exports, PDFs | Reports, audit trails, print |

Auto-switch follows OS; operators may pin Night Ops. Transition 300ms, crossfade.

### 2.2 Color Tokens (Night Ops)

| Token | Value | Usage |
|---|---|---|
| `bg-0` | `#050506` | App background |
| `bg-1` | `#0B0B0E` | Surface / cards |
| `bg-2` | `#141419` | Elevated surfaces, drawers |
| `bg-3` | `#1D1D24` | Popovers, menus, hover |
| `stroke` | `#2C2C33` | Hairline separators, borders |
| `text-1` | `#F5F5F7` | Primary text |
| `text-2` | `#A1A1A6` | Secondary text |
| `text-3` | `#6E6E73` | Tertiary, placeholders, disabled |
| `accent` | `#0A84FF` | Sentinel Blue — primary actions, focus, selection |
| `accent-2` | `#409CFF` | Hover state of accent |
| `success` | `#30D158` | Healthy, allowed, confirmed |
| `warning` | `#FFD60A` | Medium severity, attention |
| `danger` | `#FF453A` | High severity, violations |
| `critical` | `#FF375F` | Life-safety ONLY (fire, weapon, fall, fight) |
| `info` | `#64D2FF` | Operational/system info |
| `ai` | `#BF5AF2` | AI-generated content, AI Assistant |

**Daylight theme** maps 1:1 (bg `#FFFFFF`, surface `#F5F5F7`, text `#1D1D1F`, same semantic accents with AA contrast).

### 2.3 Severity System

| Severity | Color | Icon | Label | Motion | Delivery (PRD FR-118) |
|---|---|---|---|---|---|
| Critical (life-safety) | `#FF375F` | Shield-exclamation | "Critical" | 3-beat pulse 1.2s, only on new | Push + SMS + WhatsApp + phone tree |
| High (security) | `#FF453A` | Triangle-alert | "High" | 1-beat emphasis | Push + SMS |
| Medium (compliance) | `#FFD60A` | Circle-alert | "Medium" | None (static) | Push |
| Low (operational) | `#64D2FF` | Info-circle | "Low" | None | Push (silent) / email digest |
| Info (system) | `text-3` | Gear | "Info" | None | Email digest only |

**Rule:** severity is never communicated by color alone — always icon + label. Pulse animation is **disabled** under `prefers-reduced-motion`.

### 2.4 Typography

System font stack (SF Pro on Apple devices, Segoe UI variable on Windows, Roboto on Android — the platform font, never a custom font for body text; monospace only for IDs/hashes/timestamps).

| Style | Size / Weight | Usage |
|---|---|---|
| Display | 34 / 700 | Dashboard hero numbers |
| Title-1 | 28 / 700 | Screen titles |
| Title-2 | 22 / 700 | Section headers |
| Title-3 | 20 / 600 | Card titles |
| Headline | 17 / 600 | Widget titles |
| Body | 15 / 400 (line 1.45) | Default text |
| Subhead | 13 / 400 | Metadata, captions |
| Caption-2 | 11 / 400 | Micro-labels, timestamps |
| Mono | 13 | Event IDs, hashes, plate numbers, FPS |

Tabular numerals for all live metrics (no width jitter). Minimum touch target 44×44pt on mobile, 32×32 on desktop.

### 2.5 Motion Tokens

| Token | Value | Usage |
|---|---|---|
| `enter` | 240ms `cubic-bezier(0.32,0.72,0,1)` | Panels, drawers, modals |
| `reorder` | 200ms ease-out | Wall tile reorder |
| `alert` | pulse 3× @ 1.2s, then static badge | New Critical alerts |
| `scrub` | 80ms | Timeline marker hover snap |
| `map-pulse` | 2× @ 1.5s | Incident pins on Maps |
| Reduced motion | All above → opacity crossfade 150ms | `prefers-reduced-motion` |

### 2.6 Core Components (used across all screens)

**KPI Card** — label (13/subhead, `text-2`) → value (Display or Title-1, tabular) → delta (inline icon + color) → optional sparkline (right-aligned, 40×24). Tap = drill-down.

**Event Card (Alert)** — 4-row structure: `[severity icon] [type title] [age chip]` / `camera · zone` / `confidence + model tag` / `thumbnail (right, 72×48)`. Left edge 3px severity bar. Elevated by severity (`bg-2` normal → `bg-3` critical).

**Video Tile** — 16:9 canvas. Top-left: name + status dot. Top-right: event badges. Bottom: 44px hover overlay (snapshot, spotlight, PTZ, open). Active-event tiles get 1px severity ring, never more.

**Timeline Scrubber** — 44px strip: event markers as colored ticks (severity), 24h micro-grid, playhead (draggable), ±pre/post context bands (FR-117).

**Confidence Meter** — thin 3px bar + % text; ≥ site threshold = solid success; below = warning. Always paired with model version on evidence views (U8).

**Filter Bar** — collapsible row of chips: `Type ▾` `Severity ▾` `Camera ▾` `Zone ▾` `Time ▾` + active-filter count badge + Clear all. Presets: "Last 24h", "Last 7d", "This shift".

**Status Pill** — dot + word (Online / Offline / Tampered / Masked / Recording / Degraded), always text-labeled.

**Empty State** — illustration-free, icon + title + one-line reason + primary action (see §4).

---

## 3. App Strategy & Navigation Shell

### 3.1 App Portfolio

| App | Platform | Phase | Role |
|---|---|---|---|
| **SyncCam Web (PWA)** | Browser, installable | MVP (PRD §10) | Full platform — command, config, compliance, reports |
| **SyncCam Mobile** | iOS + Android native | Phase 2 (PRD §11) | On-call triage, quick live view, attendance check, guard dispatch |
| **SyncCam Tablet** | iPad + Android tablet native | Phase 2 | Small-site command wall replacement (FR-201) |
| **SentinelGuard** | iOS + Android native | Phase 2 | Field responder: dispatch, patrol, panic, evidence |

Same design language, four information architectures tuned to each role. Single design system repo; all tokens from §2.

### 3.2 Web Shell

```
┌─────────────────────────────────────────────────────────────────────┐
│ Top bar: ☰ site-switcher ▏  ⏱ 14:32:06 (local)   🛰 system-health      │
│          alerts-badge(3)  🔔 notification-center   👤 user-menu         │
├──────────┬──────────────────────────────────────────────────────────┤
│ Sidebar  │                                                          │
│ 🏠 Dash  │                                                          │
│ 🎥 Wall  │                    CONTENT AREA                          │
│ ⚡ Live   │                (12-column grid)                          │
│ 🚨 Alerts │                                                          │
│ 📋 Incid. │                                                          │
│ ◷ Reports│                                                          │
│ ├─────── │                                                          │
│ 👥 People │                                                          │
│  › Attnd │                                                          │
│  › Empl  │                                                          │
│  › Visit │                                                          │
│ 🚗 Vehic. │                                                          │
│ 🗺 Maps   │                                                          │
│ ⛔ Zones  │                                                          │
│ ├─────── │                                                          │
│ 📊 Anal. │                                                          │
│  › Heat  │                                                          │
│ ✨ AI     │                                                          │
│ ├─────── │                                                          │
│ ⚙ Settings│  (visible per role)                                     │
│ 🛠 Roles  │                                                          │
│ 🔐 Perms  │                                                          │
└──────────┴──────────────────────────────────────────────────────────┘
```

**Rules:**
- Sidebar collapses to icon rail (64px) on <1200px; drawer on mobile.
- Section grouping is role-aware: an Auditor sees no "Settings" group at all.
- **The Alert badge is global**: rendered in the top bar on every screen, click = Alert Center. Critical alerts additionally flash the badge (U10, 3 beats max).
- Command-K (⌘K) global search: cameras, employees, plates, incidents, settings — the fastest path on every screen.
- Site switcher (multi-site CSO view) is a picker, not a separate page.

### 3.3 Mobile Shell

- Bottom tab bar: **Alerts (badge)** · Live · Map · People (HR view) / Activity (ops view) / Guard dispatch (manager view) · More.
- Push notification → deep link straight into Alert Detail (evidence preloaded).
- App unlock: Face ID / Touch ID / device biometric, always on for any role; PII content additionally requires it even when app is unlocked (re-auth per session).
- Offline banner: "Offline — showing last synced data" with amber strip; alerts still playable from cache (evidence clips downloaded on open).

### 3.4 Guard Shell

- Single-action-first: bottom **Accept / Arrived / Resolved** buttons change as task state progresses.
- Persistent top-right **PANIC** button (hold 1.5s to trigger, 1s cancel) on every screen.
- 4 tabs: Today · Incident · Patrol · Me.

---

## 4. State System

Every screen implements five states with the same anatomy (icon → title → reason → action):

| State | Title example | Reason | Action |
|---|---|---|---|
| **Empty** | "No alerts yet" | "You're all clear. Alerts will appear here the moment a detection confirms." | "Open Camera Wall" |
| **Loading** | "Loading live feed" | skeleton shimmer, 240ms max perceived | — |
| **Error** | "Can't reach the service" | connection hint + retry | "Retry" |
| **Offline** | "Edge offline" | store-and-forward note: "Detection continues on-site; events will sync" | "View last synced" |
| **Permission-gated** | "You don't have access" | role hint + request path | "Request access" (generates privileged-access request, audited) |

**Global rule:** never show a dead screen — every state offers the next true action.

---

## 5. Web Screens (PWA)

---

### 5.1 Dashboard

**Purpose.** The role-aware command summary. Answers "what happened, where, how serious" in 5 seconds. Serves every persona by reconfiguring the same 12-column grid (FR-207), never by separate pages.

**Layout.** Top hero strip (site name · live clock · incident count · system health) → KPI row (4 cards) → main grid: left (safety scorecard + trend charts, 8 cols) / right rail (live incident feed, 4 cols) → bottom row (camera health + occupancy mini-heatmap).

**Widgets**
- KPI cards: Active incidents · Alerts today · Detection uptime % · PPE compliance today % (Safety Manager) / Occupancy vs capacity (Ops Director) / Sites online (CSO)
- Safety Scorecard radial gauge (0–100, composed: incidents avoided, compliance, response time, FR-207)
- Incidents by type — 7-day stacked bar
- Alert volume by hour — 24h heat strip (grayscale-to-red ramp, colorblind-safe)
- Live incident feed — newest first, 6 rows, auto-scroll paused on hover
- Camera health mini-list — offline/tampered count + top 3 offenders
- Multi-site view (CSO): site cards grid with per-site KPI instead of hero

**Buttons & Actions**
- "Open Live Wall" (primary), "Review Alerts", "Create Report", "Acknowledge All" (with confirm for Critical)
- Widget-level: date-range chips (24h / 7d / 30d), "…" menu → drill, edit layout (drag/reorder), export widget
- KPI tap → the underlying screen (incidents → Incident Dashboard)

**Filters.** Time range · Site (multi-site only) · Severity · Module type. Sticky per user, reset via "Clear".

**Charts.** Stacked bar (type×day), heat strip (hour×severity), radial gauge, sparklines, per-site KPI cards with 30-day micro-trend.

**User Flow.**
1. Login (SSO or MFA) → 2. Dashboard renders role-aware defaults → 3. Scan KPI row (5s comprehension target) → 4. Alert appears in feed → 5. Click → Alert Center triage (§7.1). CSO: switch site or open cross-site comparison via hero.

**Permissions.** Viewer: read-only widgets, no Acknowledge. Operator: ack, drill, dispatch. Site Admin: edit layout, thresholds. Super Admin: site switcher. Auditor: dashboard shows evidence-state (no raw feeds).

**Responsive.** 12→8→4 cols. <1024px: feed drops below charts. <640px: KPI row becomes 2×2 grid; hero collapses to site name + alert badge; charts stack; "Acknowledge All" moves to a sticky action bar.

**Accessibility.** 16px body minimum; severity = icon+label; charts expose data tables via "View data" link; focus order: hero → alerts → KPIs; reduced-motion disables gauge sweep (render static at 120ms); screen reader announces new critical alerts via polite live region.

**States.** Empty: "No alerts — all systems nominal." Offline edge: hero shows amber "Edge offline — events buffering on-site".

---

### 5.2 Camera Wall

**Purpose.** Multi-camera situational awareness for SOC and site managers (FR-201). One glance: what's live, what's down, what's happening now.

**Layout.** Toolbar (search · filters · layout selector) → responsive video grid (1 / 4 / 9 / 16 / 25 tiles, 16:9) → status legend strip → optional alert rail on the right (when "Show alerts" enabled).

**Widgets**
- Video tiles: live feed (WebRTC ~300–500ms), name + status dot, event badge (severity chip + type), masked-zone tiles show solid fill + label, activity sparkline (last 10 min) on hover
- Event tiles: 1px severity ring + "LIVE: Fight — Bay 2" chip, preloaded thumbnail
- Camera search (⌘K or toolbar): name, group, IP, zone, tag
- Layout switcher: 1/4/9/16/25 + "spotlight"
- Status legend: Online · Recording · Offline · Tampered · Masked · Degraded

**Buttons & Actions**
- Tile hover: Snapshot · Spotlight (expand) · Open Live View · PTZ (if panned) · Mute alerts (zone)
- Toolbar: Layout ▾ · Filter ▾ · Sort (by event / offline / name) · "Ack All" (supervisor)
- Drag tiles to reorder; drag to a layout slot; double-click tile → full Live View
- Right-click: mark camera offline (creates IT ticket, FR-116), copy stream URL (admin)

**Filters.** Status (Online/Offline/Tampered/Masked/Degraded) · Group (floor/site/building) · Has active event · Event type · Camera type · Tag.

**Charts.** None — video is the chart. Per-tile 10-min activity sparkline on hover; optional "activity map" mode: tiles sorted by recent event activity with heat ordering.

**User Flow.** Wall → event ring attracts gaze → click tile → Live View with 10s pre-event context → triage (§7.1). Offline tiles: click → "Health detail" → camera-health card with last-known-good + ticket state.

**Permissions.** Live raw video = Operator+ (audited playback log per §15.6); Viewer sees event tiles only (no live pixels, U9); Auditor sees thumbnails with metadata, no raw wall; mask zones render as `#0B0B0E` + "Masked zone" label.

**Responsive.** Desktop: 16–25 tiles. <1280px: 9–16. Tablet (web): 4–9. Mobile web: 1–2 (use native app instead; wall page hides behind "Open in app" sheet).

**Accessibility.** Arrow-key navigation between tiles with strong focus ring (3px accent); per-tile `aria-label` = "Camera 12, Front Gate, online, live event Fight, severity critical"; event announced in polite region; captions on audio-enabled cameras; reduced-motion: no pulse rings, static chips.

**States.** Loading: shimmer tiles; Error: per-tile retry, never blanking the whole wall; Offline: tile → "Buffered until HH:MM" with store-and-forward note.

---

### 5.3 Incident Dashboard

**Purpose.** Forensic analysis of confirmed incidents — trends, hotspots, disposition quality. Owned by Safety Managers, Hospital Admins, CSOs; read for auditors. (FR-117 analytics, §9 metrics.)

**Layout.** KPI row (Incidents · Open · Avg response time · Confirmed %) → 8-col chart block (trend + breakdown) / 4-col filter rail → incident table (full width, sortable).

**Widgets**
- KPIs: total incidents (period) · open/unresolved · MTTA / MTTR · confirmation rate (confirmed vs dismissed, feeds eval-svc §5.1)
- Trend chart: daily incident line, 7/30/90d bands, forecast (Phase 3, dashed)
- Stacked bar: incidents by type; Donut: by severity (icon+label legend)
- Hotspot list: top zones/cameras by incident count with sparkline
- Incident table: time · type · severity · camera/zone · confidence · status · evidence-ready icon (hash ✓) · actions

**Buttons & Actions**
- Row click → Incident Dossier drawer (§7.2)
- "Export" (PDF/CSV/JSON) — shows tamper hash after generation; "New Report"; "Compare periods"; "Share link" (role-gated)
- Table: sort all columns; bulk select → batch export / batch resolve
- "…" → Open in Playback · Re-verify evidence (cloud verify, Phase 2) · Attach note

**Filters.** Date range (presets + custom) · Type · Severity · Status (open/resolved/dismissed) · Zone · Camera · Confidence ≥ (slider 0.5–0.9 per FR-101) · Disposition source (auto/SOC/verify).

**Charts.** Line trend, stacked bars, donut, hotspot list, response-time histogram (bucket bar).

**User Flow.** Open → set period → scan trend → click hotspot → filter to zone → open top incident → dossier → export for audit. CSO: cross-site comparison toggle (per-site normalization per camera count).

**Permissions.** Operator: view + notes + resolve. Auditor: view + export (read-only, every export audited). Site Admin: delete incidents (dual-approval for >30-day records, per §12.2). Viewer: view only, no export.

**Responsive.** <1024px: filter rail becomes top chip row; table → card list (each incident = Event Card + evidence chip). <640px: charts full-width stacked, table collapses to cards, exports move to bottom sheet.

**Accessibility.** Table = proper `<table>` with caption + sort announcements; chart "View as table" links; donut legend text-labeled; focus traps in dossier drawer; print stylesheet for auditor workflow.

**States.** Empty period: "No incidents in this range — good work." All dismissed: banner "All incidents dismissed this period; confirmation rate XX%".

---

### 5.4 Alert Center

**Purpose.** The SOC's command surface. A real-time triage queue where the operator validates, dispatches, or dismisses — 3 clicks or fewer (PRD §6). This screen is the product.

**Layout.** 3-column split: **Queue** (left, 5 cols) | **Detail** (center, 4 cols) | **Context** (right, 3 cols — evidence video + timeline). Columns resize; context collapses to bottom drawer <1280px.

**Widgets**
- Queue: Event Cards sorted by severity→age; unacknowledged first; new-arrival animation (1-beat emphasis, U10); group headers "Critical — 2"
- Detail: title + severity + age · evidence clip (auto-play, muted, 10s pre-event) · snapshot · confidence + model tag + site threshold · metadata (camera, zone, rule, event ID) · similar events (same camera/type, last 24h) · guard dispatch status
- Context: mini timeline with pre/post bands (FR-117 ±30s) · related events · camera info (health, uptime) · notes thread
- Queue header: unacknowledged count + filter summary + "Ack all" (supervisor-confirmed for critical)

**Buttons & Actions (all ≤3 clicks)**
- Primary (2-click path): **Acknowledge → Dispatch Guard** (selects nearest available guard, sends Guard App push)
- **Acknowledge** · **Escalate** (raises severity + phone-tree) · **Dismiss with reason** (required: false positive / duplicate / handled — reason feeds eval-svc retraining, ARCHITECTURE §5.1) · **Mute zone** (with duration) · **Snooze** (15/60 min) · **Add Note** · **Export evidence** · **Open Live**
- Keyboard: `A` ack, `E` escalate, `D` dismiss, `J/K` next/prev, `Space` play/pause evidence — printed in a footer hint bar (progressive disclosure)

**Filters.** Status (Unacked/Acked/Resolved/Dismissed) · Severity · Type · Site · Camera · Zone · Age (<5min/<1h/<24h) · Confidence. **Auto-filter**: "Show only life-safety" toggle — one-tap mode for critical response.

**Charts.** Inline: queue rate sparkline (alerts/min over shift), severity donut (small, header). No heavy charts — speed is the feature.

**User Flow (the canonical triage, §7.1):** Alert arrives (top-bar badge + sound if enabled) → badge click → Alert Center (1) → card auto-focuses, detail preloads (2) → Acknowledge (2) → Dispatch Guard (3) → status transitions live (Acked → Dispatched → Arrived → Resolved) → dismissed events tagged with reason → nightly digest summarizes confirmation rate.

**Permissions.** Acknowledge/escalate/dispatch = Operator+. Dismiss with reason = Operator (logged); dismiss override = Supervisor. Mute/snooze config = Site Admin. View = Viewer (metadata only, evidence clip requires raw-access role). Every action in audit log (FR-204, §15.6).

**Responsive.** <1440px: context panel below detail (collapsible). <1024px: 2-column queue+detail; context as bottom sheet. <640px: single-column; evidence video full-width; actions become bottom sticky bar (Ack | Escalate | More). Mobile uses the native app shell instead.

**Accessibility.** New-alert announcement via polite live region; queue items are buttons with full keyboard flow; severity icon+label on every card; evidence auto-play honors `prefers-reduced-motion` (shows poster frame instead); focus returns to queue after modal dismiss; no haptics on web, haptics on native (severity-scaled).

**States.** Empty: "No alerts — queue clear." Flood (aggregation engaged, FR-118): banner "12 similar events grouped — showing 1 of 3 groups", with "expand group" and "mute source" shortcuts. Offline edge: amber banner "Edge buffering; events may lag up to Xs".

---

### 5.5 Live View

**Purpose.** Single-camera forensic viewing with the full analytics layer: detections, zones, timeline, playback, PTZ (FR-201). Both the operator's inspection tool and the admin's configuration canvas.

**Layout.** Full-bleed video canvas (WebRTC live / HLS playback) → right rail (inspector: detections, timeline, metadata) → bottom scrubber when in playback → floating overlay controls.

**Widgets**
- Video canvas with overlay layers (toggles): detection boxes (class + confidence %, 2px accent lines, label chips), zone polygons (translucent fill + name), tripwires (dashed lines + direction arrows), track IDs (person/vehicle labels), privacy masks (solid fill + "Masked")
- Right rail: Live detections list (person 0.93 · PPE violation) · Event timeline (markers scrubbable) · Camera info (model, firmware, uptime, FPS, bitrate, health) · Rules active on this camera
- Bottom scrubber (playback): 24h strip, event ticks, pre/post context bands, playhead, speed (0.5–4×), frame-step
- PTZ control pad (ONVIF, FR-201): directional + zoom slider, presets, patrol mode
- Follow mode: click a detection track → camera-framing follows it across the wall's other cameras (FR-119 phase 2 hint, shown as ghost outline)

**Buttons & Actions**
- Transport: Live / Playback toggle · Play/Pause · Snapshot · Record clip (start/stop, hash-stamped) · Speed · Fullscreen
- Overlay toggles: Detections · Zones · Tracks · Masks · Heat (post-process)
- Inspector: click detection → track detail (appearance timeline); click event marker → jump + preload evidence
- PTZ: pad + presets + patrol; joystick gestures on touch
- "Open in Wall" (returns to wall at this camera) · "Add Zone Here" (jumps to Zone Builder, §5.11)

**Filters.** Overlay type toggles · Time jump (date picker + scrub) · Event-type marker filter on timeline.

**Charts.** Timeline strip (the chart), FPS/latency live meters (micro, right rail), detection confidence sparkline per track.

**User Flow.** From Alert Center → "Open Live" → auto-jumps to event time (playback) → scrub ±30s → confirm/reject → return (single Esc). From Wall → live inspection → enable detections → follow a person → switch camera to continue track (Phase 2 ReID).

**Permissions.** Live raw video = Operator+ with **every session logged** (audit-svc, §15.6). Masked zones never render regardless of role (U9). PTZ = Operator+ (PTZ usage audit + "PTZ operated by" indicator). Playback export = Auditor+. Config overlays = Site Admin.

**Responsive.** Desktop: canvas 16:9 + rail. <1024px: rail becomes bottom sheet. Mobile: full-screen canvas, controls auto-hide (tap to reveal), pinch-zoom on video, vertical rail in drawer. Landscape recommended (sheet: "Rotate for full timeline").

**Accessibility.** Overlay labels ≥13px with dark scrims (AA on any video content); keyboard PTZ (arrows + shift for presets); live captions when camera audio enabled; screen reader: detections announced in polite region at 1/4 rate (no spam); focus ring on video for transport controls; reduced-motion: follow mode animates via crossfade only.

**States.** Loading: canvas shimmer + "Buffering". Degraded: "5 FPS — network constrained" amber chip (graceful degradation, ARCHITECTURE §6.3). Offline camera: "Last recording HH:MM — camera offline" with health card link.

---

### 5.6 Attendance

**Purpose.** Face-recognition attendance with liveness (FR-102): who's in, who's missing, who's late — with evidence for payroll. Owned by HR; audited by compliance.

**Layout.** KPI row (Present · Absent · Late · On leave) → live punch feed (left, 4 cols — real-time entries with thumbnail + liveness badge) → roster table (right, 8 cols, filterable) → attendance trend chart (bottom, 6 cols) + late histogram (3 cols) + export bar.

**Widgets**
- KPI cards (today, per shift toggle)
- Punch feed: entry cards "Ravi S. — 08:59 · Entry gate 1 · liveness ✓ 0.99 · photo-optional mode" — live-append, auto-scroll
- Roster table: employee · department · shift · status pill · first punch · last punch · duration · confidence · evidence icon
- Liveness badges: ✓ verified · ⚠ spoof blocked (alert!) · − not required (attendance-only mode, §15.3)
- Exception drawer: missing-punch, late, spoof-attempt, low-confidence (<site threshold, needs review)
- Trend chart: attendance % by day (7/30d) · Late histogram by 15-min bucket

**Buttons & Actions**
- "Export payroll" (CSV/JSON, HRIS-sync via integration-svc) · "Sync HRIS" · "Adjust" (manual correction, requires reason + approver for changes >1h) · "View evidence" (opens punch clip, audited) · "Enroll employee" (→ §7.3) · Batch select → "Mark exception" / "Notify"
- Punch card click → employee profile drawer (§5.7)

**Filters.** Date · Shift (day/night/custom) · Department · Status (present/absent/late/on-leave/excused) · Site · Confidence ≥ slider.

**Charts.** Attendance % trend (line), late histogram, department breakdown (horizontal bar), spoof-attempt counter (warning chip, not chart).

**User Flow.** HR opens today's roster → reviews exceptions (spoof attempts first, amber) → opens evidence on low-confidence punch → adjust with reason → export → payroll sync → audit trail records every touch.

**Permissions.** HR Manager: full (view evidence photos, adjust, export). Auditor: read-only + export, no photos in "attendance-only" mode. Operator: view counts only (no PII). Super Admin: dual-approval override. **Privacy surfaces in UI:** each profile shows consent state + retention setting; "photos not stored" mode shows silhouette placeholder (U9).

**Responsive.** <1024px: punch feed becomes collapsible strip; table→cards. <640px: KPIs 2×2, roster paginated cards, export in bottom sheet; evidence viewer full-screen.

**Accessibility.** Statuses icon+word; table sortable with announcements; evidence clips muted by default (captions if audio); liveness badge conveyed textually ("Spoof attempt blocked"); screen reader order: exceptions → KPIs → feed.

**States.** Empty: "No punches yet — first shift starts 06:00." Spoof block: red banner "2 spoof attempts blocked today — review". HRIS sync error: "Sync pending — retry in 15 min" with retry.

---

### 5.7 Employees

**Purpose.** The person directory and enrollment hub: who's enrolled in face biometrics, who's on watchlists, who's compliant — and the enrollment pipeline (FR-102, §15.3).

**Layout.** Toolbar (search · filters · view toggle table/cards) → directory (table with profile drawer) → enrollment queue panel (right, collapsible) → watchlist tabs.

**Widgets**
- Directory rows: avatar (photo or privacy silhouette) · name · employee ID · department · site · role tag · enrollment status pill (Enrolled / Pending / Suspended / Not enrolled) · PPE violation count (30d) · actions
- Profile drawer: identity info · consent + retention state · enrollment quality (embedding quality bar) · attendance summary · incident involvement list (role-scoped) · watchlist state
- Enrollment queue: "3 pending enrollments" with photo-quality checklist (pose, lighting, occlusion — live feedback while enrolling, §7.3)
- Watchlist tabs: Internal (security-sensitive roles) · No-photo mode toggle

**Buttons & Actions**
- "Enroll" (wizard §7.3) · "Re-enroll" · "Suspend/Unsuspend" · "Remove" (soft-delete → erasure job + audit) · "Export roster" · "Invite" (self-service consent link for privacy opt-in)
- Row → profile drawer; drawer → "View attendance", "View alerts involving", "Request erasure" (Super Admin, dual-approval, generates §9.4 manifest)

**Filters.** Site · Department · Status · Enrollment state · Watchlist · PPE violations ≥ n · Consent state.

**Charts.** Enrollment funnel (invited → consented → enrolled → verified), PPE violations by employee (horizontal bar, top 10 — with "coach, don't punish" framing note).

**User Flow.** Onboard staff (§7.3) → consent captured in-app (signed, versioned) → capture 3 enrollment frames (quality-gated) → verify with a live test match → appears in directory → attendance works immediately. HR/Security split: HR cannot view watchlist; Security cannot view payroll fields.

**Permissions.** HR: directory, enrollment, adjustments. Site Security: watchlists, suspension. Auditor: read-only + export (no photos in no-photo mode). Biometric scope (ARCHITECTURE §12.3) required for any embedding access; every embedding access audited.

**Responsive.** Table→cards <1024px; enrollment queue moves to full-screen wizard on mobile; drawer becomes sheet.

**Accessibility.** Avatars have empty alt (decorative) — name is text; status pills text-labeled; enrollment quality feedback given as text + meter (not color alone); all dialogs focus-trapped.

**States.** Empty: "No employees yet — start with Enrollment." Pending consent: "Awaiting consent from 5 invites" with resend. Erasure in progress: "Erasure queued — completes within 24h (audit: #EV-…)" (compliance, §15.4).

---

### 5.8 Visitors

**Purpose.** Visitor lifecycle: pre-registration → arrival → hosted zones → departure, plus watchlist/blacklist hits (Phase 2: face + plate + QR fusion, PRD §11).

**Layout.** KPI row (Checked in · Expected · Pending host · Watchlist hits) → visitor stream (left) + watchlist banner → expected-vs-actual list → zone capacity chip → footer: visitor log export.

**Widgets**
- Visitor cards: name · purpose · host · check-in time · expected duration · badge state · photo (consent-aware) · status
- Arrival alerts: "Visitor arrived — Front Desk" (auto via face/plate match) with host notification status
- Watchlist banner: red, "Match on watchlist — Front Gate, 09:14" → opens alert with evidence
- Manual check-in form: name, host, purpose, expected duration, consent checkbox (required, versioned), badge print
- Zone access chip: which zones allowed (drawn from host's zones), "Escorted only" toggle

**Buttons & Actions**
- "Pre-register" (email/QR link to host) · "Check in" / "Check out" (manual override, logged) · "Print badge" · "Extend visit" · "Flag" (adds to watchlist w/ reason) · "Notify host" · "Export log"
- Card → detail drawer: photo, host, purpose, zones, timeline, watchlist state, consent record

**Filters.** Date · Status (expected/in/out/overdue/flagged) · Host · Zone · Source (auto/manual).

**Charts.** Visitor volume by hour (bar), top hosts (list), average visit duration (KPI delta).

**User Flow.** Pre-register → arrival auto-detected → host pinged (via notification center) → check-in (badge) → zone enforcement (restricted zones alert if unescorted) → check-out → log exported nightly.

**Permissions.** Reception/Security: manage visits, flag. Hosts (self-service): pre-register, view own guests only. Auditor: read + export. Face/plate matching = biometric scope (audited, consent-gated). Unknown-person hits route to Security, not Reception.

**Responsive.** Stream→cards <1024px; check-in form becomes sheet; badge print action hidden on mobile (note "at reception").

**Accessibility.** Status icon+label; watchlist alerts announced immediately (polite region); forms: labels, error summaries, `autocomplete` hints; consent checkbox clearly separated (1.4x touch target).

**States.** Empty: "No visitors today." Watchlist hit: full red banner + alert routed (never silent). Overdue: amber "3 visits overdue" group with extend/check-out shortcuts.

---

### 5.9 Vehicles

**Purpose.** Vehicle intelligence: detection, class/color, LPR reads, whitelist/blacklist gate actions, dwell analytics (FR-103/104/105, Phase 2 — designed now, shipped then).

**Layout.** KPI row (Vehicles today · Plates read · Whitelist hits · Blacklist hits) → live plate feed (left — card per read: plate, confidence, class, color, gate, direction, snapshot) → vehicle journey panel (right — multi-camera path for selected vehicle) → whitelist/blacklist tabs → dwell chart bottom.

**Widgets**
- Plate cards: plate text (mono, large), region badge (IN/EU/US), confidence, class icon + color chip, gate, entry time, dwell timer, snapshot
- Watchlist match: red ring + "BLACKLIST — notify guard" action (auto-routed per config)
- Journey panel: timeline of camera handoffs (ReID, FR-104) with thumbnails, speeds between gates
- Whitelist/Blacklist: editable lists (plate, reason, expiry, scope), CSV import/export
- Gate status strip: gate open/closed, last auto-action, gate health (Phase 2 integration)

**Buttons & Actions**
- Plate card: "Add to whitelist/blacklist" (reason required) · "Open live" · "Copy plate" · "View journey"
- Gate: "Open gate" (manual, logged) · "Override" · "Test integration" (admin)
- "Export vehicle log" · "Search plates" (partial + wildcard, mono input)

**Filters.** Date · Plate (partial) · Vehicle type · Color · Gate · Status (matched/unknown/flagged) · Confidence ≥.

**Charts.** Vehicles by hour (bar), dwell distribution (histogram, 15-min buckets), gate throughput (small multiples), blacklist-hit counter (warning chip).

**User Flow.** Plate read → regex validated per region (AI-ARCHITECTURE §3.11) → whitelist: gate auto-opens (indicator "Auto-open — whitelist") · unknown: recorded + optional alert · blacklist: immediate High alert + guard dispatch → dwell > threshold → operational alert → journey review → export.

**Permissions.** Parking/Gate Admin: whitelist/blacklist manage (change audited). Operator: gate overrides. Auditor: export. LPR data = metadata class (no raw frames for non-operator roles).

**Responsive.** Plate cards 1-col on mobile, 2-3-col grid on desktop; journey panel → bottom sheet <1024px; plate input gets numeric keypad on mobile.

**Accessibility.** Plate text is text (never image-only); confidence meters text-labeled; blacklist matches announced + visible; keyboard access to all list actions; color chips paired with vehicle-class labels.

**States.** Empty: "No vehicles at this gate today." OCR low confidence: amber "Plate re-checked — 2 candidates" with both options (N-best, §3.11). Gate offline: red strip "Gate integration unreachable — manual mode".

---

### 5.10 Maps

**Purpose.** Spatial situational awareness: cameras, zones, live incidents, guard positions on a site map or floorplan (FR-203 visualization; MapLibre per ARCHITECTURE §22.1).

**Layout.** Full-bleed map canvas → left layers panel (collapsible) → right detail panel (contextual, slide-over) → bottom status strip (legend + zoom + coordinates).

**Widgets**
- Map layers: camera markers (status-colored dot + name on hover), zone polygons (translucent fill + type icon), incident pins (severity color, map-pulse on new), guard positions (live dot + name, Guard app heartbeat), mask zones (hatched, "never visible" affordance), tripwires (dashed)
- Floor selector (multi-floor sites: tab bar of levels)
- Search: address / camera / zone / incident
- Detail panel: selected object info — camera: health + "Open live"; zone: rule summary + event count; incident: severity + "Triage"; guard: task list + "Message"
- Basemap toggle: satellite / site plan / street (per site config)

**Buttons & Actions**
- Marker click → detail panel · incident pin → "Triage" (jumps to Alert Center focused)
- Draw mode toggle: "Add zone" (→ §5.11 with map as canvas), "Add camera" (placement wizard), "Measure" (ruler tool, area/distance)
- Layer toggles: Cameras · Zones · Incidents · Guards · Masks · Heat (spatial heatmap overlay, §5.13)
- Fit site · My location (guard view) · Print/screenshot map (auditor)

**Filters.** Layer toggles · Floor · Status (only offline cameras / only active incidents) · Time (incident history replay, optional).

**Charts.** None native — spatial heat overlay serves as the map's "chart"; legend is text+icon.

**User Flow.** CSO/Manager opens map → sees two pulsing pins → clicks → triage (§7.1) → dispatches guard → guard dot animates to location → incident resolves, pin dims. Admin: draw a new zone directly on the floorplan (§7.4).

**Permissions.** View: all roles (metadata only for Viewer). Raw-video popovers: Operator+. Draw/edit zones & cameras: Site Admin. Guard positions: Operator+ (privacy: guards opt out of live position beyond shift). Map config (basemaps, floors): Super Admin.

**Responsive.** Web: full map with panels. Tablet: same, larger touch targets. Mobile: map-first with floating action buttons; pinch zoom; bottom sheets.

**Accessibility.** Markers are buttons with `aria-label` ("Camera 12, Front Gate, online"); keyboard pan via arrow keys when map focused (skip-zoom for screen readers: "Enter map" pattern); zoom controls visible on screen (no gesture-only); colorblind-safe marker palette (shape+color, not color alone); reduced-motion: pins pulse via opacity only.

**States.** No incidents: "All clear — no active incidents on this site." Empty map (no cameras placed): onboarding CTA "Add your first camera". Offline site: map greys with "Site offline — data as of HH:MM".

---

### 5.11 Restricted Zones

**Purpose.** The zone & rule builder (FR-203) plus live zone state — the config surface where safety rules are drawn, tuned, tested, and monitored (intrusion, loitering, abandoned object, PPE matrix, capacity).

**Layout.** Split: **canvas** (live video or map with editor, dominant) | **left rule list** (zone cards with status) | **right config panel** (contextual editor, slides over).

**Widgets**
- Canvas editor: draw polygon (click-vertex), draw tripwire (line + direction arrows), drag handles, live preview of fill; mask-zone tool (hatched)
- Rule list: zone name · type icon (intrusion/loitering/abandoned/PPE/capacity/mask) · status toggle · active-events count · threshold summary
- Config panel: thresholds (dwell 30s–10min slider per FR-108; confidence 0.5–0.9 per FR-101), severity mapping, alert routing (channels + recipients from Notification Center), schedule (always / day-night / rain profile per FR-109), PPE matrix (checklist of 6 items per zone, FR-106 — presets: Welding Bay = 6/6, Office = helmet only), capacity limit + alert (FR-115), escalation thresholds (T1/T2)
- Test mode: "Simulate" injects a synthetic track to validate the rule end-to-end (no false fire on prod)
- Zone history: violations per zone (30d sparkline)

**Buttons & Actions**
- "New zone" (polygon / tripwire / mask) · "Draw" · "Edit" · "Duplicate" · "Delete" (confirm: zones with history require reason) · "Enable/Disable" · "Test" (simulate) · "Save & push" (config-svc → edge, per ARCHITECTURE §3.3 single-writer) · "Revert" (draft vs live badge)
- Rule card: quick mute · jump to Alert Center filtered to zone

**Filters.** Zone type · Status (enabled/disabled/draft) · Site · Floor · Has active events.

**Charts.** Violations by zone (bar, 30d), zone event timeline (stacked by type), capacity gauge (live occupancy vs limit, FR-114/115).

**User Flow (the builder flow, §7.4).** Admin → New Zone → draw on canvas (snap-to-grid + measurement readouts) → pick type → set thresholds (defaults pre-filled per type) → assign severity/routing → Test (simulate, green "rule passes" confirmation) → Save & push (versioned, edge converges) → zone appears on Maps + Live overlays instantly.

**Permissions.** Create/edit/delete: Site Admin (site-scoped). Draft save only: Operator (no push). View: all. Every config change versioned + audited (config-svc versioning, FR-203). Mask zones: Super Admin only + dual-approval (U9 highest bar).

**Responsive.** Desktop: canvas + panels. <1024px: panels become drawers (rule list top-left sheet, config bottom sheet). Mobile: "Zones" becomes a list with per-zone cards; drawing UI suggests tablet/desktop ("Draw on larger screen" with deep-link).

**Accessibility.** Polygon drawing keyboard-accessible (alt: numeric entry mode "enter vertices by coordinates") — required for motor-impaired admins; canvas labels ≥13px on scrim; status = icon+text; form controls labeled; focus management through panel transitions; reduced-motion: no draw-handle pulse.

**States.** Draft unsaved: "Unsaved changes — draft #3 (last saved 09:12)" banner. Test failure: red "Simulation failed — rule never triggered; check zone geometry". Push offline: "Saved locally — edge offline, will push on reconnect" (store-and-forward promise).

---

### 5.12 AI Analytics

**Purpose.** Exploratory operational intelligence: occupancy, crowd density, dwell, footfall, capacity utilization (FR-114/115, Phase 2) — where managers ask "why" and get charts, not clips.

**Layout.** Query bar (metric · dimension · breakdown) → chart grid (2-3 cards) → insight cards (auto-flagged patterns) → anomaly list → save/export bar.

**Widgets**
- Metric picker: occupancy (avg/peak), density level (low/med/high/critical), dwell time, footfall, capacity utilization, zone entries, PPE compliance (cross-module)
- Dimension picker: zone · floor · camera · day-of-week · hour · site
- Chart cards: time series (line/area), group comparison (bars), hourly heat grid (zone × hour), density gauge (FR-114 levels), dwell histogram
- Insight cards (auto): "Loading dock occupancy peaks 14:00–16:00, 22% above capacity limit on Fridays" with confidence + "view evidence" (operational events, not video, for non-operator roles)
- Anomaly list: statistical deviations (occupancy spike Tuesday 03:00) with one-tap "create alert rule" (→ §5.11) — closing the loop
- Forecast (Phase 3, PRD §11.8): dashed continuation + uncertainty band, labeled "AI forecast — not operational data"

**Buttons & Actions**
- "Compare periods" (toggle: vs previous period, delta chips) · "Add to dashboard" · "Schedule report" (→ §5.14) · "Export CSV/PNG" · "Save view" · "Share link" (role-gated) · "Create alert rule" (from anomaly) · "Reset query"

**Filters.** Time range · Site · Zone · Floor · Metric threshold sliders (e.g., dwell > 10 min).

**Charts.** Line/area series, grouped bars, hour×zone heat grid, radial gauges, histograms, small-multiples by floor. All charts share one tooltip grammar: value · unit · time · zone · compare-delta.

**User Flow.** Manager opens Analytics → picks metric (e.g., dwell) → dimensions (zone, weekday) → heat grid reveals Friday dock congestion → insight card confirms → "Create alert rule" → zone capacity rule created → pushed to edge (round-trip in ≤60s).

**Permissions.** Viewer/Auditor: read-only, no export for Viewer. Operator: export. Manager roles: full incl. alert-rule creation (audited). Forecast visibility: CSO+ (labeled beta).

**Responsive.** Chart cards 1-col on mobile; query bar becomes stacked filters; insight cards collapse to list; export → share sheet.

**Accessibility.** Every chart ships with "View as table" (proper data table); tooltips keyboard-reachable; color ramps colorblind-safe (perceptually uniform, e.g., viridis-style for heat grids); insight cards announce on arrival (polite); reduced-motion: no chart entry animations.

**States.** No data: "No analytics yet — occupancy metrics begin after 24h of calibration" (honest about calibration maps, §3.19). Low data: "Calibrating — precision improving (est. 2 days)". Forecast error: "Forecast unavailable — insufficient history".

---

### 5.13 Heatmaps

**Purpose.** Spatial movement and occupancy heatmaps (PRD §11.5 Could-Have, Phase 2): where people move, dwell, and bottleneck — for retail layouts, warehouse routing, hospital traffic.

**Layout.** Map/floorplan canvas with heat overlay → left: time scrubber (play/step, hour resolution) → right: zone totals panel + metric picker → bottom: intensity legend + controls.

**Widgets**
- Heat layer: smooth density ramp over floorplan; two modes — **Footfall** (movement intensity) and **Dwell** (occupancy time), **Occupancy** (average count per cell); opacity slider
- Time scrubber: play/pause animation of the day, hour-step, date picker, range selection
- Zone totals: per-zone sums with mini-bars, sorted
- Camera coverage overlay: translucent coverage cones so managers know blind spots (calibration honesty, §3.19 limitation)
- Intensity legend: numeric scale + color ramp (colorblind-safe), text labels ("0–5 people")

**Buttons & Actions**
- Play / step / pause · "Single frame" export PNG · "Export data CSV" · "Compare" (two dates side-by-side split view) · "Hotspot view" (top-3 cells outlined with counts) · "Screenshot for report" · Metric switcher

**Filters.** Date · Hour range · Zone subset · Floor · Metric · Day-of-week (weekday/weekend compare).

**Charts.** The heatmap itself + zone totals bars + count time-series for the selected cell (click cell → sparkline of that cell's occupancy) + correlation hint card ("Footfall peak 12–14h correlates with canteen").

**User Flow.** Ops Director → pick floor → pick date → scrub to 13:00 → sees bottleneck at checkout → clicks cell → occupancy curve → zone totals confirm → export → space plan changes. Warehouse: dwell heat reveals staging-area congestion → "Create alert rule" (density, §5.11) → prevention.

**Permissions.** Ops/Managers: full. Auditor: view + export (with data-provenance footer). Viewer: view only. Guard app: not exposed. Heat data = metadata class (no video), so access is broad by design (U9 upside).

**Responsive.** Desktop/tablet native; mobile: scrollable map + bottom sheet controls; scrubber shrinks to step buttons.

**Accessibility.** Heat color never sole encoding — each cell is hover/keyboard-focusable with numeric value; legend numeric; "View as table" per zone; reduced-motion: play animates in 2s steps without easing.

**States.** Calibrating: "Calibration in progress — cell values are estimates" chip. No data: "No movement recorded in this period — check camera coverage" with coverage toggle hint.

---

### 5.14 Reports

**Purpose.** Generate, schedule, and export evidence-grade documents (FR-117): incident dossiers, compliance scorecards, attendance, occupancy, zone violations — PDF/CSV/JSON with tamper-evident hashes.

**Layout.** Left: template gallery (cards with preview thumbnails) · right: generation form (period, scope, format, delivery) · bottom: recent exports (status list) + scheduled reports (accordion).

**Widgets**
- Template cards: Incident Dossier · PPE Compliance (per-zone %, FR-106) · Attendance Register · Occupancy Report · Zone Violations · Audit Log Extract · Camera Health (FR-116) · custom templates (user-saved)
- Form: period (presets + custom calendar), site/zone scope, format (PDF/CSV/JSON), sections checklist, schedule toggle (daily/weekly/monthly), delivery (download/email/webhook), language (v2)
- Recent exports: file · type · scope · generated by · size · hash chip (✓ SHA-256) · status (ready/failed/expired)
- Scheduled reports: name · cadence · next run · recipients · toggle · edit
- Template preview: live sample with placeholder data, page-count estimate

**Buttons & Actions**
- "Generate" (progress: analyzing → compiling → hashing) · "Download" · "Email to…" · "Share link" (TTL, role-gated) · "Schedule" · "Edit template" · "Duplicate" · "Delete" (dual-approval for evidence reports)
- Hash chip click → verification modal (recompute chain, ARCHITECTURE §6.2 hash chain)

**Filters.** Type · Status · Period · Site · Generated-by.

**Charts.** Embedded in reports (reuse §5.12 charts, print-rendered); preview thumbnails.

**User Flow.** Auditor → Incident Dossier → period (last quarter) → zones → PDF → Generate → hash appears → verify → download → file stored in audit archive (Object Lock path shown: "Archived to evidence vault"). Operator schedules weekly PPE compliance → email to Safety Manager every Monday 06:00.

**Permissions.** Generate/download: Auditor+, Operator (own scope). Schedule: Manager+. Delete: Super Admin (dual-approval, audit-logged). Hash verification: anyone with link (public trust, no auth needed — hash is the proof). Data scope enforced by site/zone claims (ABAC §12.3).

**Responsive.** Gallery→list on mobile; form becomes single-column; exports list paginated.

**Accessibility.** Generated PDFs are tagged/accessible (heading structure, alt text on charts — screen-reader friendly); form fully keyboard-able; hash text selectable (mono); status icon+label; progress announced (aria-live).

**States.** Generating: progress with stage names; Failed: reason + "Retry" (no silent failures — DLQ alerting parity, ARCHITECTURE §4.3); Expired: "Link expired — regenerate"; Empty: "No reports yet — start with Incident Dossier".

---

### 5.15 Settings

**Purpose.** Site and platform configuration: cameras, sites, retention, integrations, notification defaults, privacy defaults, branding, billing — the IT Admin and Super Admin's home (FR-202, FR-206, §15.4).

**Layout.** Settings nav (vertical grouped list) → content pane (forms) → action footer (Save / Discard / Version history). Groups: **Sites & Cameras · Zones & Rules (link to §5.11) · Data & Retention · Notifications (link to §5.18) · Integrations · Privacy & Compliance · Users (link §5.16/5.17) · Branding · Billing (Phase 2) · System (edge fleet, OTA, model versions)**.

**Widgets (per group, all forms)**
- Sites: site cards (name, region pin, edge devices, camera count, status) · Add Site wizard (name → region/residency → address → timezone → edge pairing QR)
- Cameras: table (name, IP/RTSP, model, group, health, analytics enabled) · Add Camera wizard (auto-discovery ONVIF → select → credentials → test stream → assign zone → enable modules) · batch add · config templates (FR-202) · "Apply template"
- Data & Retention: retention slider (7/30/90/365 days, per site), archive toggles, erasure request UI, storage usage bar (tiered cost preview)
- Integrations: cards per system (HRIS, Access Control, WhatsApp, Slack/Teams, webhooks) with connect/test/disable; webhook list (URL, secret, events subscribed, delivery status); "Test webhook"
- Privacy & Compliance: default mask policy, attendance mode (photos vs embeddings-only), consent templates, signage pack download (PRD §15.5), data residency display
- Notifications: default routing per severity (chips), quiet hours, escalation policy (→ §5.18 detail)
- System: edge fleet table (device, model version, uptime, store-and-forward queue depth, OTA status), model registry view (version, per-site threshold, rollback), release channel

**Buttons & Actions**
- Group-level: "Add …" · "Test" · "Save" (dirty-state tracking) · "Discard" · "View history" (config-svc versioning) · "Export config" (backup JSON)
- Camera: enable/disable analytics per module (PPE on, weapons off in canteen — per-zone module control), "Test stream", "Edit ONVIF credentials"
- Destructive: "Delete site" (dual-approval, full erasure manifest preview)

**Filters.** Settings search (global, ⌘K), category filter.

**Charts.** Storage usage bar (current vs tier), camera health donut, edge fleet queue-depth sparklines.

**User Flow (camera onboarding, §7.5).** Add Site → pair edge (QR) → Add Camera → auto-discovery finds 40 → select + assign template → test streams → analytics modules per zone → save → wall populates; config converges to edge (config-svc single-writer).

**Permissions.** Site Admin: site-scoped groups (no billing/system). Super Admin: everything + erasure + dual-approval. Auditor: read-only settings view + config history. Every save versioned + audited.

**Responsive.** Nav→top tabs on mobile; forms single-column; wizards full-screen.

**Accessibility.** All forms: labels, `autocomplete`, error summary at top + inline, required markers; contrast on all inputs; switch components have text labels; wizards announce step (aria-live); keyboard-only flows complete (no drag-drop required — alt "choose from list").

**States.** Unsaved: sticky "Unsaved changes" bar; Save conflict: "Config changed elsewhere — review diff" (merge view, 3-way); Test failure: red inline "Stream unreachable — check credentials/IP"; OTA: "Model v2.3 deploying to 12 edges (canary 5%)".

---

### 5.16 Roles

**Purpose.** Role definition and capability management (FR-204 RBAC) — what each role can see and do, with least-privilege defaults and presets.

**Layout.** Left: role cards (system + custom) · right: capability matrix editor (capability × permission toggles) · bottom: member count + assignment chip.

**Widgets**
- Role cards: name · type badge (System/Custom) · member count · summary chips (e.g., "Ack · Dispatch · Export") · description
- Capability editor: grouped checklist (Monitoring: live view, wall, playback / Response: ack, escalate, dispatch, dismiss / Evidence: export, dossier, hash verify / Admin: zones, cameras, roles, erasure / Data: biometric, raw video, audit) — each row: permission + scope hint + "why this matters" tooltip
- Scope picker: All sites / Selected sites / Site admin-owned
- Presets gallery: Super Admin · Site Admin · Operator · Auditor · Viewer · Guard · HR Manager · Reception — one-click apply, custom roles start from a preset
- Member assignment: searchable user picker, role membership list with last-assigned audit stamp

**Buttons & Actions**
- "New role" · "Duplicate" (from preset) · "Save & version" · "Apply preset" · "Assign users" · "Preview as this role" (role simulation, §5.17) · "Delete" (blocked while members exist — re-assign first)

**Filters.** Search roles · Type filter.

**Charts.** None — optional capability coverage mini-heat (roles × capabilities) for the admin's own audit.

**User Flow.** Admin → duplicate "Operator" → rename "Night Operator" → add "Dismiss without reason"? (warning: "Recommended: keep reason required — feeds model training") → scope to site B → assign 3 users → Save → users' shells change on next session (or live, with re-auth prompt).

**Permissions.** Manage roles: Super Admin only (site-scoped role editing = Site Admin within their sites). Every change audit-logged with before/after diff. System roles protected (can't be deleted; toggles locked where compliance requires — e.g., Auditor can never gain "delete").

**Responsive.** Cards→accordion on mobile; matrix becomes grouped toggles (no horizontal scroll).

**Accessibility.** Capability rows are labeled switches (not raw tables — VoiceOver-friendly); warnings inline text; scope picker radio-group; keyboard complete.

**States.** Protected: lock icon + "System role — limited editing (compliance)". In use: "3 users assigned" chip blocks delete. Unpublished draft: "Draft — not live until saved".

---

### 5.17 Permissions

**Purpose.** The data-class and policy layer (ABAC, ARCHITECTURE §12.3): who can touch raw video, biometrics, exports, deletes — plus grant simulation and the audit trail. The compliance officer's screen.

**Layout.** Top: policy cards (data class × action × condition) · middle: condition builder · bottom: grant matrix (users/roles × capabilities heat) + simulation pane + audit feed.

**Widgets**
- Policy cards: e.g., "Raw video — Operators only · site-scoped · logged", "Biometric — scope: biometric:* · audited · opt-in tenant", "Delete — Super Admin · dual-approval · erasure manifest"
- Condition builder: role · site · zone · data class (raw video / metadata / biometric) · time window · MFA required — AND/OR groups
- Grant matrix: rows = users/roles, cols = capabilities, cells = allowed/denied/conditional; colorblind-safe (✓ / ✕ / ◐)
- Simulation: "Check access" — pick a user + a resource (e.g., "Rajan → Camera 12 live feed") → verdict card "Allowed (operator, site B, logged access)" or "Denied — reason: role Viewer cannot access raw video"
- Audit feed: every grant change, simulation, and access event (who, when, what, verdict) — immutable, hash-chained (audit-svc)

**Buttons & Actions**
- "New policy" · "Edit" · "Revoke" (requires reason) · "Simulate" · "Export grants" (auditor) · "Request access" (user-side, → approval queue) · "Review outstanding requests"

**Filters.** User · Role · Policy type · Status · Date.

**Charts.** Grant matrix (the centerpiece), pending-request count chip, "access events by day" bar (compliance pulse).

**User Flow.** Auditor spot-check: simulate "Can Kavya export biometrics?" → denied with reason → verified. Admin: create "Night Operator" policy (dispatch only, no raw video after 00:00 without MFA) → attach → simulate → publish → audit stamped.

**Permissions.** Manage policies: Super Admin. View matrix: Auditor (read-only). Simulation: any admin (results not audited as access — labeled "simulation").

**Responsive.** Matrix → grouped list on mobile; simulation full-screen sheet.

**Accessibility.** Matrix readable as a list alternative ("List view" toggle); ✓/✕/◐ icons + text; simulation results in polite region; keyboard nav.

**States.** Pending approvals: "3 access requests awaiting review" banner; Policy conflict: "Two policies overlap — most specific wins (details)" with diff viewer; Revoked-live: "Policy change applied — 2 active sessions re-authenticating".

---

### 5.18 Notification Center

**Purpose.** Every notification the platform sent or will send — delivery status, routing configuration, muting, and testing (FR-118). Both an inbox and a rules console.

**Layout.** Tabs: **Inbox** (delivered) · **Routing** (configuration) · **Delivery health**. Inbox: list of notification cards with channel icons + receipts. Routing: rule list (severity → channels → recipients → schedule). Health: charts + per-channel status.

**Widgets**
- Notification cards: event title · channels sent (push/SMS/email/WA/webhook icons with ✓/✕ per channel) · timestamps · read state · link "Open alert"
- Delivery receipt expander: per-channel status (delivered / failed / retried / queued) + latency
- Routing rules: severity → channels (toggle grid) · recipients (groups) · schedule (24/7 vs business hours) · escalation (2nd wave after 2 min unacked, FR-118) · aggregation (group identical events, max 1/5 min) · mute/snooze state per zone
- Test sender: "Send test alert" (chooses severity + channels → live receipt)
- Aggregate groups: "12 similar events grouped" card with expand

**Buttons & Actions**
- Per notification: "Open alert" · "Mark read" · "Resend" (failed channels) · "Mute source" (with duration)
- Routing: "New rule" · "Edit" · "Enable/Disable" · "Duplicate" · "Test routing" (fires a test through all channels)
- "Mark all read" · "Configure quiet hours" · "Channel limits" (per-channel caps: max 3 SMS/min — flood control, ARCHITECTURE §4.4)

**Filters.** Channel · Severity · Date · Status (delivered/failed/queued) · Read state · Type.

**Charts.** Delivery success rate (line, 7d), volume by channel (bar), delivery latency (histogram, p50/p95 markers), mute coverage (donut: zones muted vs active — alert fatigue guard, PRD §13).

**User Flow.** Operator configures: Critical → push+SMS+phone tree; High → push+SMS; Medium → push; Low → digest. Test → receipt green. Incident fires → fan-out → receipts stream in → one SMS fails → retry → "Resend" → auditor exports monthly delivery SLA.

**Permissions.** Personal preferences: every user (their own phone/channels). Site-wide routing: Site Admin. Test sends: Operator (billed/rate-limited). Resend: Operator. Audit: all config + sends logged.

**Responsive.** Inbox→cards on mobile; routing rules→accordion; delivery health charts stack.

**Accessibility.** New-notification announcements (polite); channel status icon+text; receipts expandable via keyboard; contrast for failed states (red + ✕ icon); no auto-scroll without user intent.

**States.** Empty: "No notifications in this period." Failed channel: red "SMS failed ×2 — provider queue" with retry; Aggregated: "Grouped 12 → 1 delivered" chip.

---

### 5.19 AI Assistant

**Purpose.** Natural-language access to the platform's intelligence — ask questions, get answers with sources and evidence (Phase 3 generative narratives + control-plane copilot, PRD §17.3; surfaced early as read-only analyst).

**Layout.** Full-screen split: **chat** (left, 6 cols — prompt chips, conversation, citations) | **results canvas** (right, 6 cols — rendered answers: charts, tables, video clips) | input bar (prompt + time/site context chips).

**Widgets**
- Prompt chips (progressive disclosure): "Show today's zone violations" · "Summarize the 09:14 incident" · "Compare PPE compliance: this week vs last" · "Where do people linger most?"
- Conversation: user prompt → AI answer card with: verdict line, rendered widgets, **sources** (event IDs, cameras, clips with timestamps), confidence + model tag, disclaimers ("AI-generated summary — verify with evidence")
- Result canvas: any chart (§5.12 grammar), tables, evidence clips (permission-gated), dossier link
- Context chips: @site, last 24h, zone filters — parsed from prompt into removable chips (transparency: user sees what the AI "heard")
- Feedback row: 👍/👎 + "Why did it answer this way?" (explainability)
- Query audit indicator: "Query logged (#Q-2841)" — every prompt is audited (search-svc, §4.1)

**Buttons & Actions**
- Ask · Follow-up · "Open in Reports" (promotes answer to scheduled report) · "Open alert" (from cited events) · "Copy answer" · "Clear conversation" · "Explain sources"

**Filters.** Inline language parsing + explicit chips (time, site, zone, metric).

**Charts.** Any chart the platform renders can appear in the results canvas — one grammar, two surfaces.

**User Flow.** Meera asks "Show occupancy for the loading dock, Fridays, last month" → chips appear (Loading Dock, Fridays, Last month) → chart renders with hourly grid → insight line → "Create alert rule" (deep-link to §5.11) → done. Dr. Anand asks "Summarize today's fall incidents" → narrative + 2 clips + confidence + "Open dossiers" — evidence preserved, never paraphrased away.

**Permissions.** Capability-scoped exactly like the underlying data: Viewer gets metadata-only answers; raw-video citations require Operator+; biometric queries require biometric scope; **every query logged** (audit-svc). Guardrails: the assistant can never construct raw video, never bypass masks, never reveal other tenants; refusal UI ("I can't access that — data class requires Operator role") with the standard "Request access" path.

**Responsive.** Chat and results stack on mobile (results follow answer); chips wrap; input bar sticky.

**Accessibility.** Full keyboard chat (no autocomplete dependency); answers have heading structure; charts have data-table alternates; clips muted by default; announcements polite; reduced-motion: no typing animation.

**States.** No results: "I couldn't find events matching that — try broadening the date range" + suggested chips; Low confidence: "Low confidence answer — showing raw data instead" (honesty over fluency, U8); Offline: "Assistant unavailable offline — edge data is current, answers resume when connected."

---

## 6. Mobile, Tablet & Guard Apps

---

### 6.1 Mobile App (iOS / Android)

**Purpose.** The on-call commander's app: triage from anywhere, verify with evidence, dispatch, and glance at compliance — push-first, offline-resilient (PRD §11.7).

**Home tab — Alerts.** Bottom tab bar: Alerts (badge) · Live · Map · More. Alerts list = Event Cards (§2.6) with pull-to-refresh + auto-update via push.

**Alert Detail.** Push → deep link → detail with: evidence clip (pre-downloaded for offline, auto-play muted), severity, confidence, camera/zone, timeline scrub, actions bar: **Acknowledge · Escalate · Dispatch Guard · Dismiss** (reason sheet) · "Open live". Dispatch → guard picker (nearest, on-shift, with status) → confirm → progress chip ("Dispatched → Arrived → Resolved").

**Live.** Single camera, full-bleed; PTZ gestures (drag = pan, pinch = zoom, double-tap = preset); overlay toggles in a floating chip row; snapshot → share sheet.

**Map.** Site map with incidents + cameras (same design language as §5.10, guard positions on manager view); tap pin → bottom sheet → "Triage".

**More (role-aware).** People: Attendance today (HR: roster, exceptions, export share) · Employees (enroll via device camera — quality-guided capture, §7.3 mobile variant) · Visitors check-in (reception) · Reports (download recent) · Settings (profile, notifications prefs, quiet hours) · Account & security (biometric lock).

**Components & rules**
- Lock screen: Face ID/Touch ID at launch AND re-auth for PII screens (biometric scope)
- Haptics: severity-scaled (critical = 3-tap pattern, high = 2, medium = 1 — with "haptics off" in settings; muted by silent switch on iOS)
- Widgets (iOS/macOS): Alert count + top critical chip; tap → deep link
- Notifications: categorized by severity; critical bypasses focus modes only when user opted "always allow life-safety"
- Offline: cached queue of last 50 alerts + downloaded evidence; banner "Offline — buffered"; actions queue and sync (store-and-forward, ARCHITECTURE §4.3)

**User Flow.** Manager's phone buzzes (Critical — Fall, Ward 3) → tap → clip plays → Acknowledge (1) → Dispatch guard (2) → guard accepts → manager watches state chip → resolved (3 touches, same promise as desktop §7.1).

**Permissions.** Mirrors web exactly (same OPA policies — tokens carry same claims); biometric device unlock adds a device-level gate only, never replaces server RBAC.

**Responsive.** iPhone SE→Pro Max and Android small→large: single column; landscape = video + detail split; Dynamic Type to accessibility sizes (layout reflows, no truncation).

**Accessibility.** VoiceOver labels on every control; severity announced as prefix ("Critical: Fall detected, Ward 3"); contrast meets AA on Night Ops; reduced-motion honored; haptics optional; touch targets ≥44pt; captions when audio.

**States.** Empty: "All clear." Offline: amber banner. Deep-link miss: "Alert resolved or expired" with "View similar" path.

---

### 6.2 Tablet App (iPad / Android tablet)

**Purpose.** The small-site command wall: replace a monitor stack with one tablet — live wall, alert rail, spotlight, quick triage (FR-201 light).

**Layout.** Landscape-primary. Top status bar (site, clock, badge) → **video wall grid** (4/9/16 tiles) + **right alert rail** (Event Cards, swipe to ack) → tap tile = spotlight (full-screen with inspector, §5.5 components) → slide-back to wall. Optional side map panel (split view, site plan + incident pins).

**Widgets & gestures**
- Video tiles: same anatomy as §5.2; drag to reorder; two-finger swipe down = snapshot all
- Alert rail: cards swipeable (right = ack, left = snooze) — fastest tablet triage
- Spotlight: tap-to-zoom playback, timeline bottom, inspector right, "Dispatch" floating
- Map panel: picture-in-picture resize (drag corner), pins tap → sheet

**Buttons.** Layout switcher · Ack all · Dispatch (from alert) · PTZ (spotlight) · Snapshot · Mute zone · Fullscreen.

**User Flow (guard-room on iPad).** Wall running (16 cams) → alert rail pulses → swipe ack (0.5s) → tap to spotlight → clip auto-plays → Dispatch → guard on way → resume wall. This replaces 80% of a SOC seat for ≤16-camera sites.

**Permissions.** Operator role typical; **kiosk mode** (Site Admin): locks app to wall + rail, hides config, auto-relaunch on crash, passcode exit — for unattended wall tablets.

**Responsive.** iPad (portrait = 4 tiles + rail bottom-sheet; landscape = full wall), Android tablets same; Stage Manager/multi-window supported (wall + alerts as separate windows, iPadOS 16+).

**Accessibility.** VoiceOver: tiles are buttons with full labels; swipe gestures have on-screen alternatives ("swipe to ack" also = tap → ack button); keyboard (external) full nav; wall tiles announce event changes politely (configurable rate to avoid spam).

**States.** Kiosk crash → auto-relaunch + "Restored" toast; Wall offline → tiles show buffered frames + "Last synced HH:MM".

---

### 6.3 Security Guard App (SentinelGuard)

**Purpose.** The responder's tool: dispatch awareness, navigation, camera context, evidence capture, patrol checkpoints, and a one-gesture PANIC (FR-109/112/117 on the ground).

**Layout.** 4 tabs: **Today** (default) · **Incident** (active task) · **Patrol** · **Me**.

**Today**
- Task queue: dispatch cards (type · location · urgency · dispatched-by · ETA ring) — sorted by urgency + proximity
- Status chips: on-shift timer, battery, signal
- "Panic" button — persistent, every tab, top-right (hold 1.5s → countdown 1s → trigger; cancel by release)

**Incident (active task state machine)**
- Header: type + severity + timestamp + dispatcher
- **Accept → En Route → Arrived → Resolved** (primary bottom button morphs through states; state persisted to SOC in real time)
- Map: route to incident (walk/drive toggle), guard position, incident pin, nearest camera markers
- Camera context: "View nearest camera" → live clip (if permissions + coverage) — see it before you arrive
- Evidence capture: photo (auto geotag + timestamp + hash) · voice note (30s cap) · written note — all appended to incident dossier (FR-117)
- "Call backup" (phone tree) · "Request camera follow" (SOC locks cameras on location)

**Patrol**
- Route list: checkpoints (name · order · due window), progress ring, time budget
- At checkpoint: "Check in" (GPS-verified, radius 50m) → optional photo proof; missed checkpoint → alert to SOC + supervisor
- Patrol report: auto-summary (times, GPS trace, exceptions)

**Me**
- Profile, shift, availability toggle, language, notification prefs, "Safety instructions" (panic protocol), settings

**Design rules**
- **One thumb**: all primary actions bottom-third, ≥48pt targets, gloves mode (larger targets toggle)
- **Panic protocol**: hold→countdown→trigger: SOC high-priority alert + GPS stream + nearest cameras locked on + guard position shared with dispatch + supervisors notified — audited end-to-end
- Offline: tasks cached; check-ins queued; panic still fires (SMS relay at site, ARCHITECTURE §19.3)
- Haptics: arrival = 1 tap; new critical = 2 taps; panic confirmation = sustained pattern
- Battery honesty: live map defaults to 5s position refresh; "Low battery — offload to supervisor" prompt at 15%

**User Flow.** Dispatch lands (push + haptic) → Accept (1 tap) → nav → Arrive → assess (camera clip) → resolve with photo evidence → SOC sees Resolved → dossier auto-closes with chain-of-custody stamp.

**Permissions.** Guard role: read-only evidence (no export), task scoping (own site), location shared only while on shift (privacy toggle in Me), panic overrides quiet hours by design (life-safety, U10). Every action audited.

**Responsive.** Phones primary; tablets supported (map split view); gloves mode enlarges all touch targets + increases contrast.

**Accessibility.** VoiceOver for all actions; panic is also keyboard/switch-accessible (long-press alternative: triple-press side button wired via OS shortcuts); high-contrast outdoor theme (white-on-black, max brightness hint); captions on camera clips; reduced-motion removes ETA ring animation (static countdown text).

**States.** No tasks: "All clear — you're up to date" + optional "Run patrol early". Panic countdown cancelled: brief "Cancelled" toast. Dead zone (no signal): "GPS offline — check-in will use last known position + manual confirm".

---

## 7. End-to-End Flows

### 7.1 Canonical Alert Triage (3-click promise, PRD §6)
1. Detection confirms on edge (temporal layer, AI-ARCHITECTURE §4) → event → alert-svc grades severity.
2. **User sees**: top-bar badge (all screens), mobile push (native), wall tile ring (wall).
3. **Click 1** — open Alert Center (or deep link): card auto-focused, evidence preloaded.
4. **Click 2** — Acknowledge (card locks, enters operator's queue).
5. **Click 3** — Dispatch Guard (nearest on-shift, confirmation sheet pre-filled).
6. State transitions stream live (Acked → Dispatched → Arrived → Resolved); dismissals require reason (feeds eval-svc retraining).
7. Nightly digest: confirmation rate, false-positive trend, per-zone quality (PRD §9 SLO: ≤1 FA/5 cams/day surfaced as a visible KPI).

### 7.2 Incident Dossier (FR-117)
Alert → "Open dossier" → timeline (pre/post ±30s) → snapshots → detections w/ confidence + model version → camera/zone metadata → hash chain chip (✓ SHA-256) → export PDF/CSV/JSON → evidence vault path shown → auditor email with hash. All access logged.

### 7.3 Employee Enrollment
Employees → Enroll → consent capture (signed, versioned, privacy mode selected: photos vs embeddings-only, §15.3) → capture 3 frames with live quality guidance (pose straight, lighting, no occlusion — success meter) → liveness test → system match check (threshold per site) → success card with "test punch" → directory entry live. Failure paths teach, never block silently.

### 7.4 Zone Builder
Zones → New → draw polygon on live video (snap, measurement readout, undo) → type (intrusion/tripwire/loitering/abandoned/PPE/capacity/mask) → thresholds (defaults per type) → severity + routing (from Notification Center) → schedule → **Test (simulate)** → green → Save & push (versioned; edge converges) → live overlays update. Mask zones: Super Admin + dual-approval only.

### 7.5 Zero-Touch Onboarding (G4)
Settings → Add Site → region pin → Add Edge (QR pairing) → Add Cameras (ONVIF discovery, batch select, template) → test streams → analytics per zone → wall populates → 100-camera site ≤5 days. Every step has an inline "how-to" video + skip-for-now with recovery path.

### 7.6 First-Run Experience
- 5-step guided setup: Site profile → Connect edge → Add cameras → Enable modules (vertical presets: Warehouse/Hospital/Retail/Office/School/Construction) → Invite team (roles pre-assigned from presets).
- Vertical preset ships tuned defaults (PPE matrix, severity routing, retention 30d) — U6.
- First 24h: "Calibration mode" banner explains confidence tuning; first alerts are prefixed "Calibrating — verify with caution."

---

## 8. Accessibility Standard

**Baseline: WCAG 2.2 AA (web/PWA), Apple HIG Accessibility, Material Accessibility.** Exceptions only where approved and documented (none in v1).

| Area | Commitment |
|---|---|
| Contrast | Night Ops: text ≥4.5:1, large text ≥3:1, UI components ≥3:1; semantic colors tested (severity combos). Daylight theme verified. |
| Non-color encoding | Severity always icon + label; status always word + dot; charts get text/data-table alternates; heatmaps numeric on focus. |
| Keyboard | Every screen fully keyboard-operable; visible focus 3px accent; no keyboard traps; ⌘K search first-class. |
| Screen readers | Live regions for alerts (polite, rate-limited to avoid spam); all video tiles labeled; evidence described textually; modals focus-trapped with return-focus. |
| Motion | `prefers-reduced-motion`: pulses → static chips, gauge sweep → static, map pin pulse → opacity crossfade, no auto-advance. |
| Zoom & text | Dynamic Type to 310% (mobile/tablet) with reflow; desktop zoom 400% no data loss; landscape video priority on mobile. |
| Touch | ≥44pt (mobile), 48pt guard app, gloves mode; no gesture-only actions (alternatives exist). |
| Haptics | Severity-scaled; fully toggleable; respects OS silent switch; never required for understanding. |
| Voice & captions | Audio-enabled cameras: captions in live/playback; camera audio never auto-plays. |
| Cognitive | Consistent grammar (one alert card anatomy everywhere), no flashing content (≤3 beats), calm-urgency rule U10. |
| Testing | Automated axe-core in CI; quarterly manual audit with real assistive tech (NVDA/VoiceOver/TalkBack); operator onboarding includes accessibility tips. |

---

## 9. Cross-Screen Patterns

### 9.1 Privacy in the UI (U9, §15)
- Masked zones render as solid `#0B0B0E` + "Masked zone" label on *every* surface — even admin; there is no "unmask" toggle (pixels destroyed at edge).
- Raw-video access shows a persistent "LIVE — accessed by <user> · logged" watermark chip (transparency + deterrence).
- Biometric screens carry the lock icon + "Biometric data · encrypted · audited" footer.
- Attendance-only mode: silhouette placeholders instead of photos, embeddings badge.

### 9.2 Evidence Integrity in the UI (FR-117, P9)
- Every export shows the hash chip (✓ SHA-256) with "Verify" → recompute modal.
- Dossier footer: chain-of-custody line ("Clip #4841 → #4842 → #4843 · vault path · generated by report-svc v2.3").
- Tampered/expired evidence states are explicit, never silent.

### 9.3 Notification Grammar
One card anatomy everywhere (web rail, mobile push, tablet rail): **Severity prefix → Title → Camera · Zone → Age → Confidence**. Push previews: "Critical — Fall · Ward 3 · 12s ago". Aggregated groups show "12 similar → 1".

### 9.4 Empty-Site First Impressions
Before calibration data exists, dashboards show designed placeholders ("Occupancy calibrating — estimate by Thursday") instead of zeros — zeros mislead; calibration honesty builds trust (PRD §14).

### 9.5 Dark/Light Theming
Night Ops default; Daylight optional (no reports feature depends on theme); Paper for exports. OS-follow default, pin allowed, 300ms crossfade.

---

## 10. Design Acceptance Criteria

Every screen ships only when:

1. **5-second comprehension** — a new user can state what happened/where/severity/next-step from a screenshot (tested on 3 non-project users, pass ≥2/3).
2. **3-click response** — alert → actionable action reachable in ≤3 clicks on every surface (web, mobile, tablet, guard).
3. **No dead states** — every empty/error/offline/permission state offers the next true action.
4. **A11y gate** — axe-core 0 critical/0 serious; screen-reader smoke test passes per flow in §7; reduced-motion verified.
5. **Role-preview correctness** — every role's shell renders exactly the permitted surfaces (tested via role simulation).
6. **Privacy pass** — no raw-video pixel renders for masked zones or unauthorized roles in any layout, at any breakpoint.
7. **KPI proof** — each screen's north-star metric (§1.3) is visible without scrolling or interaction on first load.
8. **Offline resilience** — web/mobile/guard apps demonstrate buffered state + store-and-forward promise without error states.

---

*Document ends. Companion docs: `PRD-SyncCam-AI.md`, `ARCHITECTURE.md`, `AI-ARCHITECTURE.md`.*
