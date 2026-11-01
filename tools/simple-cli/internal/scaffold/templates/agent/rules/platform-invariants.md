---
trigger: always_on
---

# Platform Invariants

> [!IMPORTANT]
> The following rules are INVARIANTS of the Simple Platform. They must NEVER be violated.
> These are the "Laws of Physics" for the SimpleOS.

The WASM restrictions below govern Action runtime code. Spaces are browser
frontends and their build tools run in Node; Record Behaviors use injected
platform APIs rather than the Action request API.

## 1. Sandboxed Logic (WASM)
*   **Universal Runtime:** TypeScript Actions compile to WebAssembly (WASM); Record Behaviors are scripts executed by the platform on browser/server paths. Triggers bind events to Actions.
*   **Strict Isolation:** Code runs in a highly secure, deterministic sandbox.
    *   **NO** direct filesystem access (`fs`, `File`).
    *   **NO** direct network access (`net`, `http`, `fetch` without SDK).
    *   **NO** OS primitives (`process`, `child_process`, `env`).
*   **SDK Mandate:** You **MUST** use the provided `@simpleplatform/sdk` for all side effects (Database, HTTP, AI, Storage).

## 2. Universal Execution (Isomorphic)
*   **Write Once, Run Anywhere:** Logic modules are designed to execute on **BOTH** the Client (Browser) and Server.
*   **Server enforcement:** The server is authoritative. Behavior context and available data differ between browser events and partial server writes; verify both paths.
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
