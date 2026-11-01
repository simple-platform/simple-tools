---
name: build-spaces
description: Guide to building custom React-based Spaces in Simple.
---

# Build Spaces Skill

## 1. Concepts

- **Space:** A custom React application embedded within the Simple platform. Spaces provide complete UI freedom when Custom Views aren't flexible enough.
- **Routing:** Use local routing only for Space-owned pages. Record tabs, record URLs, and browser history belong to the host; do not add a competing router for them.
- **Styling:** Follow the [Space frontend design skill](../space-frontend-design/SKILL.md) for layout, tenant tokens, accessibility, responsive behavior, and browser QA; keep host and SDK contracts authoritative.

## 2. Directory Structure

Spaces reside in the `spaces/` directory of an app under the configured workspace root:

```
apps/<app-id>/spaces/<space-name>/
├── package.json      # Dependencies (React, Vite)
├── vite.config.ts    # Vite bundler config
├── index.html        # Entry HTML
├── tests/
│   └── App.test.tsx  # Unit test for App component
└── src/
    ├── styles/
    │   └── theme.css # Theme CSS variables
    ├── App.tsx       # Main React component
    └── main.tsx      # React DOM entry point
```

## 3. Scaffolding a Space

Use the Simple CLI to generate a new Space in this workspace:

```bash
simple new space <app-id> <space-name> <display-name>
```

Example: `simple new space com.acme.crm customer-portal "Customer Portal"`

## 4. API Communication (Secure RPC)

Spaces operate in an isolated, secure iframe served from `assets.simple.dev` with a strict Content Security Policy (CSP) that blocks external requests by default. Spaces _do not_ possess the parent application's authentication cookies.

Therefore, Space developers **MUST NOT** use raw `fetch()` to query the Simple Backend. Use the published `@simpleplatform/sdk` Space API instead. Its browser adapter establishes the secure MessageChannel with the parent frame and the host authorizes every request.

The scaffold imports `connect()` directly from the published SDK. Prefer the
embedding page's referrer, and use `VITE_SIMPLE_HOST_ORIGIN` only as trusted
configuration when the browser omits the referrer. Never guess an origin from
the Space URL or accept one from user input.

```tsx
import { connect } from '@simpleplatform/sdk/space'

function resolveHostOrigin(): string {
  const candidate = document.referrer || import.meta.env.VITE_SIMPLE_HOST_ORIGIN
  if (!candidate)
    throw new Error('Open this Space from the platform or configure a trusted host origin.')

  const host = new URL(candidate)
  if (host.protocol !== 'http:' && host.protocol !== 'https:')
    throw new Error('The trusted host origin must use HTTP(S).')

  return host.origin
}

const spaceConnection = Promise.resolve().then(() =>
  connect({ targetOrigin: resolveHostOrigin() }),
)

spaceConnection.catch(() => {
  const root = document.getElementById('space-root')
  if (!root)
    return
  const alert = document.createElement('p')
  alert.setAttribute('role', 'alert')
  alert.textContent = 'Unable to connect to Simple. Open this Space from the platform or check its trusted host configuration.'
  root.replaceChildren(alert)
})

const simple = await spaceConnection
```

Create one connection promise per Space and reuse it. Never load an SDK
connection alongside a copied bridge; they compete for the iframe connection.
Render connection failures visibly instead of leaving a blank Space.

### Host-provided context

Every embedded Space receives explicit context from its host at connection time. Do not infer page, table, or record details from the URL or DOM.

```ts
switch (simple.context.kind) {
  case 'standalone':
    break
  case 'record':
    console.log(simple.context.applicationId, simple.context.tableName, simple.context.recordId)
    break
}
```

`simple.context.kind` is currently either `standalone` or `record`. List context is not available yet. There is no inferred or unknown context state. An invalid `targetOrigin` or missing/malformed host context makes `connect()` reject with a structured SDK error.

### Fetching Data via GraphQL

Use `simple.data.query()` and `simple.data.mutate()` from the connected Space client.

Query the installed schema for exact qualified roots; alias results, select only
needed fields, and bound lists. For example, after verifying a `customer` table:

