---
name: build-spaces
description: Guide to building custom React-based Spaces in Simple.
---

# Build Spaces Skill

## 1. Concepts

- **Space:** A custom React application embedded within the Simple platform. Spaces provide complete UI freedom when Custom Views aren't flexible enough.
- **Routing:** Spaces are typically single-page applications (SPAs) that handle their own internal routing if needed (e.g., using `react-router-dom`).
- **Styling:** You have full control. You can use plain CSS, inline styles, or integrate libraries like Tailwind CSS or styled-components.

## 2. Directory Structure

Spaces reside in the `spaces/` directory of an app within the `client-bnv` repository:

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

Use the Simple CLI to generate a new space. The CLI should ONLY be used within the `client-bnv` repository as that is where apps and packages are deployed:

```bash
simple new space <app-id> <space-name> <display-name>
```

Example: `simple new space com.acme.crm customer-portal "Customer Portal"`

## 4. API Communication (Secure RPC)

Spaces operate in an isolated, secure iframe served from `assets.simple.dev` with a strict Content Security Policy (CSP) that blocks external requests by default. Spaces _do not_ possess the parent application's authentication cookies.

Therefore, Space developers **MUST NOT** use raw `fetch()` to query the Simple Backend. Use the published `@simpleplatform/sdk` Space API instead. Its browser adapter establishes the secure MessageChannel with the parent frame and the host authorizes every request.

The scaffold imports `connectSpace()` directly from the published SDK. Do not add a copied bridge or a local wrapper just to rename the connection.

```tsx
import { connectSpace } from '@simpleplatform/sdk/space'

const spaceConnection = connectSpace({
  targetOrigin: new URL(document.referrer).origin,
})

const simple = await spaceConnection
```

Create one connection promise per Space and reuse it. Calling `connectSpace()` again starts another handshake, which the host does not provide for the same iframe.

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

`simple.context.kind` is currently either `standalone` or `record`. List context is not available yet. There is no inferred or unknown context state. A missing or malformed host context makes `connectSpace()` reject with a structured SDK error.

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

## 5. Building, Testing, and Deploying

All commands are run from the `client-bnv` repository root using the `simple` CLI:

- **Building:** `simple build` compiles all apps including their spaces.
- **Testing:** `simple test` runs all tests including space unit tests.
- **Deploying:** `simple deploy <app-id>` deploys the app and its spaces to a dev instance.

> **Note:** There is currently no local live-preview for Spaces. To preview and debug a Space, deploy the app to a dev instance and load the deployed Space in the browser.

## 6. Best Practices

- **UI Components:** Build reusable React components for a consistent look and feel across your space.
- **Error Handling:** Always handle loading and error states for queries and mutations to provide good UX.
- **State Management:** For complex state, combine React Context or a state management library with the `query`/`mutate` SDK functions.
- **Styling:** Consider a robust UI library (Material-UI, Chakra UI, Radix UI, etc.) for complex interfaces.
- **SDK:** Call `connectSpace()` from `@simpleplatform/sdk/space`. Do not copy, fork, reimplement, or rename the MessageChannel connection protocol.
