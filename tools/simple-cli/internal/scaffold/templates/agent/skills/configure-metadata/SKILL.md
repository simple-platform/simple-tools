---
name: configure-metadata
description: Guide to polishing the User Interface (Display Names, Ordering, Visibility).
---
# Configure Metadata Skill

## 1. Introduction
By default, the Platform generates UI labels from your SCL identifiers:
*   `table user` -> "Users"
*   `field first_name` -> "First Name"

Use **Metadata Records** in `records/100_metadata.scl` to override these defaults, reorder fields, or hide technical columns.

## 2. Prerequisites
You MUST define a `var` to look up Table and Field IDs, as they are required for all configuration records.

```scl
var meta {
  query ```
  query {
    tables: dev_simple_system__tables(where: { ... }) {
      id name
      fields { id name }
    }
  }
  ```
}
```

## 3. Table Configuration
Customize how a Table appears in the navigation.

```scl
set dev_simple_system.table, order_config {
  # 1. Target ID
  id `$var('meta') |> $jq('.tables[] | select(.name == "order") | .id')`

  # 2. UI Properties
  display_name "Sales Orders"
  description "Manage customer purchase orders"
  icon "shopping-cart"      # Feather Icon name
  hidden false              # Set true to hide from nav
}
```

## 4. Field Configuration
Customize how a Field appears in Forms and Lists.

```scl
set dev_simple_system.table_field, order_total_config {
  # 1. Target ID
  id `$var('meta') |> $jq('.tables[] | select(.name == "order") | .fields[] | select(.name == "total") | .id')`

  # 2. UI Properties
  display_name "Grand Total"
  help_text "Includes tax and shipping"
  
  # 3. Positioning (Lower = Higher in form)
  position 10 
  
  # 4. Visibility rules
  hidden false
  readonly true  # Note: Use Behaviors for conditional readonly logic; this sets a static readonly state.
}
```

## 5. View Configuration (Custom Views)
See the `create-view` skill for configuring list layouts and dashboards.
