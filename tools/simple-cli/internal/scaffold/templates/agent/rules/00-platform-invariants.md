---
activation: always
---
# Platform Invariants

> [!IMPORTANT]
> The following rules are INVARIANTS of the Simple Platform. They must NEVER be violated.
> These are the "Laws of Physics" for the SimpleOS.

## 1. Sandboxed Logic (WASM)
*   **Universal Runtime:** All business logic (Server Actions, Client Behaviors, Triggers) compiles to WebAssembly (WASM).
*   **Strict Isolation:** Code runs in a highly secure, deterministic sandbox.
    *   **NO** direct filesystem access (`fs`, `File`).
    *   **NO** direct network access (`net`, `http`, `fetch` without SDK).
    *   **NO** OS primitives (`process`, `child_process`, `env`).
*   **SDK Mandate:** You **MUST** use the provided `@simpleplatform/sdk` for all side effects (Database, HTTP, AI, Storage).

## 2. Universal Execution (Isomorphic)
*   **Write Once, Run Anywhere:** Logic modules are designed to execute on **BOTH** the Client (Browser) and Server.
*   **Divergence is Impossible:** Architectural constraints ensure that client-side validation logic is *identical* to server-side enforcement.
*   **Behavioral Directives:**
    *   **Browser:** Provides instant, optimistic UI feedback to the user (e.g., "Invalid Email").
    *   **Server:** Provides authoritative security, data integrity, and final validation before persistence.

## 3. Declarative Data Management
*   **Schema as Source of Truth:** The database schema is defined purely declaratively in SCL (`tables.scl`).
*   **No Imperative DDL:** You NEVER write SQL `CREATE TABLE`, `ALTER TABLE`, or manual migrations. The Platform handles all DDL generation and execution.
*   **Tenant Isolation:** The Platform automatically manages multi-tenant data isolation. You do not need to add `tenant_id` columns manually.

## 4. Enterprise Mindset
*   **Scale:** Design for millions of records. Always use pagination and efficient queries.
*   **Security:** "Treat User Data as Toxic." Validate all inputs. Never rely on client-side state alone.
*   **Observability:** Logic should be transparent. Use logging and proper error handling.
