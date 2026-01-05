# SDK Reference

Complete API reference for the TypeScript SDK (`@simpleplatform/sdk`).

> [!IMPORTANT]
> This SDK is designed for use within Simple Platform **Actions** (WASM modules).

---

## Installation

```bash
npm install @simpleplatform/sdk
```

---

## Core Concepts

Understanding the core primitives is essential for building Actions.

### Entry Point (`simple.Handle`)

Every Action must export a handler using `simple.Handle`. This function registers your code with the platform's runtime.

```typescript
import simple, { type Request } from '@simpleplatform/sdk'

simple.Handle(async (request: Request) => {
  // Your logic goes here
  const input = request.parse<{ name: string }>()
  return { message: `Hello, ${input.name}!` }
})
```

### Request Object

The `Request` object is passed to your handler and provides access to the input data and execution context.

```typescript
class Request {
  /**
   * Parses the raw input payload into a typed object.
   * usage: const data = request.parse<MyInputType>()
   */
  parse<T>(): T

  /**
   * Returns the raw input payload as a string.
   */
  data(): string

  /**
   * The execution context containing user, tenant, and logic metadata.
   */
  readonly context: Context
  
  /**
   * Headers passed to the action (if triggered via HTTP).
   */
  readonly headers: Record<string, any>
}
```

### Context Object

The `Context` is critical for all SDK operations. It carries security tokens and metadata required by the host environment to authorize API calls. You must pass `request.context` to almost every SDK function.

```typescript
interface Context {
  // Metadata about the current execution
  logic: {
    id: string            // Unique ID of this logic definition
    execution_id: string  // Unique ID for this specific run
    execution_env: string // e.g., 'staging', 'production'
    trigger_id: string    // ID of the trigger that caused this execution
  }
  
  // The tenant (environment) where code is running
  tenant: {
    id?: string
    name: string
    host?: string
  }
  
  // The user who initiated the action (if applicable)
  user: {
    id?: string
  }
}
```

---

## Data Access

### GraphQL (`@simpleplatform/sdk/graphql`)

Reading and writing database records is the most common task. Use the GraphQL module to interact with your application's schema.

```typescript
import { query, mutate } from '@simpleplatform/sdk/graphql'

// QUERY: Fetch data
// usage: await query<ResultType>(queryString, variables, context)
const data = await query<{ orders: Order[] }>(
  `query GetValidOrders($status: String!) {
    orders: com_my_app__orders(where: { status: { _eq: $status } }) {
      id
      total
    }
  }`,
  { status: 'valid' },
  request.context
)

// MUTATION: Modify data
// usage: await mutate<ResultType>(mutationString, variables, context)
const result = await mutate<{ update_order: { id: string } }>(
  `mutation Update($id: ID!) {
    update_order: update_com_my_app__order(
        id: $id, 
        _set: { status: "processed" }
    ) {
      id
    }
  }`,
  { id: 'ORD000123' },
  request.context
)
```

---

## File Storage

### Storage (`@simpleplatform/sdk/storage`)

Before you can process files with AI or save them to records, you often need to import them into the platform.

**Key Concept: DocumentHandle**
When you upload a file, the SDK returns a `DocumentHandle`. This object represents the file uniquely (by content hash) and is used to:
1.  Save the file to a record's `:document` field.
2.  Pass the file to AI functions (extraction, transcription).

```typescript
import { uploadExternal } from '@simpleplatform/sdk/storage'

// Upload a file from a URL
const invoiceHandle = await uploadExternal(
  {
    url: 'https://example.com/invoice.pdf',
    // Optional auth for the source URL
    auth: { type: 'bearer', bearer_token: '...' } 
  },
  {
    // Where the file will eventually be referenced (for permission checks)
    app_id: 'com.mycompany.myapp',
    table_name: 'invoices',
    field_name: 'attachment'
  },
  request.context
)

// Result 'invoiceHandle' structure:
// {
//   file_hash: "sha256...",
//   filename: "invoice.pdf",
//   mime_type: "application/pdf",
//   size: 1024,
//   storage_path: "..."
// }

// You can now save this handle to your database:
await mutate(..., { data: { attachment: invoiceHandle } }, request.context)
```

---

## Intelligence (AI)

### AI (`@simpleplatform/sdk/ai`)

The AI module allows you to extract data, summarize content, or transcribe media. It can accept plain text or `DocumentHandle` objects (created via the Storage module).

```typescript
import { extract, summarize, transcribe } from '@simpleplatform/sdk/ai'

/**
 * 1. EXTRACT
 * Convert unstructured text/files into structured JSON.
 */ 
const userDetails = await extract(
  // Input: Can be a string or a DocumentHandle (e.g., invoiceHandle from above)
  "My name is Alice and I am 30 years old.", 
  {
    prompt: "Extract user details",
    model: "medium", // 'lite' | 'medium' | 'large' | 'xl'
    schema: {
      type: "object",
      properties: {
        name: { type: "string" },
        age: { type: "number" }
      },
      required: ["name"]
    }
  }, 
  request.context
)
console.log(userDetails.data.name) // "Alice"


/**
 * 2. SUMMARIZE
 * Generate a concise summary of text or a document.
 */
const summary = await summarize(
  invoiceHandle, // Pass a DocumentHandle directly
  { prompt: "Summarize the line items in this invoice" },
  request.context
)


/**
 * 3. TRANSCRIBE
 * Convert audio/video files into text.
 */
const transcript = await transcribe(
  callRecordingHandle, // Must be a DocumentHandle (audio/video)
  { 
    includeTranscript: true, 
    includeTimestamps: true,
    // Optional: Identify speakers
    participants: ['Agent', 'Customer'] 
  },
  request.context
)
```

---

## Utilities

### HTTP (`@simpleplatform/sdk/http`)

Make requests to external APIs.

```typescript
import { get, post, put, patch, del, fetch } from '@simpleplatform/sdk/http'

// Simple helpers
await get('https://api.example.com/data', { 'Authorization': '...' }, request.context)
await post('https://api.example.com/webhook', { status: 'done' }, {}, request.context)
await put('https://api.example.com/resource/1', { update: 'data' }, {}, request.context)
await patch('https://api.example.com/resource/1', { partial: 'data' }, {}, request.context)
await del('https://api.example.com/resource/1', {}, request.context)

// Advanced usage
await fetch({
  url: 'https://api.example.com/item',
  method: 'PUT',
  body: { id: 123 },
  headers: { 'Content-Type': 'application/json' }
}, request.context)
```

### Settings (`@simpleplatform/sdk/settings`)

Access configuration values defined for your application.

```typescript
import { get } from '@simpleplatform/sdk/settings'

const config = await get(
  'com.mycompany.myapp', // App ID
  ['api_key', 'retry_count'], // Keys to fetch
  request.context
)

const key = config['api_key']
```
