---
name: space-frontend-design
description: Design and refine Simple Space frontends with intentional composition, tenant branding, managed record workflows, rich data views, and responsive visual QA. Use for standalone Spaces, Record Spaces, and Space UI fixtures.
---

# Simple Space frontend design

Design as a product designer who also understands the platform. Give the screen
an intentional composition and a recognizable business purpose. Use the tenant's
visual language with confidence: strong hierarchy, considered proportions,
well-edited content, and useful interactions. A polished enterprise workspace
can be expressive through its information and layout while staying on brand.

## Establish the direction before coding

Read the app KB, relevant source, installed SDK/UI Kit exports, and the Space's
registration. Inspect the current screen and a comparable platform screen when
available. Use [platform contracts](references/platform.md) for API details;
`record-space-builder` covers managed record composition when installed.

For a new page or substantial redesign, make a compact design plan:

- **Job:** who uses it, what decision they make, and the next action.
- **Content:** real fields, relationships, units, statuses, and realistic long
  values. Identify sample data; never invent a KPI, customer logo, or history.
- **Composition:** the dominant work region, supporting context, visual reading
  order, and what is visible in the first viewport on desktop and phone.
- **Character:** describe concrete choices, such as a compact incident queue
  beside a generous response workspace, or a broad dependency map with a narrow
  affected-services rail. “Clean, modern, enterprise” alone specifies nothing.
- **Behavior:** selection, navigation, persistence, recovery, and mobile order.

Sketch two plausible compositions for substantial new designs, using brief
notes or ASCII wireframes. Choose by the user's task and available components.
For a small refinement, keep the established composition and fix the actual gap.
Use [layout recipes](references/layouts.md) as starting points, not templates
that every page must resemble. Continue without an approval round when the
brief already supplies the needed direction.

## Two enterprise layout blueprints

Spaces exist within the customer's tenant branding theme. Do not invent arbitrary color palettes, hardcode brand hex values, or copy external mockup widgets (such as artificial keyboard shortcut badges). The skill drives **overall layout, structure, information hierarchy, and responsiveness**, while all visual styling is governed 100% by the tenant's Simple theme tokens (`var(--simple-color-*)`, `var(--simple-radius-*)`, `var(--simple-font-family-*)`).

### 1. The Record Space (2-column split workspace)
Bound to a specific record route (`/om/:app/:table/:record`) for CRM, IT service ticketing, incident resolution, CMDB asset management, and change review.
- **Platform boundaries & Preventing double headers:**
  The platform host owns breadcrumbs, record title, the primary Save/Submit actions (`simple.ui.header.actions.set`), and host line tabs (`simple.ui.tabs.set`).
  - **Never render a double header:** Do NOT render a top-level page header (`<header>` with `h1` or large `h2`) or a duplicate row of tab buttons inside the Space iframe. The iframe content must immediately start with the 2-column workspace layout. Form- or card-level headings belong *inside* the respective cards (`text-base font-semibold` or `text-sm font-semibold`), never spanning the page as an artificial secondary header.
  - **Do not restrict width:** Do not clamp the outer space container with artificial max-width constraints (e.g. `max-w-4xl`, `max-w-6xl`, or `max-width: 1400px; margin: 0 auto;`). Use `width: 100%; max-width: 100%;` (`w-full max-w-none`) so the enterprise layout takes full advantage of widescreen displays.
- **Left / Main stage (~67%–70% width, flexible):**
  - **Record briefing callout card:** High-contrast subtle surface (`var(--simple-color-surface)` with `var(--simple-color-border-subtle)`), icon tag (`✦ Record Briefing` using info status tokens), concise summary of the issue or record state, and revision badge.
  - **High-density KPI summary cards:** 3-card horizontal grid (e.g. Account health, open follow-ups, review target).
  - **Tabbed work sections (driven by host tabs):**
    - *Overview:* Briefing, KPI chips, overview details, and related work action dock.
    - *Record details:* Form implementation switcher, primary `<RecordForm record={record} />`, secondary comparison session, and document staging example.
    - *Activity and files:* Full `<RecordActivity record={record} />` stream with note composer and field change audit timeline.
