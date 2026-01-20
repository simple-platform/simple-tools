---
activation:
  type: glob
  pattern: "apps/**/*.{ts,js}"
---
# Logic Boundaries & API Constraints

## 1. Record Behaviors (`scripts/record-behaviors/*.js`)
Lightweight form logic. Runs independently in Browser and Server.

*   **Runtime:** JavaScript (compile-to-WASM).
*   **Scope:** Single Record context.
*   **Permitted APIs:**
    *   `$form`: **Read/Write**. Manage form state, field visibility, validation errors.
    *   `$user`: **Read-Only**. Access current user context (`id`, `email`).
    *   `$db`: **READ-ONLY**. You MAY query data (e.g., look up prices), but you **MUST NOT mutate**.
*   **PROHIBITED:**
    *   `$db.mutate(...)` / `mutation { ... }`.
    *   `alert()`, `console.log()` (Use `$form.info()` or `$ai.log()`).
    *   External HTTP calls (Use Actions instead).

## 2. Server Actions (`actions/**/*.ts`)
Heavy business logic. Runs primarily in Server Sandbox.

*   **Runtime:** TypeScript (transpiled to WASM).
*   **Scope:** Request/Response context.
*   **Permitted APIs (`@simpleplatform/sdk`):**
    *   `simple.Handle`: The required entry point.
    *   `request.context`: **MANDATORY**. Must be passed to all SDK functions to propagate auth/tenant context.
    *   `graphql.query` / `graphql.mutate`: Full database access.
    *   `http`: Secure external API calls.
    *   `ai`: LLM capabilities (extract, summarize).
    *   `storage`: File handling.

## 3. Global Runtime Restrictions (The Sandbox)
*   **Engine:** QuickJS (via Javy).
*   **Compliance:**
    *   **No Node.js Built-ins:** `fs`, `path`, `process`, `os`, `crypto` are NOT available.
    *   **No NPM Binaries:** You can trust type-only packages (e.g., `lodash`, `date-fns`), but packages relying on Node.js bindings will fail.
    *   **Async/Await:** ALWAYS use `async/await`. The runtime is asynchronous.
