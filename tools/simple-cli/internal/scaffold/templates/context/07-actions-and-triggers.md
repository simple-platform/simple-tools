# Actions and Triggers

Register Actions (Logic) as compiled WASM modules that can execute in both **server** and **client** environments, responding to triggers or database events.

---

## Actions (Logic)

### Creating an Action

Use the CLI to scaffold a new Action with the correct structure and configuration:

```bash
simple new action com.mycompany.myapp import-data --lang ts
```

This generates the source code and configuration. You then register it in SCL.

### Registering an Action

Register the Action (compiled WASM module) in your SCL:

```ruby
set dev_simple_system.logic, import_data {
  name "import-data"
  display_name "Import Data from API"
  description "Imports records from external API every 15 minutes"
  application_id "com.mycompany.myapp"
  execution_environment server
  language typescript
}
```

### Logic Properties

| Property                | Values                     | Description                |
| ----------------------- | -------------------------- | -------------------------- |
| `name`                  | string                     | Matches action folder name (kebab-case)    |
| `display_name`          | string                     | UI display name            |
| `description`           | string                     | Detailed description       |
| `application_id`        | string                     | App ID                     |
| `execution_environment` | `server`, `client`, `both` | Where Action runs          |
| `language`              | `typescript`, `go`         | Source language            |

---

## Triggers

### Time-Based Trigger

Schedule recurring execution using the `recurrence` object:

````ruby
var schedule {
  value ```
  {
    "recurrence": {
      "frequency": "daily",
      "interval": 1,
      "timezone": "America/New_York",
      "time": "09:00:00",
      "weekdays": true
    },
    "options": {
      "start_at": "2024-01-01T00:00:00Z"
    },
    "on_overlap": "skip"
  }
  ```
}

set dev_simple_system.trigger, import_schedule {
  key import_data
  name "Import Data Schedule"
  description "Runs every weekday at 9 AM"
  time_schedule `$var('schedule') |> $json()`
}
````

### Schedule Configuration Options

| Field           | Type    | Description                                                                |
| :-------------- | :------ | :------------------------------------------------------------------------- |
| **Recurrence**  |         |                                                                            |
| `frequency`     | string  | **Required**. `minutely`, `hourly`, `daily`, `weekly`, `monthly`, `yearly` |
| `timezone`      | string  | **Required**. IANA timezone (e.g., `America/New_York`, `Etc/UTC`)          |
| `interval`      | integer | How many periods to wait between runs (default: `1`)                       |
| `time`          | string  | Time of day in `HH:MM:SS` format (e.g., `17:30:00`)                        |
| `days`          | array   | List of days: `["MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"]`          |
| `weekdays`      | boolean | Shortcut for Mon-Fri                                                       |
| `weekends`      | boolean | Shortcut for Sat-Sun                                                       |
| `week_of_month` | string  | For monthly/yearly: `first`, `second`, `third`, `fourth`, `fifth`, `last`  |
| **Options**     |         | Lifecycle constraints                                                      |
| `start_at`      | string  | ISO8601 datetime to start scheduling                                       |
| `end_at`        | string  | ISO8601 datetime to stop scheduling                                        |
| **Execution**   |         |                                                                            |
| `run_as`        | string  | Email of user to impersonate (defaults to app principal)                   |
| `on_overlap`    | string  | `skip` (default), `queue`, `allow`                                         |

### Manual Trigger

Trigger that can be invoked manually or via API:

```ruby
set dev_simple_system.trigger, generate_report {
  key generate_report
  name "Generate Report"
  description "Manually triggered report generation"
}
```

---

### Database Event Trigger

Execute Actions asynchronously (after transaction commit) using the `db_event` record.

> [!NOTE]
> Database events are **asynchronous**. For synchronous validation or data modification during the transaction, use [Record Behaviors](./08-record-behaviors.md).

#### 1. Define the Trigger

First, define a conceptual "Trigger" to act as the hub:

```ruby
set dev_simple_system.trigger, order_events {
  name "Order Events"
  description "Triggers when orders are created or updated"
}
```

#### 2. Define the Database Event (Source)

Connect a table event to the Trigger:

````ruby
# Define variable for JSON operations
var ops {
  value `["insert", "update"]`
}