- **Right / Context inspector rail (~30%–33% width, sticky):**
  - Stays sticky in the viewport as the user works through the main stage tabs.
  - **Record properties card:** Status pill, Priority badge (`var(--simple-color-status-warning)`), Assignee avatar and name, Stage, and Last Revision.
  - **Customer / Entity 360 card:** Account name, VIP tier badge (`var(--simple-color-status-success)`), contract value, SLA clock/target, primary cloud region, technical contact.
  - **Linked dependencies card:** Associated infrastructure / CMDB items with live status dots (e.g. `api-gateway-01` Operational, `postgres-primary` High I/O).
- Reference HTML: See [`assets/record-space-workspace.html`](assets/record-space-workspace.html).

### 2. The Standalone Space (enterprise workspace hub & dashboard)
For high-level business dashboards, multi-experience suites, system monitoring, and operational triage at `/space/:id`:
- **Consolidated Multi-Workspace Hub:**
  - Instead of scattering dozens of disconnected spaces across the platform sidebar, consolidate related enterprise domains into a unified Standalone Space with a top switcher.
  - Pair a persistent **Line Tab Bar** with a companion **Dropdown Switcher** (`<select aria-label="Switch Workspace">`), ensuring effortless one-click navigation across desktop and narrow screens.
- **Top operational header matching Platform Record Header:**
  Match the platform's native record header styling pixel-for-pixel:
  - **Container & Seamless Surface:** `header class="flex-none bg-[var(--simple-color-surface)]"` containing the title/actions row (`px-4 pt-4 pb-3 sm:px-6`) and the tablist (`px-4 sm:px-6`).
  - **No divider border between header and tabs:** The top header title row and line tabs share a single continuous surface. Do NOT put a `border-t` on the tablist or a `border-b` on the title row. There must be NO border separating the title from the tabs (matching native platform Record Spaces).
  - **Icon Box:** `size-11 shrink-0 bg-[var(--simple-color-surface-sunken)] rounded-[var(--simple-radius-md)] border border-[var(--simple-color-border-subtle)] flex items-center justify-center` with an icon (`size-5 text-[var(--simple-color-text-primary)]`, e.g. `LayoutGrid`). Never render custom bright-blue initials badges.
  - **Eyebrow:** Small muted category/app text above the title: `text-xs text-[var(--simple-color-text-secondary)] font-normal truncate` (e.g. `Simple UI Fixtures`).
  - **Main Title:** `text-2xl font-semibold break-words text-[var(--simple-color-text-primary)] tracking-tight` (`fontSize: 24px, fontWeight: 600, lineHeight: 32px`). Avoid long paragraph subtitles beneath the title; the eyebrow + confident 24px title creates the platform's signature clean, elegant balance.
  - **Status Pill:** Placed cleanly beside the title (`StatusBadge label="Operational" tone="success"`).
  - **Action dock & controls:** Action controls (`<select>` workspace switcher, `↻ Refresh` button) styled with standard platform control dimensions: `h-8` (32px height), `px-3 text-xs font-medium rounded-[var(--simple-radius-md)] border border-[var(--simple-color-border-default)]`.
  - **Tabs baseline border:** The line tab bar sits directly below the title row with a single continuous baseline border (`border-b border-[var(--simple-color-border-default)]`) that cleanly demarcates the header block from the page content.
