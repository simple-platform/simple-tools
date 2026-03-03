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

Spaces reside in the `spaces/` directory of an app:

```
apps/<app-id>/spaces/<space-name>/
├── package.json      # Dependencies (React, Vite)
├── vite.config.ts    # Vite bundler config
├── index.html        # Entry HTML
└── src/
    ├── App.tsx       # Main React component
    └── main.tsx      # React DOM entry point
```

## 3. Scaffolding a Space

Use the Simple CLI to generate a new space:

```bash
simple new space <app-id> <space-name> <display-name>
```

Example: `simple new space com.acme.crm customer-portal "Customer Portal"`

## 4. API Communication

To interact with Simple's backend, spaces use standard HTTP requests (e.g., built-in `fetch` or Axios). The environment injects necessary context (like authentication tokens or API URLs) when rendering the Space in an iframe.

### Fetching Data via GraphQL

```tsx
import { useEffect, useState } from 'react'

const GET_CUSTOMERS = `
  query GetCustomers {
    customer {
      id
      first_name
      last_name
    }
  }
`

function CustomerList() {
  const [data, setData] = useState<any[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    async function fetchCustomers() {
      try {
        const response = await fetch('/api/graphql', {
          body: JSON.stringify({ query: GET_CUSTOMERS }),
          headers: { 'Content-Type': 'application/json' },
          method: 'POST'
        })
        const result = await response.json()
        setData(result.data.customer || [])
      }
      catch (err) {
        console.error(err)
      }
      finally {
        setLoading(false)
      }
    }
    fetchCustomers()
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

### Calling Actions

Custom Logic/Actions can be invoked via Simple's Action API.

```tsx
import { useState } from 'react'

function ProcessButton({ orderId }: { orderId: string }) {
  const [loading, setLoading] = useState(false)

  const handleProcess = async () => {
    setLoading(true)
    try {
      const response = await fetch(`/api/actions/process-order`, {
        body: JSON.stringify({ order_id: orderId }),
        headers: { 'Content-Type': 'application/json' },
        method: 'POST'
      })
      if (response.ok) {
        alert('Order processed successfully!')
      }
      else {
        throw new Error('Action failed')
      }
    }
    catch (err) {
      console.error(err)
    }
    finally {
      setLoading(false)
    }
  }

  return (
    <button onClick={handleProcess} disabled={loading}>
      {loading ? 'Processing...' : 'Process Order'}
    </button>
  )
}
```

## 5. Local Development and Building

- **Dev Server:** Run `npm run dev` (or `pnpm dev`) inside the space directory to start the Vite development server.
- **Building:** The `simple build` command at the workspace root automatically compiles all spaces for production deployment. You can also run `npm run build` directly inside the space directory.

## 6. Best Practices

- **UI Components:** Build reusable React components for a consistent look and feel across your space.
- **Error Handling:** Always handle loading and error states returned by the SDK hooks to provide good UX.
- **State Management:** For complex State, combine React Context or Redux with the data fetching capabilities of the Simple SDK.
- **Styling:** If building complex interfaces, consider utilizing a robust UI library (like Material-UI, Chakra UI, or Radix UI + Tailwind) to speed up development.
