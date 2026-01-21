# Logic Standards Rule

> [!CRITICAL]
> **Strict Separation of Concerns**
> You must distinguish between **Server Actions** (Service Logic) and **Record Behaviors** (Form Logic).
> **NEVER** mix their signatures or contexts.

## 1. Signature Invariants (Do Not Hallucinate)

### ✅ Server Actions (The "Backend")
*   **Role**: API Endpoint / Job Processor / Event Handler.
*   **Environment**: Server (WASM).
*   **Trigger**: Database Events, Schedules, Webhooks.
*   **Context**: `Request` object.
*   **Output**: JSON Response or Void.
*   **Signature**:
    ```typescript
    import simple from '@simpleplatform/sdk'
    simple.Handle(async (req) => { ... })
    ```

### ✅ Record Behaviors (The "Form")
*   **Role**: UI Interaction / Validation / Defaulting.
*   **Environment**: Client (Browser) + Server (Validation).
*   **Trigger**: Form Lifecycle (`load`, `update`, `submit`).
*   **Context**: `{$form, $db, $user, $ai}`.
*   **Output**: Side-effects on `$form`.
*   **Signature**:
    ```javascript
    export default async ({ $form, $db }) => { ... }
    ```

## 2. Anti-Patterns (Forbidden)
*   ❌ **The "Frankenstein"**:
    *   `export default async function ({ $db })` ... intending to be a Server Action. (**WRONG**: Actions use `simple.Handle`).
    *   `simple.Handle(...)` ... intending to use `$form`. (**WRONG**: Actions have no `$form` access).

*   ❌ **Imperative Seeding**:
    *   Using an Action to loop and `create()` static data (e.g. `seed_data` action).
    *   **CORRECTION**: Use SCL `instance` blocks for all static data.

## 3. Storage Usage
*   **Record Behaviors**: `$db` is **Read-Only** (Querying reference data). You rarely write to DB directly in a behavior; you modify the `$form` and let the system save.
*   **Server Actions**: Use the SDK generated clients (e.g., `client.com_acme_crm.reservation.create({...})`) to perform writes.