- **Five core enterprise domains:**
  1. *IT Service Desk & Ticketing (Plain/Linear archetype):* Master-detail triage inbox with queue category sidebar, live ticket stream, SLA adherence badges, AI triage briefing card, timeline conversation stream, and properties inspector rail.
  2. *CRM & Account Pipeline:* Pipeline ARR and win-rate hero metrics, deal stage tabs, rep filters, and high-density deals table with tabular values, health badges, and scheduled actions.
  3. *CMDB Fleet & Infrastructure:* Multi-region telemetry banner, latency and alert counters, CI inventory table with type filters, and interactive dependency/telemetry inspector.
  4. *Financial Operations & Treasury (Mercury archetype):* Liquidity balance hero with SVG sparkline area, quick money movement pills (`+ Move Money`, `⇄ Transfer`), accounts breakdown, and high-density transaction ledger.
  5. *Deep Analytics & BI (PowerBI / GA4 archetype):* Enterprise business intelligence workspace featuring interactive slicers (timeframe pills, tier filters, live active user pulse), 4-metric executive KPI ribbon, multi-series trajectory comparison area charts (Current Period vs. Benchmark) with metric mode toggles, 5-stage conversion funnel drop-off visualization, regional distribution telemetry, and a borderless acquisition channel attribution table.
- Reference HTML: See [`assets/enterprise-workspace-hub.html`](assets/enterprise-workspace-hub.html) and [`assets/standalone-dashboard.html`](assets/standalone-dashboard.html).

## Micro-layout & craftsmanship standards

1. **Information density with breathing room:**
   - Avoid bloated cards with 40px padding. Use compact `p-3` to `p-4` inside cards, `px-3 py-2` on table rows, and `py-1 px-2.5` on filter pills.
   - Text scale: `text-sm` (14px) for primary data and table cells, `text-xs` (12px) for secondary metadata and timestamps. Reserve `text-xl` or `text-2xl font-semibold` for page titles and hero metrics.
2. **Tabular numbers:**
   - Always use tabular numerals (`font-variant-numeric: tabular-nums` or `font-mono`) for currency, timestamps, percentages, counters, and IDs so figures align vertically.
3. **Status dots & pill badges (single status token, borderless layout & sleek padding):**
   - **Single base status tokens:** The platform theme contract defines 4 single base status tokens: `--simple-color-status-info`, `--simple-color-status-success`, `--simple-color-status-warning`, and `--simple-color-status-danger` (there are NO separate `-background` or `-foreground` tokens in the theme contract). Status backgrounds derive automatically via `color-mix(in srgb, var(--simple-color-status-*) 12%, transparent)`, while text and icons use the base token directly `var(--simple-color-status-*)`.
   - Combine a colored status dot (`size-1.5` or `size-2 rounded-full inline-block mr-1.5`) with a concise label inside a subtle pill (`px-2.5 py-0.5 rounded-full text-xs font-medium`).
   - **Padding for badges and enum options:** Keep vertical (y) padding tight and horizontal (x) padding generous (`px-2.5 py-0.5` / `padding: 0.125rem 0.625rem`). Maintain consistent padding across status badges, pills, chips, and enum options so elements share uniform capsule proportions without bulbous or boxy vertical stretching.
   - **Enum options:** Enum options follow the same capsule layout (`rounded-full px-2.5 py-0.5`) with both the text and icon matching the same resolved status color, on top of the 12% `color-mix` background.
   - **No borders on badges, status pills, or tags:** Do NOT add borders (`border`, `border-color`, or `1px solid ...`) to badges, status pills, tags, chips, or enum options. For an elegant, uncluttered appearance, these small elements should rely strictly on their tinted/translucent background, foreground text, and icon or status dot. Outlines/borders on small badges add visual noise and make the UI feel heavy.
4. **Subtle surface contrast & borders:**
   - Keep borders crisp and quiet (`1px solid var(--simple-color-border-default)` or `var(--simple-color-border-subtle)`) on structural containers, cards, and input controls. Do NOT put borders on badges, pills, or enum options.
   - Use soft surface contrast: canvas background (`var(--simple-color-canvas)`), card panels (`var(--simple-color-surface)`), and subtle hover fills (`var(--simple-color-surface-hover)`). Do not add artificial keyboard shortcut badges to buttons or inputs.
