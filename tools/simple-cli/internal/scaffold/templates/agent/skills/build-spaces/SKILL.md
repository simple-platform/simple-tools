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
    ├── lib/
    │   └── simple.ts # RPC SDK (auto-generated, do not modify)
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

Therefore, Space developers **MUST NOT** use raw `fetch()` to query the Simple Backend. Instead, they must use the generated MessageChannel RPC SDK (`src/lib/simple.ts`), which asks the secure parent frame to resolve the query on its behalf.

### Fetching Data via GraphQL

Use the `query` and `mutate` functions provided by the local Simple SDK.

```tsx
import { useEffect, useState } from 'react'
import { query } from './lib/simple'

const GET_CUSTOMERS = `
  query GetCustomers {
    customer { id first_name last_name }
  }
`

function CustomerList() {
  const [data, setData] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    query(GET_CUSTOMERS)
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

Use the `mutate` function for standard GraphQL mutations:

```tsx
import { mutate } from './lib/simple'

const INSERT_TASK = `
  mutation InsertTask($title: String!) {
    insert_my_app__task(object: { title: $title }) { id title }
  }
`

async function createTask(title: string) {
  const result = await mutate(INSERT_TASK, { title })
  return result?.insert_my_app__task
}
```

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
- **SDK:** Never modify `src/lib/simple.ts` — it is auto-generated by the scaffold. Use only the exported `query` and `mutate` functions.
