---
name: build-spaces
description: Guide to building custom React-based Spaces in Simple.
---

# Build Spaces Skill

## 1. Concepts

- **Space:** A custom React application embedded within the Simple platform. Spaces provide complete UI freedom when Custom Views aren't flexible enough.
- **Routing:** Spaces are typically single-page applications (SPAs) that handle their own internal routing if needed (e.g., using `react-router-dom`).
- **Styling:** Reuse the UI Kit, shared host theme, and Tailwind/shadcn patterns. Follow the current Space design skill when the host supplies it.

## 2. Directory Structure

Spaces reside in the `spaces/` directory of an app in your workspace:

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

Use the Simple CLI to generate a new space. Run it from the workspace root, the folder that holds `simple.scl`:

```bash
simple new space <app-id> <space-name> <display-name>
```

Example: `simple new space com.acme.crm customer-portal "Customer Portal"`

## 4. API Communication (Secure RPC)

Spaces operate in an isolated, secure iframe served from `assets.simple.dev` with a strict Content Security Policy (CSP) that blocks external requests by default. Spaces _do not_ possess the parent application's authentication cookies.

Therefore, Space developers **MUST NOT** use raw `fetch()` to query the Simple Backend. Use the published `@simpleplatform/sdk` Space API instead. Its browser adapter establishes the secure MessageChannel with the parent frame and the host authorizes every request.

The scaffold imports `connect()` directly from the published SDK. Do not add a copied bridge or a local wrapper just to rename the connection. Pass an explicit trusted host origin as `targetOrigin`. The embedded example uses the parent origin from `document.referrer`; when that is missing, configure a trusted platform origin rather than guessing from the Space URL. The scaffold shows a visible error if neither is available.

```tsx
import { connect } from '@simpleplatform/sdk/space'

const spaceConnection = Promise.resolve().then(() => {
  if (!document.referrer)
    throw new Error('Open this Space from the platform or configure a trusted host origin.')

  return connect({ targetOrigin: new URL(document.referrer).origin })
})

const simple = await spaceConnection
```

Create one connection promise per Space and reuse it. Calling `connect()` again starts another handshake, which the host does not provide for the same iframe.

### Host-provided context

Every embedded Space receives explicit context from its host at connection time. Do not infer page, table, or record details from the URL or DOM.

```ts
switch (simple.context.kind) {
  case 'standalone':
    // A dashboard, tool, or another embedded page without record context.
    break
  case 'record':
    console.log(simple.context.applicationId, simple.context.tableName, simple.context.recordId)
    break
}
```

`simple.context.kind` is currently either `standalone` or `record`. List context is not available yet. There is no inferred or unknown context state. A missing or malformed host context makes `connect()` reject with a structured SDK error.

### Fetching Data via GraphQL

Use `simple.data.query()` and `simple.data.mutate()` from the connected Space client.

```tsx
import { useEffect, useState } from 'react'

const GET_CUSTOMERS = `
  query GetCustomers {
    customer { id first_name last_name }
  }
`

function CustomerList() {
  const [data, setData] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    spaceConnection
      .then(simple => simple.data.query(GET_CUSTOMERS))
      .then(result => setData(result?.customer || []))
      .catch(err => console.error('RPC Query Failed', err))
      .finally(() => setLoading(false))
  }, [])

  if (loading)
    return <p>Loading...</p>

  return (
    <ul>
      {data.map(c => (
        <li key={c.id}>
          {c.first_name}
          {' '}
          {c.last_name}
        </li>
      ))}
    </ul>
  )
}
```

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

### Record Space workflow

When `simple.context.kind === 'record'`, the platform has configured this Space as a record body and keeps the platform header and record workflow authoritative. Use `simple.records.current()` to work with that current route record:

```ts
const simple = await spaceConnection

if (simple.context.kind === 'record') {
  const record = await simple.records.current()
  await record.update({ first_name: 'Ada' })
  const result = await record.submit()

  if (!result.ok) {
    const snapshot = record.snapshot()
    // Render snapshot.errors.form and each affected snapshot.fields[field].error.
  }
}
```

Do not use `simple.data.mutate()` to change the current record form. `record.update()` and `record.submit()` preserve Record Behaviors, validation, documents, permissions, and the shared state used by the platform Update button. In a standalone Space, `simple.records.current()` throws a structured unavailable error when called; `simple.data` remains available.

Spaces can also run an action declared by their own app through the host:

```ts
const result = await simple.actions.run('my-action', { record_id: 'REC000001' })
```

The host authorizes the request; Spaces cannot name an action from another app. Handle the SDK's structured unavailable error when the host has not negotiated the action capability.

### Managed Record Views (`ListView`, `DataGrid`, `Board`)

