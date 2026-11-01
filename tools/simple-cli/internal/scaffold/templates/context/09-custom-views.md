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
| `list` | Collection presets: Table, List, Board, and Inbox |
| `dashboard` | Dashboard view |

For an active `list` Custom View, put a version 1 preset set in
`customization.views`. Preset filters require native `RecordWhere`; shorthand and
predicate-tree filters are rejected without conversion. Tile layouts also use
version 1. Presets configure database-backed ordering, columns, and tiles; they
do not supply rows. Platform collection routes support
all four kinds. Spaces support List, DataGrid, and Board through the UI Kit;
Inbox is platform-only. Board lanes require an enum field. See
`.agent/skills/create-view/SKILL.md` and the
[Custom Views guide](https://simple.dev/docs/features/custom-views/#configure-table-view-presets) for JSON examples.
Tile layouts choose field bindings and one of `standard`, `media`, or `message`;
typography, spacing, and sizing are controlled by the platform.

To author presets in SCL, store the JSON object in a `var` block string and
parse it with `$json()` when assigning `customization`. This example assumes
`work_item` has `status` and `title` fields:

````scl
var work_item_table {
  query ```
  query {
    tables: dev_simple_system__tables(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        name: {_eq: "work_item"}
      }
      limit: 1
    ) {
      id
    }
  }
  ```
}

var open_items_customization {
  value ```
  {
    "views": {
      "version": 1,
      "defaultPresetId": "open-items",
      "presets": [{
        "id": "open-items",
        "title": "Open items",
        "kind": "list",
        "filter": {"status": {"_eq": "Open"}},
        "config": {
          "tile": {
            "version": 1,
            "presentation": "standard",
            "titleFieldName": "title"
          }
        }
      }]
    }
  }
  ```
}

set dev_simple_system.custom_view, open_items_list {
  display_name "Open items"
  type list
  target_table_id `$var('work_item_table') |> $jq('.tables[0].id')`
  customization `$var('open_items_customization') |> $json()`
  priority 100
  is_active true
}
````

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
