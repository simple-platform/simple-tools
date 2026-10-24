---
name: testing
description: Guide to running and writing tests for Actions and Record Behaviors.
---

# Testing Skill

## 1. Running Tests

`simple test` hands each target to its own runner: **Vitest** for TypeScript and
JavaScript, **`cargo test`** for Rust actions. Both are reached through the same
command, so a workspace holding both kinds is tested in one run.

```bash
# Run ALL tests in the workspace (all apps)
simple test

# Run tests for a specific App
simple test com.mycompany.crm

# Run tests for a specific Server Action
simple test com.mycompany.crm --action import-data
# OR via alias
simple test com.mycompany.crm -a import-data

# Run tests for a specific Record Behavior (Client Script)
simple test com.mycompany.crm --behavior order
# OR via alias
simple test com.mycompany.crm -b order
```

### Options

- `--coverage`: Generate coverage report (text/lcov). Vitest targets only —
  coverage for Rust is a separate tool (`cargo-llvm-cov`) rather than a flag on
  the test runner, so Rust actions run without it and the run says so.
- `--json`: Output results in JSON for CI integration.

## 2. Testing Actions (Server)

Actions are pure TypeScript functions. Helper utilities in `tests/helpers.ts` mock the SDK Request/Context.

**Path:** `apps/<app>/actions/<name>/tests/index.test.ts`

```typescript
import { describe, expect, it } from 'vitest'
import { handle } from '../index'
import { createMockRequest } from './helpers'

describe('import-data', () => {
  it('should parse input and return success', async () => {
    const req = createMockRequest({ source: 'api' })
    const res = await handle(req)
    expect(res).toEqual({ status: 'ok' })
  })
})
```

## 3. Testing Rust Actions

A Rust action's tests live in the `#[cfg(test)] mod tests` inside its own source,
which is where cargo looks for them — there is no separate tests directory.

**Path:** `apps/<app>/actions/<name>/src/main.rs`

`simple::testing::install` puts a closure where the platform would be, so the
handler is called directly and answers in the same types production hands it.
Nothing here is compiled to wasm, so the tests run at the speed of a normal
`cargo test`.

```rust
#[cfg(test)]
mod tests {
    use simpleplatform_sdk::testing;

    use super::*;

    #[test]
    fn it_totals_the_open_invoices() {
        let session = testing::install(|name, params| {
            assert_eq!(name, "action:db/execute");
            assert_eq!(params["variables"]["id"], json!("CUS1"));

            Ok(json!({ "invoices": [{ "amount": 12.5 }, { "amount": 7.5 }] }))
        });

        let output = handler(Request::new(Input { customer_id: "CUS1".into() })).unwrap();

        assert_eq!(output.total, 20.0);
        assert_eq!(session.calls().len(), 1);
    }
}
```

`simple::run` works under a session too, so a test can assert the exact document
the platform reads at the end of a run — `session.done()`.

## 4. Testing Record Behaviors (Client)

Behaviors are scripts with injected globals (`$form`, `$db`). We MUST mock these.

**Path:** `apps/<app>/scripts/record-behaviors/<table_name>.test.js`

```javascript
import { describe, expect, it, vi } from 'vitest'
// Import the default export from the script
import script from './order.js'

describe('order behavior', () => {
  it('should set default status on load', async () => {
    // 1. Mock the API
    const $form = {
      event: 'load',
      status: { set: vi.fn(), value: () => null }
    }
    const $user = { id: 'usr_123' }

    // 2. Run the script
    await script({ $form, $user })

    // 3. Assertions
    expect($form.status.set).toHaveBeenCalledWith('Draft')
  })
})
```