# Fetch table and trigger IDs (boilerplate)
var ids {
  query ```
  query {
    tables: dev_simple_system__tables(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        name: {_eq: "order"}
      }
    ) { id }
    triggers: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "trigger"}}
        logical_key: {_eq: "order_events"}
      }
    ) { id: target_record_id }
  }
  ```
}

set dev_simple_system.db_event, order_changes {
  name "order-changes"
  display_name "Order Changes"
  description "Fires on high-value orders"

  # Link to Table and Trigger
  table_id `$var('ids') |> $jq('.tables[0].id')`
  trigger_id `$var('ids') |> $jq('.triggers[0].id')`

  # Configuration
  operations `$var('ops') |> $json()`
  condition `.record.total_amount > 1000`
}
````

#### 3. Bind Logic to Trigger

Finally, bind your Logic to the Trigger (see [Logic-Trigger Bindings](#logic-trigger-bindings)).

### Database Event Properties

| Property       | Type    | Description                                                                  |
| :------------- | :------ | :--------------------------------------------------------------------------- |
| `name`         | string  | **Required**. Unique system name for this event hook.                        |
| `table_id`     | ID      | **Required**. The table to monitor.                                          |
| `trigger_id`   | ID      | **Required**. The Trigger record to fire.                                    |
| `operations`   | JSON    | **Required**. Array of database operations: `["insert", "update", "delete"]` |
| `condition`    | string  | **Optional**. `jq` filter expression. Runs if result is `true`.              |
| `display_name` | string  | UI-friendly name.                                                            |
| `description`  | string  | Description of the event's purpose.                                          |
| `is_active`    | boolean | Default `true`. Set to `false` to disable.                                   |

#### Condition Data Context

The `condition` filter has access to the following data context:

```json
{
  "record": { ... },       // The full record data (new state)
  "changes": { ... },      // Only the changed fields
  "operation": "insert",   // "insert", "update", or "delete"
  "table": "order",        // Table name
  "app": "com.mycompany"   // App name
}
```

---

## Webhooks

Trigger Actions via HTTP requests. Like Database Events, this requires a **Trigger** (hub) and a **Webhook** (source).

#### 1. Define the Trigger

```ruby
set dev_simple_system.trigger, payment_events {
  name "Payment Events"
  description "Fires on payment callbacks"
}
```

#### 2. Define the Webhook

Connect an HTTP endpoint to the Trigger:

````ruby
var ids {
  query ```
  query {
    triggers: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "trigger"}}
        logical_key: {_eq: "payment_events"}
      }
    ) { id: target_record_id }
  }
  ```
}

set dev_simple_system.webhook, payment_callback {
  name "payment-callback"
  display_name "Payment Callback"
  description "Receives Stripe payment notifications"

  # Relationship
  trigger_id `$var('ids') |> $jq('.triggers[0].id')`

  # Configuration
  method post
  is_public true
}
````

#### 3. Bind Logic to Trigger

Bind your Logic to the `payment_events` Trigger.

### Webhook Properties

| Property     | Type    | Description                                                               |
| :----------- | :------ | :------------------------------------------------------------------------ |
| `name`       | string  | **Required**. URL slug. Endpoint: `POST /api/hooks/:app_id/:name`         |
| `method`     | enum    | **Required**. `get`, `post`, `put`, `delete`.                             |
| `trigger_id` | ID      | **Required**. The Trigger record to fire.                                 |
| `is_public`  | boolean | `true` allows anonymous access. `false` requires Platform Auth (default). |

---

## Logic-Trigger Bindings

Connect Actions to Triggers:

````ruby
var metadata {
  query ```
  query {
    logics: dev_simple_system__logics(
      where: {application_id: {_eq: "com.mycompany.myapp"}}
    ) {
      id
      name
    }
    triggers: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "trigger"}}
        logical_key: {_eq: "payment_events"}
      }
    ) {
      key: logical_key
      id: target_record_id
    }
  }
  ```
}

set dev_simple_system.logic_trigger, import_binding {
  logic_id `$var('metadata') |> $jq('.logics[] | select(.name == "import-data") | .id')`
  trigger_id `$var('metadata') |> $jq('.triggers[] | select(.key == "import_data") | .id')`
}
````

---

→ **Next:** [Record Behaviors](./08-record-behaviors.md)