5. **Borderless primary elements standard:**
   - **No outer perimeter border on primary elements:** The primary element of the page or view—such as the main Table, DataGrid, ListView, Activity Stream (`RecordActivity`), or primary Record Form—should **never have an outer card border or perimeter stroke surrounding it** (e.g. avoid enclosing them in `rounded-lg border bg-surface p-3.5` or `.space-panel`).
   - **Tables & DataGrids:** Primary data tables must sit directly on the page canvas/surface without an enclosing card box or outer perimeter border. The table header has a clean bottom border (`border-b border-[var(--simple-color-border-default)]`), and rows separate with subtle horizontal dividers (`divide-y divide-[var(--simple-color-border-subtle)]`). The outer wrapper handles only responsive horizontal scrolling (`overflow-x-auto [scrollbar-width:none] -webkit-overflow-scrolling-touch`) without an outer border stroke.
   - **Activity Streams:** Render `RecordActivity` directly on the page (`<section aria-label="Record activity" className="min-w-0">`) rather than wrapping it in an artificial card panel (`.space-panel`).
   - **Borders are reserved for supporting/contextual elements:** Metric KPI cards, summary chips, and secondary inspector rails use standard card styling (`.space-panel` or `border border-[var(--simple-color-border-default)] bg-[var(--simple-color-surface)]`), ensuring consistent border colors across the screen, but the dominant work stage and primary elements must breathe freely without nested boxes.
6. **No divider borders below section or card headers:**
   - **Do not place divider lines beneath section titles:** Sections, cards, and charts establish structure through typography (size, font-weight like `text-xs sm:text-sm font-semibold`, tracking, and subtle muted subtitles) and whitespace (`mb-2` or `gap-2`), NOT horizontal rules.
   - Avoid `border-b` / `border-bottom` immediately below section titles or card headers (e.g. no border divider lines directly beneath "Conversion Funnel Drop-off", "Performance Trajectory vs. Benchmark", "Properties", "Record Properties", or "Customer 360").
   - Horizontal borders are reserved exclusively for:
     1. Table header baseline (`thead border-b`) and table row dividers (`tbody divide-y`).
     2. Line tab bar continuous baseline (`tablist border-b`).
     3. Dedicated split-pane toolbars (e.g. dividing a search input bar from a scrollable ticket list).
7. **Consistent border colors across cards and controls:**
   - **Canonical border token:** All card perimeters, panel borders, container frames, inputs, selects, and button borders must strictly use `var(--simple-color-border-default)` (or Tailwind `border-border` / `border-[var(--simple-color-border-default)]`).
   - **Never mix border colors across adjacent cards:** Outer card frames across the screen must never have mismatched border strengths (e.g. never use `border-subtle` on one card and `border-default` on another). All cards share the same border stroke: `1px solid var(--simple-color-border-default)` (the standard `.space-panel` specification).
   - **Never invent non-existent tokens:** Do NOT use `var(--simple-color-border)`—it is undefined and causes browsers to fall back to black or currentColor. Always use `var(--simple-color-border-default)`.
   - **Subtle borders:** Reserve `var(--simple-color-border-subtle)` strictly for inner hairline dividers inside tables/lists (`divide-y divide-[var(--simple-color-border-subtle)]`) and chart grid lines (`stroke="var(--simple-color-border-subtle)"`).
   - **Strong borders:** Reserve `var(--simple-color-border-strong)` strictly for active or focused states on inputs and controls.