```typescript
const { customers } = await simple.data.query<{
  customers: Array<{ id: string; name: string }>
}>(`query Customers {
  customers: com_acme_crm__customers(limit: 25, order_by: { id: asc }) { id name }
}`)
```

Render query failure separately from an empty successful result and provide a
retry path. Guard against stale results after unmount/selection changes. For a
standard browse workflow prefer a managed collection when the installed UI Kit
and host support it.

### Mutations (Insert, Update, Delete)

Use `simple.data.mutate()` for standard GraphQL mutations that do not update the current record form:

```tsx
const INSERT_TASK = `
  mutation InsertTask($title: String!) {
    insert_my_app__task(object: { title: $title }) { id title }
  }
`

async function createTask(title: string) {
  const simple = await spaceConnection
  const result = await simple.data.mutate(INSERT_TASK, { title })
  return result?.insert_my_app__task
}
```

### Managed Record Views (`ListView`, `DataGrid`, `Board`)

When exported by the installed release and supported by the host, Spaces can render managed record views using supported components from `@simpleplatform/ui-kit/react`. These components integrate natively with host styles, themes, authorization, and data transport.

Managed views accept an authorized database table source. Records are fetched and paged by the platform through the host bridge. Do not pass row arrays, query documents, or access grants.
Spaces support List, DataGrid, and Board. Inbox presets are rendered only on platform collection pages.

