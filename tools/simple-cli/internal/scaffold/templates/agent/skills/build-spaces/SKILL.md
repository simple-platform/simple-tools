---
name: build-spaces
description: Guide to building custom React-based Spaces in Simple.
---

# Build Spaces Skill

## 1. Concepts

- **Space:** A custom React application embedded within the Simple platform. Spaces provide complete UI freedom when Custom Views aren't flexible enough.
- **Routing:** Spaces are typically single-page applications (SPAs) that handle their own internal routing if needed (e.g., using `react-router-dom`).
- **Styling:** Tailwind classes only, with every colour taken from the brand theme and components from the UI Kit first. See [Design, Styling and Components](#5-design-styling-and-components).

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
    │   └── theme.css # Tailwind and the UI Kit's brand theme
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

The scaffold imports `connect()` directly from the published SDK. Do not add a copied bridge or a local wrapper just to rename the connection. Explicit host-origin configuration is required using `targetOrigin` (use `new URL(document.referrer).origin` in the embedded example; trusted configuration is required when referrer is unavailable).

```tsx
import { connect } from '@simpleplatform/sdk/space'

// Trusted configuration is required when referrer is unavailable.
const spaceConnection = connect({
  targetOrigin: new URL(document.referrer).origin,
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

> **Note:** Custom Logic (Actions) cannot be invoked from Spaces at this time. Only pure GraphQL queries and mutations (insert, update, delete) are supported.

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

## 5. Design, Styling and Components

- **Choose the screen:** Prefer a Space wherever a plain list or form would leave people doing work the screen could do for them. For one record's own page, such as a project, an invoice or an employee, build a Record Space (see [Record Space workflow](#record-space-workflow)): the platform keeps the record's header, workflow, permissions and Update button. Use a standalone Space for dashboards and work across many records.
- **Design:** Rich and eye-catching but calm. Put the figures and statuses that matter most first, group related details in cards, and design what people see with no data, while data loads, and when a read fails. Show the instance's live data, never sample data.
- **Colours come from the brand theme.** The UI Kit's theme maps Tailwind's colour names to the instance's brand colour variables, in light and dark: `primary`, `card`, `muted-foreground`, `destructive`, `border` and their kin, the platform's `surface-`, `text-`, `border-` and `action-` families, and `chart-1` to `chart-5` for charts, as in `bg-primary` or `text-muted-foreground`. Never write a colour value (hex, rgb, hsl or a colour's name), never use a bracketed value for a colour or a variable, and never define a CSS variable of your own when a theme name covers it.
- **Tailwind classes only.** No stylesheets beyond `src/styles/theme.css`, no inline `style` attributes, and no CSS-in-JS. `theme.css` imports Tailwind and then the UI Kit's theme, `@simpleplatform/ui-kit/theme.css`, and `src/main.tsx` imports it once; the scaffold sets both up.
- **UI Kit components first.** Use `@simpleplatform/ui-kit` wherever it has a component: `StatusBadge` for every status, `RecordForm` for a record's form, `RecordActivity` for its activity, and in a Record Space the header actions (`simple.ui.header`) and tabs the Space SDK provides. The kit is young, so check its guide for what it offers today.
- **Otherwise shadcn and Tailwind.** Only when the UI Kit has nothing that fits, build the piece from shadcn components styled with Tailwind, and never write from scratch a component shadcn offers.

## 6. Building, Testing, and Deploying

All commands are run from the workspace root using the `simple` CLI:

- **Building:** `simple build` compiles all apps including their spaces.
- **Testing:** `simple test` runs all tests including space unit tests.
- **Deploying:** `simple deploy <app-id>` deploys the app and its spaces to a dev instance.

> **Note:** There is currently no local live-preview for Spaces. To preview and debug a Space, deploy the app to a dev instance and load the deployed Space in the browser.

## 7. Best Practices

- **UI Components:** Use the UI Kit's components first, then shadcn styled with Tailwind, as section 5 describes.
- **Error Handling:** Always handle loading and error states for queries and mutations to provide good UX.
- **State Management:** For complex state, combine React Context or a state management library with the `query`/`mutate` SDK functions.
- **SDK:** Call `connect()` from `@simpleplatform/sdk/space`. Do not copy, fork, reimplement, or rename the MessageChannel connection protocol.
