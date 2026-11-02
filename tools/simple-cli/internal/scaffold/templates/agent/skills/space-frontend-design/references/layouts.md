# Layout recipes

Choose a composition for the user's decision. These recipes describe visual
weight and information order; they do not prescribe a page full of the same
panels. Keep tenant tokens and familiar component semantics across compositions.

## Record Space (2-column split workspace)

The canonical layout for custom record page bodies bound to `/om/:app/:table/:record` (IT Ticketing, Incident Management, CRM Accounts/Leads, and CMDB Configuration Items). Pairs a dominant tabbed stage with a sticky context inspector rail.

- **Platform boundaries & Preventing double headers:** The platform host renders the record header (`<record-title>` and actions) and host line tabs (`RecordSpaceTabList`). The Space iframe must NOT render a second page title or a duplicate tab strip at the top. The Space body starts directly with the workspace content.
- **Full width without artificial restriction:** Do not clamp the outer space container to a narrow max-width (`max-width: 100%; width: 100%;` without `max-w-4xl` or `max-width: 1400px; margin: 0 auto;`).

```text
┌── Platform Host Record Shell (Outside iframe) ─────────────────────────────────────────────────────────────┐
│ Simple UI Fixtures > Ui Fixture Records > UI Kit Fixture Record                      [Refresh] [Submit] [⋮]│
│ [ Overview (0) ]  [ Record details (2) ]  [ Activity and files (1) ]                                       │
└───┬────────────────────────────────────────────────────────────────────────────────────────────────────────┘
    ▼ Inside Space iframe (Full width, no double headers):
┌── Main Stage (~70%, driven by Host Tabs) ───────────────────────────────────┬── Sticky Inspector Rail (~30%) ──┐
│ Active Tab Section (e.g. Details or Overview):                              │ Record Properties                │
│ ┌─ Section Card ──────────────────────────────────────────────────────────┐ │ - Status, Priority (P1), Assignee│
│ │ Header: Form controls & session diagnostics                             │ │ - Stage, Last Revision (rev-3)   │
│ │ Form Mode: [Managed form] [Custom controls]                             │ ├──────────────────────────────────┤
│ │ Managed <RecordForm record={record} /> or Custom Inputs                 │ │ Customer 360 Card                │
│ │ Secondary comparison session & document staging                         │ │ - Acme Global Corp [VIP]         │
│ └─────────────────────────────────────────────────────────────────────────┘ │ - Contract ARR & SLA Clock       │
│                                                                             ├──────────────────────────────────┤
│ When Overview tab selected:                                                 │ Linked CI Dependencies           │
│ - [✦ AI Record Briefing Callout]                                            │ - api-gateway-01 (Operational)   │
│ - High-density KPI Summary Cards: [Account Health] [Open Follow-ups]        │ - postgres-primary (High I/O)    │
│ - Related work action dock                                                  │ - auth-connector (Operational)   │
└─────────────────────────────────────────────────────────────────────────────┴──────────────────────────────────┘
```

- **Proportions:** Main work canvas ~67%–70% width (`grid-cols-1 lg:grid-cols-12`, with `lg:col-span-8`), sticky context rail ~30%–33% width (`lg:col-span-4 lg:sticky lg:top-4`).
- **Responsive collapse:**
  - Desktop (`>1024px`): Full 2-column split view with sticky right rail.
  - Tablet/Mobile (`<1024px`): 1-column stacked (main tabbed stage on top, context rail below).
- **Theme tokens:** 100% `--simple-color-*` tokens for all text, surfaces, borders, and badges. No hardcoded brand hex values.
- See sample HTML: [`assets/record-space-workspace.html`](../assets/record-space-workspace.html).

## Enterprise Workspaces Hub (Multi-domain Standalone Space)

