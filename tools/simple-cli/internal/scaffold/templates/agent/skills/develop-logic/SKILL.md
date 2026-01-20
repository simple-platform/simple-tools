---
name: develop-logic
description: Comprehensive guide to implementing full-stack logic, focusing on the "Hub-and-Spoke" binding architecture.
---
# Develop Logic Skill

## 1. The Architecture: Actions & Binding
In Simple Platform, logic is decoupled from execution triggers.
*   **Logic (The Code):** A pure function (WASM).
*   **Trigger (The Event):** A signal (DB change, Schedule, Webhook).
*   **Binding (The Link):** Connecting a Logic to a Trigger.

This allows one piece of Logic to be triggered by multiple events (e.g., "Send Email" triggered by "Signup" OR "Nightly Job").

## 2. Server Actions (The Code)
**File:** `apps/<app>/actions/<name>/index.ts`
**Command:** `simple new action <app> <name> --scope myorg --env server`

```typescript
import simple from '@simpleplatform/sdk'
simple.Handle(async (req) => {
  const { id } = req.parse<{ id: string }>()
  // ... implementation ...
})
```

## 3. The Registration Pattern (SCL)
You must register the components in `apps/<app>/records/`.

### Step A: Define Logic (`20_logic.scl`)
```scl
set dev_simple_system.logic, action_process_order {
  name "process-order"
  display_name "Process Order"
  execution_environment server
  language go # or ts
}
```

### Step B: Define Trigger (`20_triggers.scl`)
Choose **ONE** type:

**Type 1: Database Event**
```scl
set dev_simple_system.trigger, trigger_order_created {
  name "on-order-created"
  type "db_event"
  # Configuration: Table + Operations
  table_id `$var('meta') |> $jq('.tables[] | select(.name == "order") | .id')`
  operations `["insert"] |> $json()`
}
```

**Type 2: Schedule (Cron)**
```scl
set dev_simple_system.trigger, trigger_nightly {
  name "nightly-cleanup"
  type "time_based"
  time_schedule `{"frequency": "daily", "time": "00:00"} |> $json()`
}
```

**Type 3: Webhook**
```scl
set dev_simple_system.trigger, trigger_stripe_hook {
  name "stripe-webhook"
  type "webhook"
}
```

### Step C: Bind Them (`30_links.scl`)
This is the critical step that activates the logic.

```scl
set dev_simple_system.logic_trigger, bind_process_order {
  is_active true
  
  # Connect the dots
  logic_id `$var('meta') |> $jq('.logics[] | select(.name == "process-order") | .id')`
  trigger_id `$var('meta') |> $jq('.triggers[] | select(.name == "on-order-created") | .id')`
}
```

## 4. Client Record Behaviors
**File:** `apps/<app>/scripts/record-behaviors/<table>.js`
**Command:** `simple new behavior <app-id> <table-name>`

*   **Scope:** Runs in the browser (and verified on server).
*   **API:** `$form`, `$db` (Read-Only), `$user`.
*   **Events:** `load` (Defaults), `update` (Computed), `submit` (Validation).