Inspect installed SDK/UI Kit exports and negotiated host capabilities; older published UI Kits expose only RecordForm/RecordActivity/StatusBadge. Keep both packages at compatible releases and use public compatibility documentation rather than a forecast version number. Standalone ConditionBuilder/ConditionSummary remain private. Store multiple presets in `custom_view.customization.views`; select one supported preset for each Space component. Tile presentations are `standard`, `media`, and `message`; `subtitleFieldNames` accepts an ordered array. See the [Custom Views guide](https://simple.dev/docs/features/custom-views/#configure-table-view-presets) and [UI Kit API](https://github.com/simple-platform/simple-sdks/blob/main/sdks/space-ui/README.md#managed-database-backed-views).

#### Database-Backed Table Source (`TableRecordSource`)

Use a table source (`kind: 'table'`) when displaying authorized records directly from a Simple Platform database table (such as `dev.simple.system` / `application` or your application's custom tables):

```tsx
import type { TableRecordSource } from '@simpleplatform/ui-kit/contracts'
import type { ListPreset } from '@simpleplatform/ui-kit/contracts'
import { ListView } from '@simpleplatform/ui-kit/react'

const source: TableRecordSource = {
  appId: 'dev.simple.system',
  kind: 'table',
  tableId: applicationsTable.id,
}

const preset: ListPreset = {
  orderBy: [{ direction: 'asc', fieldName: 'display_name' }],
  config: {
    tile: {
      detailFieldNames: ['version'],
      presentation: 'standard',
      titleFieldName: 'display_name',
      version: 1,
    },
  },
  id: 'applications-list',
  kind: 'list',
  title: 'Applications',
}

// In your component:
<ListView
  client={simple}
  preset={preset}
  source={source}
  onItemOpen={openRecord}
/>
```

Implement `openRecord` with public `simple.navigation.open()` or a deliberate
independent record session, handling failure visibly.

Key characteristics:

- **Database-backed:** Records are queried, sorted, and paginated automatically through the authenticated host bridge.
- **Stable logical SCL names:** Presets and filters specify schema field names (e.g., `display_name`, `version`) directly without inspecting metadata tables.
- **Table ID:** `source.tableId` is the platform table metadata record ID. Resolve it from the authorized metadata available to the Space; the managed renderer verifies it belongs to `source.appId` before loading fields or records.
- **Presets configure presentation only:** Presets define layout, tiles, sorting, and filters; they do not carry row records.
- **Tile styling is platform-owned:** Tile presets choose field bindings and one of `standard`, `media`, or `message`; typography, spacing, and sizing come from the platform.
- **Preset icons follow kind:** The platform selects the view icon from `kind`; tile icon IDs use a provider prefix such as `lucide:star`.

Board moves use an enum lane and persist through managed record update/submit;
reject failed saves so the UI can roll back.

Managed views do not render records supplied by external APIs or caller-owned arrays. For external data, build a Space-owned UI around that API instead of presenting it as platform-managed records.

#### No Protocol Code Copying

Do **not** copy, fork, or reimplement the internal `<simple-record-view>` custom element, MessagePort channels, or wire RPC protocols in your Space code. Import supported components (`ListView`, `DataGrid`, `Board`) from `@simpleplatform/ui-kit/react` and source/preset types from `@simpleplatform/ui-kit/contracts`.

### Invoking app Actions

When the host negotiates the Action capability, a Space can call `simple.actions.run(name, input)` to invoke an Action belonging to its own app. This is distinct from a header action, whose callback runs inside the Space; there is no public `RecordAction` component, and the API cannot target another app. For record-specific composition and rules, see the [Record Space Builder skill](../record-space-builder/SKILL.md).

### Record Space workflow

For record-bound Spaces—especially those using host tabs, managed record forms, header actions, app Actions, or platform theming—follow the [Record Space Builder skill](../record-space-builder/SKILL.md) for the current host and SDK contracts.

When `simple.context.kind === 'record'`, the platform has configured this Space as a record body and keeps the record workflow authoritative. Use `simple.records.current()` for the current route record:

```ts
const simple = await spaceConnection

if (simple.context.kind === 'record') {
  const record = await simple.records.current()
  const update = await record.update({ first_name: 'Ada' })
  if (!update.ok) {
    // Render the managed snapshot errors; do not submit a failed update.
    return
  }
  const result = await record.submit()

  if (!result.ok) {
    const snapshot = record.snapshot()
    // Render snapshot.errors.form and each affected snapshot.fields[field].error.
  }
}
```

Do not use `simple.data.mutate()` to change the current record form. `record.update()` and `record.submit()` preserve Record Behaviors, validation, document handling, permissions, and the managed record session. A Record Space supplies its own Save callback through the host header actions; check the submit result. In a standalone Space, `simple.records.current()` throws a structured unavailable error when called; `simple.data` remains available.

An app Action does not update the managed route record; use its record handle for that workflow.

### Accessing External APIs

If your Space needs to load images or make requests to external domains, you must declare those domains in the space's `permissions` field in `10_spaces.scl`. The parent app reads this JSON and injects it into the `<iframe csp="...">` attribute at runtime.

Wildcard subdomains are supported using the `*.` prefix (e.g., `https://*.amazonaws.com`).

````scl
var my_space_permissions {
  value ```
  {
    "network": ["https://api.stripe.com", "https://*.example.com"],
    "images": ["https://*.amazonaws.com", "https://avatars.githubusercontent.com"]
  }
  ```
}

set dev_simple_system.space, my_space {
  permissions `$var('my_space_permissions') |> $json()`
  # ... other fields
}
````

Once declared, you may then use standard `fetch()` for _those specific external domains_, and the browser's CSP engine will permit it.

## 5. Building, Testing, and Deploying

Run all commands from this workspace root using the `simple` CLI:

- **Building:** `simple build <app-id>` builds an app; `simple build --all` builds all apps. Build Spaces as well as Actions before deploying; deploy uploads their `dist` artifacts.
- **Testing:** `simple test` runs all tests including space unit tests.
- **Deploying:** `simple deploy apps/<app-id> --env <configured-env>` publishes and installs the app. `--no-install` publishes without installation.

Use the workspace's supported DevStudio or development-instance workflow for integrated preview. A Vite-only preview cannot prove the host handshake, record lifecycle, permissions, or theme negotiation. Check `simple devstudio serve --help` for the installed CLI.

## 6. Best Practices

- Use the [Space frontend design skill](../space-frontend-design/SKILL.md) as the source for visual composition, tokens, accessibility, responsive layouts, charts, and browser QA.
- Prefer public platform components and the organization's shared UI package before local controls. Check installed package exports and host capabilities before importing managed views.
- Handle query and mutation loading, error, retry, and stale-result states. Use managed record handles when changing the active Record Space form.
- Keep one SDK connection per iframe and use `connect({ targetOrigin })` from `@simpleplatform/sdk/space`; never copy the MessageChannel protocol.