8. **Typography, font weights, and text colors matching the platform:**
   - **Antialiasing & rendering:** All Spaces must apply the platform font smoothing rules (`-webkit-font-smoothing: antialiased; -moz-osx-font-smoothing: grayscale; text-rendering: optimizeLegibility; font-feature-settings: 'rlig' 1, 'calt' 1;`).
   - **Canonical text color tokens:**
     - Primary text (titles, headings, values, field inputs): `var(--simple-color-text-primary)` (`hsl(240 10% 3.9%)` / `#09090b`).
     - Secondary text (subtitles, counts, table headers, descriptions, timestamps): `var(--simple-color-text-secondary)` (`hsl(240 3.8% 46.1%)` / `#71717a`). Never use non-existent `--simple-color-text-muted`.
     - Disabled text: `var(--simple-color-text-disabled)` (`hsl(240 3.8% 64%)`).
   - **Type Scale & Hierarchy:**
     - Page Title: `text-2xl font-semibold text-[var(--simple-color-text-primary)]` (`24px`, 600, `leading-8` / 32px).
     - Page Eyebrow / Breadcrumb: `text-xs text-[var(--simple-color-text-secondary)]` (`12px`, 400).
     - Section Headings: `text-sm font-semibold text-[var(--simple-color-text-primary)]` (`14px`, 600, `leading-5` / 20px).
     - Section Subtitles: `text-xs text-[var(--simple-color-text-secondary)]` (`12px`, 400).
     - Card Eyebrows: `text-xs font-semibold uppercase tracking-wider text-[var(--simple-color-text-secondary)]` (`11px`–`12px`, 600, tracking `0.05em`).
     - Body / Descriptive Text: `text-sm text-[var(--simple-color-text-secondary)] leading-normal` (`14px`, 400).
     - KPI Metric Values: `text-2xl font-semibold text-[var(--simple-color-text-primary)]` (`24px`, 600).
     - Field / Table Labels: `text-xs font-medium text-[var(--simple-color-text-secondary)]` (`12px`, 500).
     - Table Cell Data: `text-sm text-[var(--simple-color-text-primary)]` (`14px`, 400).
   - **Line Tab Strip (`RecordSpaceTabList` / `space-tabs`):**
     - Container: `flex h-10 w-full min-w-0 items-center justify-start border-b border-[var(--simple-color-border-default)] bg-transparent px-4 py-0 sm:px-6 gap-0.5 md:gap-2`.
     - Triggers: `h-[var(--simple-control-height-md)] shrink-0 inline-flex items-center justify-center gap-1.5 rounded-none border-b-2 bg-transparent px-2.5 pt-1.5 pb-0.5 text-sm font-medium leading-5 shadow-none transition-colors`.
     - Inactive Tab: `border-transparent text-[var(--simple-color-text-secondary)] hover:text-[var(--simple-color-text-primary)]`.
     - Active Tab: `border-[var(--simple-color-action-primary)] text-[var(--simple-color-text-primary)]`. Selected text is primary foreground text, with a 2px blue underline highlight tightly bounded to the trigger's `px-2.5` padding.
     - Badge: `rounded-full bg-[var(--simple-color-surface-sunken)] px-1 py-0.5 text-2xs leading-none text-[var(--simple-color-text-secondary)] tabular-nums font-medium`.
     - Icon: `size-3.5` (14px).

## Compose a working screen

**Make one region lead.** The record being resolved, the comparison being made,
or the chart answering the main question should carry the most visual weight.
Give it useful width and detail. Supporting summaries, controls, and activity
should help it. Reserve the strongest emphasis for that task and its main action.

**Build richness from the domain.** A CRM workspace can show account context,
open opportunities, next steps, and recent activity. Incident response needs
impact, ownership, affected services, and related changes. CMDB needs dependencies
and service exposure. Select the relationships and views that improve the user's
decision; distribute secondary work onto named pages/tabs rather than squeezing
all features into an overview. Preserve existing working actions and access paths.

**Use type as hierarchy.** Keep tenant fonts. Give identity, section headings,
values, labels, and metadata distinct roles through the existing size, weight,
line-height, and color tokens. A typical starting point is a 2xl/semibold title,
lg section headings, sm body/controls, and xs secondary metadata. A central
measure may deserve a larger value; ordinary labels do not. Use tabular numerals
for quantities; reserve monospace for code and technical identifiers. Readability
and hierarchy matter more than mechanically repeating these sizes. Keep explanatory prose to a readable measure; data views can be wide.

