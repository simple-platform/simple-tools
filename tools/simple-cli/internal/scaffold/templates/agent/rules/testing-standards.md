---
trigger: glob
globs:
  - "apps/*/actions/*/*.ts"
  - "apps/*/scripts/record-behaviors/*.js"
---
# Testing Standards

> [!IMPORTANT]
> Untested code is legacy code the moment it is written.

## 1. Zero Logic Without Tests
*   **Rule:** Every `Action` or `Record Behavior` MUST have a corresponding `.test.ts` or `.test.js` file.
*   **Coverage:** Aim for 80%+ coverage on business logic paths.

## 2. Mocking Boundaries
*   **Actions:** Do NOT make real HTTP calls or DB queries in tests. Use `vi.mock` or dependency injection.
*   **Behaviors:** Mock `$db.query`. Do not attempt to connect to a real backend.
    *   *Bad:* `await $db.query(...)` in a unit test.
    *   *Good:* `const $db = { query: vi.fn().mockResolvedValue(...) }`

## 3. Hermetic Tests
*   **Guideline:** Tests must be reliable and run offline.
*   **Antipattern:** Tests that fail flakily because of network issues or missing environment variables.
