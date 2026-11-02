# Current Simple frontend contracts

Verified against the local SDK/UI Kit and fixture sources on 2026-10-26. Package
releases and host capabilities can differ; verify the installed version.

## Ownership and supported APIs

| Capability        | Contract and boundary                                                                                                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Bootstrap         | `connect({ targetOrigin })` from `@simpleplatform/sdk/space`; one promise per iframe; exact trusted host origin from scaffold/referrer                                                   |
| Context           | `simple.context.kind` is `standalone` or `record`; read application/table/record identity from context                                                                                   |
| Primary record    | `await simple.records.current()` in record context; `snapshot()`, `update(values)`, `submit()`; check both returned `ok` and thrown errors; there is no public snapshot subscription API |
| Related record    | `simple.records.open({ appId, tableName, recordId })`; independently authorized; opening does not change host context; no public close API                                               |
| Forms             | `<RecordForm record={handle} />`; platform owns fields, relations, validation, documents, and behaviors                                                                                  |
| Activity          | `<RecordActivity record={handle} />`; mount when relevant; platform owns reads and note/file workflows                                                                                   |
| Host tabs         | `await simple.ui.tabs.set({ tabs, onChange })`; initialize from returned `selectedTabId`; `simple.ui.tabs.select(id)`; host owns URL/history; record context only                        |
| Header actions    | `simple.ui.header.actions.set([{ id, label, icon, type, onClick }])`; local callbacks; record context only; async pending; explicitly submit record when saving                          |
| Server Actions    | `simple.actions.run(name, input)`; own app only; independently negotiated; does not replace record session writes                                                                        |
| Navigation        | `simple.navigation.open({ path, target: 'same-tab'                                                                                                                                       | 'new-tab' })`; verified host-relative `/om/<app>/<table>/<record>`path; no`window.parent` navigation |
| Feedback          | `simple.ui.toast.show({ title, description, variant })`; variants `default` or `destructive`                                                                                             |
| Collections       | `<ListView>`, `<DataGrid>`, `<Board>` from `@simpleplatform/ui-kit/react`; `client`, `source`, typed `preset`; host owns authorized reads, pagination, fields, filters, rendering        |
| Collection open   | `onItemOpen(reference)` supplies `appId`, `tableName`, `recordId`; navigate or deliberately open an independent record session                                                           |
| Collection filter | `RecordWhere` with canonical `_eq`, `_in`, `_and`/`_or` arrays and relationship fields; pair controlled `filter` with `onFilterChange`; initialize from preset                           |
| Board move        | `onMoveRequest({ item, laneFieldName, from, to })`; enum lane; await managed record update and submit, reject failed saves so the platform rolls back                                    |
| Status            | `<StatusBadge label="Ready for review" tone="info" />`; tones `neutral`, `info`, `success`, `warning`, `danger`                                                                          |
| Theme             | SDK applies negotiated host snapshot to `--simple-*`; UI Kit CSS maps tokens to Tailwind/shadcn aliases; no public `simple.theme` API                                                    |

Query table metadata to resolve a physical `tableId` by application ID and table
name. Never guess an ID from examples. Saved preset sets use envelope version 1,
filters use native `RecordWhere`, and tile layouts use version 1. Validate saved presets with
`validateViewPresetSet` / `validateRecordViewPreset`. Presets configure fields,
sorting, filters, and enum lanes; they do not accept record arrays or arbitrary
per-tile CSS. Current tile presentations are `standard`, `media`, `message`;
older presentation names remain in some docs. Inbox and standalone condition
editor components are platform-only. Inspect the public contracts rather than
copying a private custom element, runtime, or renderer.

## Styling and shadcn

The local Space UI Kit source exports `RecordForm`, `RecordActivity`, `ListView`,
`DataGrid`, `Board`, and `StatusBadge`. Older published packages expose only a
subset; verify installed declarations and host negotiation before importing. Its stylesheet bundles default Geist fonts and defines
fallback tokens. Host snapshots resolve tenant fonts/mode/branding. The host's
shadcn/Radix components are not implicitly public Space imports.

Record Space tabs use a transparent, horizontally scrolling line strip with a
2px primary-action underline, square edges, sm/medium text, and md control height.
The host implementation is `simple/apps/platform_web/components/record-page/record-space-tab-list.tsx`.
Use host tab APIs for record sections. For standalone sections, style the
organization's accessible tab primitive to match; the UI Kit does not currently
export `Tabs`. The skill's tab HTML/CSS illustrates a native fallback.

| Purpose        | Semantic token / utility alias                                                                                  |
| -------------- | --------------------------------------------------------------------------------------------------------------- |
| Page           | `--simple-color-canvas` / `bg-background`                                                                       |
| Work region    | `--simple-color-surface` or `surface-raised` / `bg-surface` or `bg-card`                                        |
| Secondary      | `--simple-color-surface-sunken` / `bg-muted`                                                                    |
| Text           | `--simple-color-text-primary`, `text-secondary` / `text-foreground`, `text-muted-foreground`                    |
| Border         | `--simple-color-border-default`, `border-subtle` / `border-border`, `border-border-subtle`                      |
| Primary action | `--simple-color-action-primary`, `action-primary-hover`, `action-primary-active`, `action-primary-foreground`   |
| Focus          | `--simple-color-focus-ring` / `ring-ring`                                                                       |
| Field          | `--simple-color-field-background`, `field-text`, `field-border`, `field-placeholder`, `field-border-invalid`    |
| Shape          | `--simple-radius-md`, `lg`, `xl`, `round` / `rounded-md`, `lg`, `xl`, `full`                                    |
| Layout         | `--simple-space-*`, `--simple-content-width-*`, `--simple-control-height-*`                                     |
| Typography     | `--simple-font-family-sans`, `mono`, `--simple-font-size-*`, `--simple-line-height-*`, `--simple-font-weight-*` |

Use selected foreground/background pairs for statuses. Charts use accent tokens;
status color should express status, never decoration. Host fonts follow an
approved loading path; ordinary Spaces do not need new font CSP permissions.

## Where to verify in a platform checkout

- `simple-sdks/sdks/space-ui/src/react/index.ts`, `src/contracts/`, `theme.css`.
- `simple-sdks/sdks/ts/src/space/{core,record,ui,tabs,navigation,theme}.ts`.
- `simple-sdks/architecture_history/space-sdk.md` (latest decisions).
- `simple/architecture_history/{platform-theme-token-contract,record-space-tabs,record-form,space-views}.md`.
- `simple-apps/apps/dev.simple.ui_fixtures/spaces/` and its `docs/kb/`.

In a customer repo, use installed package declarations, bundled context, and
platform documentation instead. Missing capability is a visible unavailable
state, not an invitation to reproduce the host implementation.