**Shape the page through proportion and rhythm.** Align headings, toolbars, and
content edges. Group closely related elements tightly; leave more space between
independent tasks. Use the platform spacing scale, normally 4/6 page insets and
4/6 panel padding. Check combined margin and padding at section boundaries.
Keep frequently used filters compact and near their results; place advanced
rules in a disclosure. Essential work should appear before long explanations.

**Give surfaces a purpose.** Use canvas for the page, surface for a work region,
sunken for supporting context, and overlay for floating layers. A card is useful
when it groups independently understandable content. Adjoining sections, quiet
rails, lists, and whitespace can create hierarchy without another frame. Use
md control radii and lg panel radii from tokens; badges can be pills. Shadows
belong mainly to floating layers. Keep one clear radius and elevation hierarchy.

**Keep section tabs consistent with platform contracts.** Match the platform's line tab specification (`RecordSpaceTabList`) exactly:
- **Tablist container:** `role="tablist"` with `display: flex; width: 100%; border-bottom: 1px solid var(--simple-color-border-default); background: transparent; padding: 0 1rem; gap: 0.125rem; md:gap-2;`.
- **Tab trigger:** `role="tab"`, `h-[var(--simple-control-height-md)]` (40px / 2.5rem), `px-2.5 pt-1.5 pb-0.5` (top 6px, left/right 10px, bottom 2px), `gap-1.5` (6px), `text-sm font-medium` (14px, 500).
- **Inactive state:** `border-b-2 border-transparent text-[var(--simple-color-text-secondary)] bg-transparent hover:text-[var(--simple-color-text-primary)]`.
- **Active state:** `border-b-2 border-[var(--simple-color-action-primary)] text-[var(--simple-color-text-primary)] bg-transparent`. Selected text is primary text color (`var(--simple-color-text-primary)`), NOT blue; the underline highlight is `2px solid var(--simple-color-action-primary)` brand blue; and the size of the blue highlight is tightly bounded by the 10px horizontal padding (`px-2.5`).
- **Badge:** `rounded-full bg-[var(--simple-color-surface-sunken)] px-1 py-0.5 text-[10px] leading-none text-[var(--simple-color-text-secondary)] tabular-nums`.
- **Icon:** Lucide icon with `size-3.5` (14px).
Reserve filled segmented controls for mode switches such as Table/Cards. Record Spaces declare [host tabs](https://simple.dev/docs/features/spaces/#add-tabs-to-a-record-space) on the platform shell; standalone sections use `.space-tabs` from [`assets/space-tabs.css`](assets/space-tabs.css). Keep draft forms mounted across section changes. Actual route navigation uses links.

**Make color informative.** Import Tailwind and `@simpleplatform/ui-kit/theme.css`
once. Use semantic `--simple-*` tokens or their utility aliases for surfaces,
text, actions, states, and charts. Let brand accents give the page character;
use status colors for actual status and readable foreground/background pairs.
The SDK supplies the tenant theme, fonts, and mode. Ordinary Spaces do not add a
palette, font loader, or theme fetcher. Tenant branding is compatible with varied
page compositions; it does not require every page to have the same silhouette.

**Edit the words and details.** Use specific business nouns, sentence case, and
verbs that say what happens: “Save changes”, “Assign owner”, “Review change”.
Keep action names consistent through pending state and feedback. Make empty and
error states explain the next step. Use installed Lucide icons to clarify actions
or entities, with consistent size/stroke. Use motion to explain a user's action;
respect reduced motion. Every visible label, divider, icon, or animation should
have a job. Keep API/session diagnostics in named disclosures and identify sample
data or preview controls clearly. Decorative gradients, oversized heroes, repeated eyebrow labels,
and identical KPI cards are poor substitutes for a considered composition.

## Build with the platform

- Use `RecordForm` for editing and `RecordActivity` for record history/notes/files
  when exported. Configure labels, field order, and presentation through
  [field metadata](https://simple.dev/docs/features/table-configuration/field-configuration/#core-capabilities)
  and supported Record Behaviors. Preserve validation, values, and denied states.
- Use managed `ListView`, `DataGrid`, and `Board` with typed table/preset sources
  when available. Choose columns, tile details, and default views for the decision.
  Collections have no outer frame; the Space owns any enclosing border, radius,
  or surface. Keep the component's themed header, row dividers, and lane boundaries.
  Check installed exports and negotiated capabilities; never invent UI Kit imports
  or copy its private runtime. [Platform contracts](references/platform.md) lists
  the supported APIs and recovery boundaries.
- Use host tabs, header actions, icons, and SDK navigation where supported. The
  host owns the Record Space heading, tabs, route, permissions, and record session.
  Standalone Spaces use local task controls without duplicating the host shell.
  Keep one SDK connection per iframe lifecycle.
- Before creating a local control, inspect the installed UI Kit and shared
  component variants and interaction states; reuse a fit and add only missing
  behavior.
- Prefer the organization's shared UI package for local primitives, then existing
  app shadcn/Radix components, then accessible native controls. Shared organization
  packages can compose UI Kit components and reusable presentational patterns;
  consuming apps own business state, Actions, and authorization. Follow
  [organization component packages](https://simple.dev/docs/features/spaces/#organization-component-packages).
- For dashboards, choose charts by the question, with readable values, units,
  dates, and keyboard drilldowns. Use the Recharts setup in `references/layouts.md` when analytics charts are needed.
  The base CLI scaffold has no chart package, shadcn chart helper, or `@` alias;
  use a configured helper only when the app already provides it. Style every chart
  with Simple semantic tokens (`--simple-color-*`). Share applied
  filters with the managed records and use authorized server aggregates rather
  than totals from a paged table. Distinguish current state, historical events,
  and forecasts. Use the [analytics recipe](references/layouts.md#analytics-dashboard)
  for chart choices and Recharts configuration.

The [CSS foundation](assets/space-layout.css) supplies optional layout mechanics;
the [HTML example](assets/workspace.html) illustrates managed-component placement.
Adapt them to the chosen composition. Neither is a finished design or a substitute
for platform queries/forms.

## Mobile responsiveness & adaptive master-detail standard

Every Space and Record Space must be responsive and intentionally designed to work on small mobile screens (e.g. 390px iPhone viewport) as well as wide desktops:

1. **Master-Detail adaptive mobile drilldown:**
   - In master-detail layouts (such as IT Service Desk, queue triage, and issue trackers), **never render both master list and detail pane side-by-side on mobile screens (`<md` / `<768px`)**. Splitting a 390px viewport into two 190px slivers crushes metadata tags, causes single words to break awkwardly (`T-\n1048`, `Via\nSlack`), and renders both panes unreadable.
   - **Default mobile view:** Render the queue stream at 100% full width (`w-full md:w-80 lg:w-96 flex flex-col`). The detail pane is hidden (`hidden md:flex`).
   - **Tapping an item:** Set state `mobileDetailOpen = true`. The list hides on mobile (`hidden md:flex`), and the detail pane takes 100% full width (`flex flex-1`).
   - **Mobile back button:** At the top of the detail pane, render an explicit, touch-friendly back navigation control visible only on mobile (`md:hidden`):
     ```tsx
     <button
       type="button"
       onClick={() => setMobileDetailOpen(false)}
       className="md:hidden inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--simple-color-action-primary)] hover:underline self-start cursor-pointer py-1"
       aria-label="Back to ticket queue"
     >
       <ArrowLeft className="size-3.5" />
       <span>Back to active queue</span>
     </button>
     ```
   - When the user taps the back button, `setMobileDetailOpen(false)` smoothly restores the full-width list view.
   - On desktop (`md:`), both panes display side-by-side as a continuous master-detail workspace without the mobile back button.

2. **Safe metadata tag & pill wrapping:**
   - Metadata pills, badges, and breadcrumb chips (e.g. `T-1048`, `Via Slack`, `Acme Corp`, VIP badges) must always have `shrink-0 whitespace-nowrap` on individual items.
   - The container must use `flex flex-wrap items-center gap-1.5 sm:gap-2` so items wrap as whole pills rather than breaking words across lines.

3. **Horizontal scroll containers for data tables:**
   - Always wrap DataGrids and tables in an explicit horizontal scroll container:
     ```html
     <div className="overflow-x-auto [scrollbar-width:none] -webkit-overflow-scrolling-touch rounded-[var(--simple-radius-md)] border border-[var(--simple-color-border-subtle)]">
       <table className="w-full min-w-[540px] text-left text-xs">
     ```
   - Enforce a minimum width (`min-w-[540px]` or `min-w-[640px]`) so columns preserve readable proportions while enabling smooth horizontal swiping on touch devices.

4. **Responsive KPI & metric grids:**
   - Use adaptive grid columns: `grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3`.
   - On 390px mobile screens, KPI cards stack in 1 clean column with full breathing room. On tablet they form 2 columns; on desktop they span 4.
   - Metric numerals use `truncate tabular-nums` to guarantee numbers never wrap onto multiple lines.

5. **Touch-friendly horizontal tab & filter strips:**
   - Workspace tabs and domain filter pill bars must allow smooth horizontal scrolling:
     `overflow-x-auto [scrollbar-width:none] shrink-0 whitespace-nowrap`
   - Individual tab triggers and filter buttons must have `shrink-0 whitespace-nowrap`.

6. **Record Space responsive 12-column collapse:**
   - Record spaces must use `grid grid-cols-1 lg:grid-cols-12 gap-6`:
     - On desktop (`lg:`): `lg:col-span-8` main work stage, `lg:col-span-4` sticky context inspector rail.
     - On mobile (`<lg`): Single-column stack, with main stage forms and tabs on top, followed by the inspector card below.
     - Container padding scales gracefully: `px-3 sm:px-6 py-3 sm:py-4`.

## Finish with visual critique

Run affected tests/typechecks/builds and `git diff --check`. Open the integrated
Space; inspect handshake, console/network, and managed component readiness.
Exercise the main action, selection, record navigation/save, collection filters,
and recovery at the affected layer. Inspect desktop, tablet, phone, zoom, theme
modes, and an iframe-scoped alternate brand where available.

Review screenshots at normal scale, including expanded controls and empty/error
states. Check the following before calling the design finished:

1. **Hierarchy:** the primary task is immediately recognizable and visually leads;
   headings, values, controls, and metadata do not all look equally important.
2. **Composition:** the first viewport contains useful work; broad empty areas,
   excessive filter chrome, repeated frames, or redundant charts do not bury it.
3. **Craft:** spacing, alignment, type, surfaces, focus, and numeric formatting
   feel considered together. Content remains readable with long values and zoom.
4. **Domain:** replacing the title with “Dashboard” would not erase all evidence
   of the actual workflow. Content and composition have a reason to be there.
5. **Continuity:** connected screens use the same visual language and vocabulary,
   with compositions suited to each job. States and mobile layouts retain it.

If the screen passes functional checks but still looks flat or interchangeable,
revise the hierarchy or composition and inspect again. Record the chosen visual
contract in the existing app KB; use it for subsequent screens. Report the actual
checks and remaining limits. Skill validation alone does not establish quality.

[Research notes](references/research.md) contain the exact GitHub sources,
revision pins, and the design guidance adopted or rejected for Simple.