Spaces can render managed record views using supported components from `@simpleplatform/ui-kit/react`. These components integrate natively with host styles, themes, authorization, and data transport.

Managed views accept an authorized database table source. Records are fetched and paged by the platform through the host bridge. Do not pass row arrays, query documents, or access grants.
Spaces support List, DataGrid, and Board. Inbox presets are rendered only on platform collection pages.

Managed views require the planned SDK `^3.2.0` release, which is not yet published; SDK 3.1.0 lacks their bridge. Use matching feature builds locally. Publish the compatible SDK before the UI Kit; the release check validates the published bridge. Standalone ConditionBuilder/ConditionSummary remain private. Store multiple presets in `custom_view.customization.views`; select one supported preset for each Space component. See the [Custom Views guide](https://simple.com/docs/features/custom-views) and [UI Kit API](https://github.com/simple-platform/simple-sdks/blob/main/sdks/space-ui/README.md#managed-database-backed-views).

#### Database-Backed Table Source (`TableRecordSourceV1`)

Use a table source (`kind: 'table'`) when displaying authorized records directly from a Simple Platform database table (such as `dev.simple.system` / `application` or your application's custom tables):

```tsx
import type { TableRecordSourceV1 } from '@simpleplatform/ui-kit/contracts'
import type { ListPresetV1 } from '@simpleplatform/ui-kit/contracts'
import { ListView } from '@simpleplatform/ui-kit/react'

const source: TableRecordSourceV1 = {
  appId: 'dev.simple.system',
  kind: 'table',
  tableId: applicationsTable.id,
}

const preset: ListPresetV1 = {
  config: {
    orderBy: [{ direction: 'asc', fieldName: 'display_name' }],
    tile: {
      detailFieldNames: ['version'],
      presentation: 'row',
      titleFieldName: 'display_name',
      version: 1,
    },
  },
  id: 'applications-list',
  iconId: 'list',
  kind: 'list',
  title: 'Applications',
}

// In your component:
<ListView
  client={simple}
  preset={preset}
  source={source}
  onItemOpen={(ref) => console.log('Opened record:', ref.recordId)}
/>
```

Key characteristics:
- **Database-backed:** Records are queried, sorted, and paginated automatically through the authenticated host bridge.
- **Stable logical SCL names:** Presets and filters specify schema field names (e.g., `display_name`, `version`) directly without inspecting metadata tables.
- **Table ID:** `source.tableId` is the platform table metadata record ID. Resolve it from the authorized metadata available to the Space; the managed renderer verifies it belongs to `source.appId` before loading fields or records.
- **Presets configure presentation only:** Presets define layout, tiles, sorting, and filters; they do not carry row records.
- **Preset icons are optional:** Use a supported `iconId` from `RECORD_VIEW_ICON_IDS_V1`; older presets render with a family icon.

Managed views do not render records supplied by external APIs or caller-owned arrays. For external data, build a Space-owned UI around that API instead of presenting it as platform-managed records.

#### No Protocol Code Copying

Do **not** copy, fork, or reimplement the internal `<simple-record-view>` custom element, MessagePort channels, or wire RPC protocols in your Space code. Import supported components (`ListView`, `DataGrid`, `Board`) from `@simpleplatform/ui-kit/react` and source/preset types from `@simpleplatform/ui-kit/contracts`.

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

All commands are run from the workspace root using the `simple` CLI:

- **Building:** `simple build` compiles all apps including their spaces.
- **Testing:** `simple test` runs all tests including space unit tests.
- **Deploying:** `simple deploy <app-id>` deploys the app and its spaces to a dev instance.

> **Note:** There is currently no local live-preview for Spaces. To preview and debug a Space, deploy the app to a dev instance and load the deployed Space in the browser.

## 6. Best Practices

- **UI Components:** Build reusable React components for a consistent look and feel across your space.
- **Record Views:** Use supported components from `@simpleplatform/ui-kit/react` (`ListView`, `DataGrid`, and `Board`) with a `{ kind: 'table', appId, tableId }` source. Managed views fetch rows through the host and provide built-in filtering; presets configure presentation and initial filters. Standalone filter-builder components are not public Space APIs. Inbox is platform-only. Never pass row arrays, copy protocol code, or embed raw `<simple-record-view>` custom elements.
- **Error Handling:** Always handle loading and error states for queries and mutations to provide good UX.
- **State Management:** For complex state, combine React Context or a state management library with the `query`/`mutate` SDK functions.
- **Styling:** Reuse the UI Kit and shared theme; use shadcn primitives where a managed component does not fit.
- **SDK:** Call `connect()` from `@simpleplatform/sdk/space`. Do not copy, fork, reimplement, or rename the MessageChannel connection protocol.