The canonical layout for rich standalone suites that consolidate multiple enterprise domains into one Space using top tab and dropdown navigation (IT Service Desk, CRM Pipeline, CMDB Infrastructure, and Financial Operations).

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────┐
│ Header: [EW] Enterprise Workspaces · [● Operational] │ Workspace: [IT Service Desk ▾] [↻] [⤓]│
│ Line Tabs: [ IT Service Desk & Ticketing ]  [ CRM & Accounts ]  [ CMDB Fleet ]  [ Treasury ] │
├──────────────────────────────────────────────────────────────────────────────────────────────┤
│ Active Domain View (e.g. IT Service Desk & Ticketing):                                       │
│ ┌─ Queues (w-56) ─┬─ Ticket Stream (w-96) ───────────┬─ Detail & Action Inspector (flex-1) ──┐│
│ │ All Threads (4) │ Search: [Filter by Priority ▾]   │ Title: Defective banner print [#T1049] ││
│ │ P1 Urgent   (2) │ [ ] T-1049 · Acme Corp (P1) 14m  │ [Reply] [Internal Note] [Reassign]     ││
│ │ Waiting     (1) │ [ ] T-1048 · CloudScale (P1) 42m │ [✦ AI Triage Briefing: High impact...] ││
│ │ Resolved    (1) │ [ ] T-1047 · FinTech (P2) 2h     │ Timeline & Notes: John Doe / AI Agent  ││
│ │ ✉ Email / #Slack│ [ ] T-1046 · Delta (Norm) 1d     │ Properties: Assignee, Priority, ARR    ││
│ └─────────────────┴──────────────────────────────────┴────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Proportions:** Unified header with responsive dropdown `<select>` and line tabs, leading into full-height domain viewports.
- **No divider border between header title and tabs:** The top header title row and line tabs share the same uninterrupted surface. No horizontal border separates the title row from the tabs row; only the single continuous baseline border at the bottom of the line tabs separates the header block from the domain content (matching the platform record header).
- **Master-detail triage:** 3-column split for Service Desk (queue rail / stream / inspector); 4-metric strip + data table for CRM; telemetry banner + inventory table + dependency inspector for CMDB; and liquidity hero + accounts cards + ledger for Treasury.
- **Mobile master-detail drilldown (390px viewport standard):**
  - On screens `<md`, do NOT render ticket list and detail side by side.
  - List state: `w-full` ticket queue with search and priority pills. Detail is `hidden md:flex`.
  - Detail state: when tapped, list is `hidden md:flex` and detail is `flex flex-1 w-full`.
  - Top back navigation: `<button onClick={() => setMobileDetailOpen(false)}> [←] Back to active queue</button>` (`md:hidden`).
  - Tags use `shrink-0 whitespace-nowrap` to prevent awkward word wraps.

```text
┌── Mobile Viewport (~390px) ──────────────────────┐
│ [EW] Enterprise Workspaces                       │
│ [IT Service Desk ▾] [↻]                          │
│ [ IT Service Desk (4) ] [ CRM ] [ CMDB ] [ Treas │
├──────────────────────────────────────────────────┤
│ Mode A: Queue List View (full width 390px)       │
│ Active Queue (4) [All Priorities ▾]              │
│ [ Search tickets...                            ] │
│ ┌─ Ticket Item ────────────────────────────────┐ │
│ │ JD John Doe                          14m ago │ │
│ │ Defective banner print — refund request      │ │
│ │ [P1 Urgent] T-1049 · Acme Global Corp        │ │
│ └──────────────────────────────────────────────┘ │
│ ┌─ Ticket Item (Tapped -> switches to Mode B) ─┐ │
│ │ SC Sarah Chen                        42m ago │ │
│ │ Production API Gateway 502 Bad Gateway spike │ │
│ │ [P1 Urgent] T-1048 · CloudScale Labs         │ │
│ └──────────────────────────────────────────────┘ │
│                                                  │
│ Mode B: Ticket Detail View (full width 390px)    │
│ [←] Back to active queue                         │
│ T-1048 · Via Slack · CloudScale [Core Enterprise]│
│ Production API Gateway 502 Bad Gateway spike     │
│ [Reply]  [Internal Note]  [Reassign]             │
│ [✦ AI Ticket Briefing: P99 Edge Latency spike...]│
│ Timeline & Notes: Sarah Chen / Alex Rivera       │
│ Properties Card: Status, Priority, Assignee, ARR │
└──────────────────────────────────────────────────┘
```
- **Theme tokens:** 100% `--simple-color-*` tokens for all text, surfaces, borders, and badges.
- See sample HTML: [`assets/enterprise-workspace-hub.html`](../assets/enterprise-workspace-hub.html).

## Review desk

A queue and a selected record support sequential decisions. Give the selected
record enough width for its meaningful fields. The queue can be compact; the
current identity, status, and pending edits remain clear.

```text
Page title                                Main action
Selection / compact filters
┌──────── queue ────────┬──────── selected work ────────────────┐
│ Managed ListView      │ Identity, status, decision context   │
│ Relevant row details │ RecordForm                           │
│ Selected row         │ Related evidence / activity          │
└──────────────────────┴──────────────────────────────────────┘
```

The frame illustrates regions, not a requirement to border every region. Start
near a one-third queue/two-thirds detail split when the form needs space. Let
content determine the final widths. On a phone, show the queue or selected detail
as the active step with a clear Back action; stack both when the list is short.
Preserve unsaved edits across selection and screen changes. “Select a record” is
a useful initial state; a large empty card with no next step is not.

## Operations table

Comparison work needs a generous DataGrid and carefully chosen columns. Use a
compact title/action row and result toolbar. Put the first useful rows high in
the viewport. Use saved presets for alternate decisions: “Needs assignment” and
“Past target” are more helpful than an unexplained “View 1”.

Make identity and the discriminating fields readable. Secondary fields can use
the platform column controls or record detail. Avoid adding a metric strip unless
it changes how users choose rows. Horizontal overflow stays inside the grid;
phone presentations retain identity, status, and the main row action.

## Pipeline board

Stage-based work belongs in the managed Board. Lane summaries, record titles,
value, owner, and next step can make the board rich without surrounding every
lane with custom dashboard chrome. Put stage and ownership filters next to it.
Use a separate analytical view when users need comparison beyond the board.

Configure the enum-lane preset and real summary fields. If moving is enabled,
persist through a managed record handle and reject failed saves so the host can
restore the item. Keep a keyboard/click move alternative. Label read-only boards.

## Record workspace

The host provides the record heading, tabs, and main Save/action controls. Lead
inside the iframe with the context needed for the current task, then its managed
form or collection. Use host tabs for independently useful related work.

Standalone section tabs and their nested rows follow that same line treatment;
see [tab HTML](../assets/tabs.html) and [CSS](../assets/space-tabs.css). Use
`aria-selected` tabs with linked panels and keyboard selection. Table/Cards mode
buttons remain a separate `aria-pressed` control group.

```text
Host identity, status, Save changes
Host tabs: Overview | Details | Related work | Activity

Current task context / concise impact summary
┌──────────── dominant region ────────────┬── supporting rail ──┐
│ Managed form or related collection     │ Owner / target      │
│ Relevant details for this tab          │ Related records     │
│                                       │ Recent context      │
└────────────────────────────────────────┴─────────────────────┘
```

CRM account overview: account context and opportunities lead; contacts and
activity support. Opportunity detail: stage, value, close date, and next step
lead; editing and history have their own clear places. Incident response: impact,
severity, assigned owner, affected services, and the next response lead. CMDB:
service dependencies and affected incidents/changes lead. Controls: review
status, owner, evidence, and remediation lead. These are content selections,
not mandatory fields to fabricate.

Use `RecordForm`/`RecordActivity`, metadata labels/order, and authorized related
sources. Keep audit fields and technical identifiers in their appropriate
advanced context. On a phone, order identity/impact, next action, main work,
then supporting context; avoid a squeezed two-column form.

## Workspace overview

An overview should help someone choose where to work next. A priority collection
can lead beside a smaller context rail, or a dominant business insight can lead
above its affected work. Preserve enough row detail to act without repeatedly
opening records. Activity is helpful when it explains a current decision.

A bare list plus three empty-looking summaries is not automatically an overview.
Use the subject to establish visual character: incident ownership/impact, an
account's active work, or configuration dependencies. Use existing components
and real data. Quiet context and an occasional strong value provide contrast;
equal emphasis on everything does not.

## Deep Analytics & BI (PowerBI / GA4 Archetype)

The canonical layout for rich, exploratory business intelligence and analytics spaces modeled after PowerBI and Google Analytics 4. It combines multi-dimensional interactive slicers, real-time platform telemetry, multi-series trajectory comparison, conversion funnel drop-off analysis, and borderless attribution data tables.

```text
┌── Interactive Slicers Bar ────────────────────────────────────────────────────────────────────────┐
│ Timeframe: [7D] [30D] [90D] [YTD] │ Segment: [Enterprise ▾] │ ● 842 active sessions │ [⤓ Export]  │
├───────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Executive KPI Ribbon:                                                                             │
│ ┌─ Gross ARR ──────┐ ┌─ Active Users (WAU) ┐ ┌─ Avg Session Duration ┐ ┌─ Blended Conversion ─┐ │
│ │ $4.82M  (+14.8%) │ │ 64,280      (+8.2%) │ │ 4m 38s         (-12s)  │ │ 3.92%       (+0.45%) │ │
│ └──────────────────┘ └─────────────────────┘ └────────────────────────┘ └──────────────────────┘ │
├─────────────────────────────────────────────┬─────────────────────────────────────────────────────┤
│ Visual 1: Multi-Series Trajectory (Area)    │ Visual 2: Conversion Funnel Drop-off (Bar)          │
│ [Revenue ($k) | Sessions] Toggle            │ 5-Stage Drop-off Velocity                           │
│ - Current Period vs Previous Benchmark      │ [1. Impressions]  124k   (100%)                     │
│ - Shaded gradient trajectory with tooltips  │ [2. Sessions]      48k    (38.7%)                   │
│ - Recharts AreaChart with subtle grid       │ [3. Signups]       12.8k  (26.7%)                   │
│                                             │ [4. Activated]      5.2k  (40.6%)                   │
│                                             │ [5. Subscribed]     1.88k (36.2%)                   │
├─────────────────────────────────────────────┴─────────────────────────────────────────────────────┤
│ Regional Distribution: [North America: $2.41M (50%)] [EMEA: $1.35M (28%)] [APAC: $723k (15%)]...  │
├───────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Visual 3: Acquisition & Attribution Breakdown (BORDERLESS Primary Table)                          │
│ Filters: [All Channels] [Organic] [Direct SSO] [Partner] [Inbound]                                │
│ Channel Name         │ Sessions │ Conversion Rate │ Platform ARR │ Velocity / Health              │
│ Organic Search       │ 24,190   │ 3.84%           │ $1.42M       │ [● High Volume]                │
│ Direct Workspace SSO │ 18,440   │ 5.12%           │ $1.68M       │ [● Top Performer]              │
│ B2B Partner Referral │  9,820   │ 4.26%           │ $940k        │ [● Strategic]                  │
│ Targeted Inbound     │  7,350   │ 2.91%           │ $520k        │ [● Normal]                     │
│ Developer Docs       │  4,480   │ 2.15%           │ $260k        │ [● Growing]                    │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Interactive Slicers Bar:**
  - Placed immediately beneath the operational header.
  - Timeframe pills (`Last 7 Days`, `Last 30 Days`, `Last 90 Days`, `Year to Date`) for instant analytical slicing.
  - Customer tier / segment dropdown (`Enterprise`, `Mid-Market`, `Self-Serve`).
  - Real-time pulse indicator (`● 842 active sessions now` with emerald animated pulse) giving live situational awareness.
  - Fast report export action (`Export Report`).
- **Executive KPI Ribbon:**
  - 4 high-density metric chips displaying top-level business measures with period-over-period delta badges (`+14.8%`, `+8.2%`).
  - Numbers formatted with tabular figures (`tabular-nums font-mono`).
- **Visual 1 (AreaChart — Trajectory Comparison):**
  - Powered by Recharts (`ResponsiveContainer`, `AreaChart`, `Area`, `XAxis`, `YAxis`, `Tooltip`).
  - Compares the Current Analytical Period against the Benchmark Comparison Period.
  - Metric mode switcher allows swapping between `Revenue ($k)` and `Sessions` trajectories on the fly.
  - Uses theme brand tokens (`var(--simple-color-action-primary)`) and muted secondary tokens (`var(--simple-color-border-subtle)`).
- **Visual 2 (BarChart — Stage Conversion Funnel):**
  - Visualizes drop-off velocity across 5 key lifecycle milestones (Impressions → Sessions → Signups → Activated → Subscribed).
  - Explicit percentage conversion labels display stage-to-stage retention.
- **Borderless Primary Table Standard Applied:**
  - The Acquisition & Attribution breakdown table is the primary work surface and must **never be boxed inside a card perimeter or `.space-panel`**.
  - Outer border = `0px`. Inner vertical borders = `0px`.
  - Divider borders exist only horizontally: `border-b border-[var(--simple-color-border-default)]` for table header, and `divide-y divide-[var(--simple-color-border-subtle)]` for data rows.
- **Mobile Responsiveness (390px Viewport):**
  - Slicers bar scrolls horizontally without clipping (`overflow-x-auto [scrollbar-width:none]`).
  - KPI ribbon wraps from 4 columns to a 2x2 grid (`grid-cols-2 md:grid-cols-4`).
  - Visuals 1 and 2 stack vertically (`grid-cols-1 lg:grid-cols-12`).
  - Table wrapper enables smooth touch-scrolling (`overflow-x-auto -webkit-overflow-scrolling-touch`).

## Analytics dashboard

Choose the primary question before choosing charts. Use a compact shared filter
row, then the principal insight with supporting evidence. Metrics can be small
inline summaries when the chart carries the story. Give a detailed chart more
width than its supporting breakdown. Keep an obvious path to the managed records
behind each value; the table can sit below or in a named view depending on space.

```text
Title / reporting scope                        Refresh
Search · dates · owner/context · More filters   Apply

Principal question + scoped measure
┌──────────── main analysis ───────────────┬──── supporting ────┐
│ Chart chosen for the decision           │ Exception / mix    │
│ Readable axes, labels, units            │ Contextual values  │
└────────────────────────────────────────┴─────────────────────┘
Record results / saved views / current drilldown
Managed DataGrid
```

Use charts deliberately:

| Question                                | Useful starting point                        | Detail that matters                                                       |
| --------------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------- |
| Which category contributes most?        | Sorted horizontal bars                       | Zero baseline, aligned labels/values, stable category colors              |
| How do measures change over time?       | Line chart                                   | Real events, ordered dates, explicit interval and missing-data meaning    |
| How many events occur in each period?   | Columns or bars                              | Equal intervals for direct comparison, or clearly disclosed unequal bins  |
| What is the composition of a small set? | Stacked bar; donut if useful                 | Small readable set, totals and values, no near-identical unlabeled slices |
| Which items need attention?             | Managed table/list with status               | Identity, severity, owner, and an actionable next step                    |
| What depends on this service?           | Dependency map plus managed affected records | Named links, direction, readable node states, non-graph navigation        |

A bar chart and donut repeating the same category breakdown often add no new
insight. Choose complementary questions rather than filling chart slots. A
scheduled-close-date chart is not historical revenue or a forecast. A saved
asset review date is not a history of health. Keep forecasts distinct and sourced.

The same applied `RecordWhere` scopes server aggregates and managed records.
Pass values as GraphQL variables; table advanced rules update charts too. Totals
must not be derived from a paged table. Chart selection narrows the scope and has
a clear reset. Label units, reporting window, sample data, and current drilldown.
Expose readable values as keyboard controls or an accessible data table; SVG
alone is insufficient. Preserve category color identity across related charts
using tenant accent tokens; use status tokens for actual status. Respect contrast
and provide text values so color is never the only signal.

On a phone, keep search and the applied scope visible; disclose less-used filter
fields. Compact summaries so the main chart or actionable record appears in the
first viewport, then stack supporting analysis and records. Do not repeat desktop metric/card rows
at reduced size. Long chart descriptions belong in help/disclosures rather than
appearing beneath every graph. Empty/error states preserve scope and recovery.

### Charts with Recharts

The CLI Space scaffold includes the shared theme but no chart package, shadcn
chart helper, or `@` import alias. Add Recharts in the Space directory before
using this example:

```sh
npm install recharts
```

If the app already has a configured shadcn chart helper, it can wrap Recharts;
otherwise use Recharts directly and style it with tenant tokens:

```tsx
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

export function MetricBarChart({ data }: { data: Array<{ name: string, active: number, pending: number }> }) {
  return (
    <div aria-label="Active and pending records by period" className="h-[220px] w-full" role="img">
      <ResponsiveContainer height="100%" width="100%">
        <BarChart data={data}>
          <CartesianGrid stroke="var(--simple-color-border-subtle)" vertical={false} />
          <XAxis dataKey="name" tick={{ fill: 'var(--simple-color-text-secondary)' }} />
          <YAxis tick={{ fill: 'var(--simple-color-text-secondary)' }} />
          <Tooltip contentStyle={{ background: 'var(--simple-color-surface-raised)', borderColor: 'var(--simple-color-border-default)', color: 'var(--simple-color-text-primary)' }} />
          <Bar dataKey="active" fill="var(--simple-color-status-success)" radius={4} />
          <Bar dataKey="pending" fill="var(--simple-color-status-warning)" radius={4} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
```

Use shared chart components when they already exist in the app. Keep readable
labels and values available; color must not be the only signal.

## Configuration and settings

Settings benefit from readable groups, clear labels, brief help, and a stable
place for Save changes. Routine organization settings should feel calm and
well-organized. Use progressive disclosure for advanced configuration, retaining
search and direct access where the platform supports them. An explicit theme
editor may preview candidate tokens in an isolated frame; applying them is a
separate persisted action.

A generation/configuration studio can use a compact editor and generous preview.
On a phone, place inputs before the result. Keep the preview's own density useful
at its available width. Put JSON, token dumps, API diagnostics, and session details
in named disclosures. Candidate preview chrome is not duplicated host navigation.

## Calibration

Assess generated screens from realistic briefs and screenshots. A strong result
combines useful domain content, a visible primary task, differentiated information
hierarchy, and working managed components. The same theme should produce suitable
compositions for a CRM record, incident queue, analytics dashboard, and settings.

Check the connected journey as well as the first screen: collection → record →
related work → edit/recovery. A desktop screenshot alone cannot establish phone
quality, working save, or authorization. Use the skill's finish gate and record
what was actually exercised.
