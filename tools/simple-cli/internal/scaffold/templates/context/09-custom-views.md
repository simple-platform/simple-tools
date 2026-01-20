# Custom Views and View Actions

Create custom UI views and add action buttons to tables and records.

---

## Custom Views

Register a custom view for a table:

````scl
var tables {
  query ```
  query {
    tables: dev_simple_system__tables(
      where: {application_id: {_eq: "com.mycompany.myapp"}}
    ) {
      id
      name
    }
  }
  ```
}

set dev_simple_system.custom_view, order_record_view {
  display_name "Order Record View"
  type record
  target_table_id `$var('tables') |> $jq('.tables[] | select(.name == "order") | .id')`
}
````

### View Types

| Type | Description |
|------|-------------|
| `record` | Single record view |
| `list` | Table list view |
| `dashboard` | Dashboard view |

---

## View Actions

Add buttons to custom views that trigger Actions:

````scl
var data {
  query ```
  query {
    custom_views: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "custom_view"}}
      }
    ) {
      key: logical_key
      id: target_record_id
    }
    triggers: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "trigger"}}
      }
    ) {
      key: logical_key
      id: target_record_id
    }
  }
  ```
}

set dev_simple_system.view_action, generate_invoice_btn {
  icon "file-text"
  name "generate-invoice"
  label "Generate Invoice"
  type outline
  custom_view_id `$var('data') |> $jq('.custom_views[] | select(.key == "order_record_view") | .id')`
  trigger_id `$var('data') |> $jq('.triggers[] | select(.key == "generate_invoice") | .id')`
}
````

### View Action Properties

| Property | Description |
|----------|-------------|
| `icon` | Icon name (e.g., `file-text`, `download`, `send`) |
| `name` | Internal identifier (kebab-case) |
| `label` | Button text |
| `type` | `primary`, `outline`, `danger` |
| `custom_view_id` | Target view |
| `trigger_id` | Trigger to invoke |

---

## Complete Example

Full workflow for a "Generate LOI" button on an offers table:

````scl
# 1. Query existing data
var data {
  query ```
  query {
    tables: dev_simple_system__tables(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        name: {_eq: "offer"}
      }
    ) {
      id
    }
    logic: dev_simple_system__logics(
      where: {application_id: {_eq: "com.mycompany.myapp"}}
    ) {
      id
      name
    }
    custom_views: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "custom_view"}}
      }
    ) {
      key: logical_key
      id: target_record_id
    }
    triggers: dev_simple_system__app_records(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        target_table: {name: {_eq: "trigger"}}
      }
    ) {
      key: logical_key
      id: target_record_id
    }
  }
  ```
}

# 2. Register the Action
set dev_simple_system.logic, generate_loi {
  name "generate-loi"
  display_name "Generate LOI"
  application_id "com.mycompany.myapp"
  execution_environment server
  language typescript
}

# 3. Create a manual trigger
set dev_simple_system.trigger, generate_loi {
  key generate_loi
  name "Generate LOI Trigger"
}

# 4. Bind Action to Trigger
set dev_simple_system.logic_trigger, generate_loi {
  logic_id `$var('data') |> $jq('.logic[] | select(.name == "generate-loi") | .id')`
  trigger_id `$var('data') |> $jq('.triggers[] | select(.key == "generate_loi") | .id')`
}

# 5. Create Custom View
set dev_simple_system.custom_view, offer_record_view {
  display_name "Offer Record View"
  type record
  target_table_id `$var('data') |> $jq('.tables[0].id')`
}

# 6. Add Button to View
set dev_simple_system.view_action, generate_loi_btn {
  icon "file-text"
  name "generate-loi"
  label "Generate LOI"
  type outline
  custom_view_id `$var('data') |> $jq('.custom_views[] | select(.key == "offer_record_view") | .id')`
  trigger_id `$var('data') |> $jq('.triggers[] | select(.key == "generate_loi") | .id')`
}
````

---

→ **Next:** [GraphQL API](./10-graphql-api.md)
