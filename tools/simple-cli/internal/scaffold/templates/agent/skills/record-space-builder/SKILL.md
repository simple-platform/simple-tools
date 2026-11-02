---
name: record-space-builder
description: Create or revise Simple record Spaces that compose platform record workflows with tabs, header actions, managed forms, and shared theming.
---

# Record Space Builder

## Purpose

Build app-owned Spaces for Simple record routes using the supported SDK, UI Kit, and host contracts, so the Space controls its composition while Simple retains record behavior, navigation, authorization, and persistence.

Slug: record-space-builder

## When To Use

Use this skill when creating or changing a Space selected by a record view, especially when the work includes host tabs, header actions, managed record UI, record-related Actions, or tenant theming. For a standalone Space with no record route, use the general Space APIs and consult this skill only for shared records or theme behavior.

## Workflow

1. Find the app, its `spaces/<name>` package, and the SCL records that declare the Space and bind it to a record view. Read the applicable `AGENTS.md` files. Resolve table and Space identifiers from app metadata and existing records; do not guess physical IDs.
2. Read the [public Space contracts](https://simple.dev/docs/features/spaces/#configure-a-record-space), [Custom Views](https://simple.dev/docs/features/custom-views/#record-spaces), and installed SDK/UI Kit declarations for the APIs being changed. Check negotiated host capabilities. Platform source may be newer than published customer packages; a sibling platform checkout is not required.
3. Keep the Space bound to the intended record view in SCL. A record `custom_view` names `type record`, the target table, and the app's Space record (commonly via `$app_record("<space-name>")`). The Space itself is declared in the app's Space records. Follow the app's existing pattern and CLI scaffold.
4. Bootstrap with `connect({ targetOrigin })` from `@simpleplatform/sdk/space`, using the app scaffold's host-origin plumbing. Keep one connection for the Space lifecycle. Do not add a `connectSpace()` alias or infer record context from a URL; use `simple.context` and the negotiated APIs.
5. In a record Space, resolve the route-owned handle with `await simple.records.current()`. Use its snapshot for display and `update()` / `submit()` for record writes. `current()` is unavailable in standalone context. For a related record, use `simple.records.open({ appId, tableName, recordId })` only when the workflow needs a second independently authorized record; `open()` works in record and standalone Spaces when `records` is negotiated. Opened handles submit independently and live until the Space connection ends; there is no handle `close()` API, and unmounting a form does not close the session.
6. Register host tabs with `simple.ui.tabs.set({ tabs, onChange })`. Use the returned `selectedTabId` for initial rendering, and update the same view state from `onChange`; call `simple.ui.tabs.select(id)` for Space-initiated navigation. The host owns the `?tab=` URL and browser history. Do not build a competing tab URL or remount the Space iframe when a tab changes. Use stable, lowercase URL-safe IDs, one optional `default: true`, and descriptive labels. The SDK enforces the tab count and metadata limits.
7. Prefer the managed UI when Simple should own record-field behavior and presentation:

   ```tsx
   import { RecordActivity, RecordForm } from '@simpleplatform/ui-kit/react'

   <RecordForm record={record} />
   { selectedTabId === 'activity' && <RecordActivity record={record} /> }
   ```

   `RecordForm` is the supported complete form, including platform field workflows. `RecordActivity` supplies the managed activity stream where the current UI Kit release exports it. Keep the Space responsible for surrounding layout. If a fully custom form is required, bind fields to the handle and submit through it; do not write the managed record through `simple.data.mutate()`. Mount Activity only when useful to avoid background queries. Keep RecordForm mounted across tab/selection changes when drafts must survive; a live record session does not preserve unblurred local field drafts from an unmounted form. UI Kit availability can differ between local and published versions, so check package exports before depending on a pre-release component.

8. Register Space-owned platform header buttons with `simple.ui.header.actions.set([...])`. Each action has a unique `id`, `label`, and iframe-local `onClick`; an async callback lets the host show its pending state. Call `record.submit()` explicitly when the button should save the route record, then report validation or completion with the platform toast API as appropriate. While the Space is active, `actions.set()` replaces the main Update/View Action area; `actions.set([])` intentionally leaves it empty. Delete, recovery, navigation, and other platform-owned controls remain host-owned, and teardown restores persisted View Actions. Keep registration in a lifecycle tied to the active connection and current callbacks.
9. Distinguish header buttons from server Actions. There is no public `RecordAction` component. `simple.ui.header.actions.set()` creates a host-rendered button whose callback stays in the Space. `simple.actions.run(name, input)` invokes an Action belonging to the Space's own app; it is not tied to the current record and cannot name another app. Compose them only when needed, for example by calling `simple.actions.run()` from a header button. Do not use an Action or raw data mutation to bypass `record.update()` / `record.submit()` for the managed record.
10. Use the shared UI Kit theme adapter (`@simpleplatform/ui-kit/theme.css`) and semantic `--simple-*` tokens. Do not add a second tenant theme setting or hard-coded `:root` / `.dark` brand palette. The host theme snapshot is versioned and capability-negotiated; check the SDK and host versions in use before relying on live theme updates or selected fonts. Keep local defaults usable when a host does not negotiate theme v1. Do not build a parallel theme bridge or accept arbitrary font URLs.
11. Keep Space permissions narrow in its SCL record. Add network, image, style, font, or document origins only for resources the Space itself needs. The platform theme owns its approved font-loading path; do not broaden permissions to make a local palette or arbitrary font URL work.
12. Validate the behavior at the layer changed: Space tests/build, SDK contract tests, or platform host tests. Use the existing `dev.simple.ui_fixtures` record Space as the end-to-end reference for tabs, actions, managed forms, Activity, and theme adapter usage. For iframe/host changes, verify the deployed SDK and host together because unit tests cannot prove the cross-origin handshake.

## Layout, Craftsmanship, and Responsiveness Standards

Record Spaces must follow the platform's visual standards and responsive rules:

1. **Never render double headers:**
   - The platform host shell already renders the breadcrumbs, record title, and primary Save/Submit actions (`simple.ui.header.actions.set`), as well as the host line tabs (`simple.ui.tabs.set`).
   - **Do NOT render a top-level page header (`<header>` with `h1` or large `h2`) or duplicate tab bar inside the Space iframe.** The Space iframe body must start immediately with the 2-column workspace content. Section headings belong *inside* their respective cards (`text-sm font-semibold` or `text-base font-semibold`), never spanning the page as an artificial secondary header.
2. **Do not restrict width:**
   - Do NOT clamp the outer Space container with artificial max-width constraints (such as `max-w-4xl`, `max-w-6xl`, or `max-width: 1400px; margin: 0 auto;`). Always use `width: 100%; max-width: 100%;` (`w-full max-w-none`) so the workspace takes full advantage of widescreen monitors.
3. **Borderless primary elements (Activity and primary forms):**
   - The primary element of the view must **not have an outer perimeter border or enclosing card panel**:
     - **Activity stream:** Render `<RecordActivity record={record} />` directly on the page inside `<section aria-label="Record activity" className="min-w-0">`. **Never wrap it in `.space-panel` or an artificial border box**.
     - **Primary form:** The dominant editing stage should breathe directly on the canvas without an outer border card enclosing it.
     - **Supporting context:** Bordered cards are reserved exclusively for supporting widgets (e.g. contextual KPI summary chips and the right-hand context inspector rail).
4. **Canonical 2-column split workspace pattern:**
   - **Left / Main Stage (~67%–70% width, flexible):**
     - **Record briefing callout card:** High-contrast subtle surface (`var(--simple-color-surface)` with `var(--simple-color-border-subtle)`), icon tag (`✦ Record Briefing` using info status tokens), concise summary of the issue or record state, and revision badge.
     - **High-density KPI summary cards:** 3-card horizontal grid (e.g. Account health, open follow-ups, review target).
     - **Tabbed work sections (driven by host tabs):**
       - *Overview:* Briefing, KPI chips, overview details, and related work action dock.
       - *Record details:* Form implementation switcher, primary `<RecordForm record={record} />`, secondary comparison session, and document staging example.
       - *Activity and files:* Full `<RecordActivity record={record} />` stream with note composer and field change audit timeline.
   - **Right / Context Inspector Rail (~30%–33% width, sticky):**
     - Stays sticky in the viewport (`lg:sticky lg:top-4`) as the user works through the main stage tabs.
     - **Record properties card:** Status pill, Priority badge (`var(--simple-color-status-warning-*)`), Assignee avatar and name, Stage, and Last Revision.
     - **Customer / Entity 360 card:** Account name, VIP tier badge (`var(--simple-color-status-success-*)`), contract value, SLA clock/target, primary cloud region, technical contact.
     - **Linked dependencies card:** Associated infrastructure / CMDB items with live status dots (e.g. `api-gateway-01` Operational, `postgres-primary` High I/O).
5. **No divider borders below section or card headers:**
   - Do NOT place divider lines (`border-b border-subtle` or `border-t border-subtle`) beneath section headings, card titles, or within card property groupings (e.g. within Record Properties, Customer 360, Form controls, or Linked Dependencies).
   - Use typography (`text-sm font-semibold`, `text-xs uppercase tracking-wider`) and natural vertical spacing (`gap-2.5` or `space-y-3`) to establish hierarchy, matching the platform record page.
6. **Consistent border colors across cards and controls:**
   - **Canonical border token:** All card perimeters, panel borders, container frames, inputs, selects, and button borders must strictly use `var(--simple-color-border-default)` (or Tailwind `border-border` / `border-[var(--simple-color-border-default)]`).
   - **Never mix border colors across adjacent cards:** Outer card frames across the screen must never have mismatched border strengths (e.g. never use `border-subtle` on one card and `border-default` on another). All cards share the same border stroke: `1px solid var(--simple-color-border-default)` (the standard `.space-panel` specification).
   - **Never invent non-existent tokens:** Do NOT use `var(--simple-color-border)`—it is undefined and causes browsers to fall back to black or currentColor. Always use `var(--simple-color-border-default)`.
   - **Subtle borders:** Reserve `var(--simple-color-border-subtle)` strictly for inner hairline dividers inside tables/lists (`divide-y divide-[var(--simple-color-border-subtle)]`).
   - **Strong borders:** Reserve `var(--simple-color-border-strong)` strictly for active or focused states on inputs and controls.
7. **Typography, font weights, and text colors matching the platform:**
   - **Antialiasing & rendering:** Apply platform font smoothing rules (`-webkit-font-smoothing: antialiased; -moz-osx-font-smoothing: grayscale; text-rendering: optimizeLegibility; font-feature-settings: 'rlig' 1, 'calt' 1;`).
   - **Canonical text color tokens:**
     - Primary text (titles, headings, values, field inputs): `var(--simple-color-text-primary)` (`hsl(240 10% 3.9%)` / `#09090b`).
     - Secondary text (subtitles, counts, table headers, descriptions, timestamps): `var(--simple-color-text-secondary)` (`hsl(240 3.8% 46.1%)` / `#71717a`). Never use non-existent `--simple-color-text-muted`.
     - Disabled text: `var(--simple-color-text-disabled)` (`hsl(240 3.8% 64%)`).
   - **Type Scale & Hierarchy:**
     - Page Title: `text-2xl font-semibold text-[var(--simple-color-text-primary)]` (`24px`, 600, `leading-8` / 32px).
     - Page Eyebrow: `text-xs text-[var(--simple-color-text-secondary)]` (`12px`, 400).
     - Section Headings: `text-sm font-semibold text-[var(--simple-color-text-primary)]` (`14px`, 600, `leading-5` / 20px).
     - Section Subtitles: `text-xs text-[var(--simple-color-text-secondary)]` (`12px`, 400).
     - Card Eyebrows: `text-xs font-semibold uppercase tracking-wider text-[var(--simple-color-text-secondary)]` (`11px`–`12px`, 600, tracking `0.05em`).
     - Field / Table Labels: `text-xs font-medium text-[var(--simple-color-text-secondary)]` (`12px`, 500).
     - Table Cell Data: `text-sm text-[var(--simple-color-text-primary)]` (`14px`, 400).
8. **Mobile Responsiveness Standard (390px Viewport):**
   - **Responsive 12-column collapse:** Use `grid grid-cols-1 lg:grid-cols-12 gap-6`. On desktop (`lg:`), the 2-column split is active (`lg:col-span-8` main stage, `lg:col-span-4` inspector rail). On mobile (`<lg`), the layout collapses into a single vertical column with the main work stage on top and the inspector rail directly below.
   - **Container padding:** Scales from `px-3 sm:px-6 py-3 sm:py-4`.
   - **Action docks & mode switchers:** Wrap cleanly with `flex flex-wrap items-center justify-between gap-2.5`.
   - **Safe metadata tag wrapping:** Always apply `shrink-0 whitespace-nowrap` to status pills and badges so words never break awkwardly across lines.

## Contract checks

- The current route-record API is `simple.records.current()`; do not revive `simple.record()` or the old `simple.ui.header.setActions()` spelling.
- `simple.ui.tabs.set()` and `simple.ui.header.actions.set()` are record-context APIs. Handle `unavailable` for older hosts or the wrong Space context instead of silently assuming negotiation.
- Simple owns the canonical record route, shell, header, authorization, and fallback body. A configured Space supplies the custom body below the platform shell; do not rebuild platform navigation or header controls inside the iframe.
- Host tabs are platform-rendered and URL-backed; their callback updates Space content but does not own route state.
- The route record remains server-authorized and behavior-aware. Use its handle for edits and submit. Secondary `records.open()` handles do not change `simple.context` or the active route.
- Use `simple.data.query()` / `mutate()` only for auxiliary app data, not writes to the managed route record. The current SDK also exposes `simple.documents.stage()` for customer-owned uploads that remain unattached; this is separate from RecordForm's private upload/attach flow. For either capability, use the public Space contracts and installed declarations.
- Other independently negotiated capabilities include `simple.tasks.create()` / `reply()` and `simple.ui.toast.show()`; check their current SDK contracts and host support instead of copying bridge code.
- Only use public UI Kit exports. Private runtime elements, form models, Activity queries, and bridge fields are not Space APIs.
- Do not bypass a host lifecycle/compatible-SDK guard. Custom forms must flush
  their edits through `update()` before `submit()`; use the managed form when possible.
- Theme is consumed passively; there is no `simple.theme` API. The semantic token contract is owned by Simple, the host sends a resolved snapshot, and the SDK applies it in the Space document. A missing negotiated capability should not break unrelated APIs.

## Useful references

- [Build Spaces](../build-spaces/SKILL.md) and [frontend design](../space-frontend-design/SKILL.md).
- [Spaces](https://simple.dev/docs/features/spaces/#configure-a-record-space), [Custom Views](https://simple.dev/docs/features/custom-views/#record-spaces), [tenant theme](https://simple.dev/docs/features/branding/theme/#create-the-theme-setting).
- In a platform checkout, use current SDK/UI Kit exports and the latest accepted
  Space decisions; older architecture sections retain superseded APIs.
- In `simple-apps`, `dev.simple.ui_fixtures/spaces/record-space-smoke` and its KB
  provide integration examples. Customer repositories use their own app source
  and installed package declarations.
