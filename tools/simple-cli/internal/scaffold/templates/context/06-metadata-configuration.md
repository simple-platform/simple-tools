# Metadata Configuration

Configure how tables and fields appear in the UI: display names, field order, and cross-app relationships.

---

## Display Names

The platform automatically generates human-readable display names from your snake_case identifiers (e.g., `sales_orders` becomes "Sales Orders"). You only need to provide a `display_name` if you want to override this default.

### Table Display Names

Override table display names:

````scl
var metadata {
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

set dev_simple_system.table, order_display {
  id `$var('metadata') |> $jq('.tables[] | select(.name == "order") | .id')`
  # "Orders" is generated automatically from the table's plural name "orders".
  # Override:
  display_name "Sales Orders"
}
````

---

## Field Display Names

Customize field labels in the UI:

````scl
var fields {
  query ```
  query {
    order_fields: dev_simple_system__table_fields(
      where: {
        table: {
          name: {_eq: "order"}
          application_id: {_eq: "com.mycompany.myapp"}
        }
      }
    ) {
      id
      name
    }
  }
  ```
}

set dev_simple_system.table_field, display_customer_id {
  id `$var('fields') |> $jq('.order_fields[] | select(.name == "customer_id") | .id')`
  display_name "Customer"
}

set dev_simple_system.table_field, display_total {
  id `$var('fields') |> $jq('.order_fields[] | select(.name == "total_amount") | .id')`
  display_name "Order Total"
}
````

---

## Field Positions

Control the order fields appear in forms and lists:

````scl
# Query field IDs
var metadata {
  query ```
  query {
    fields: dev_simple_system__table_fields(
      where: {
        table: {
          name: {_eq: "order"}
          application_id: {_eq: "com.mycompany.myapp"}
        }
      }
    ) {
      id
      name
    }
  }
  ```
}

# Set positions (lower numbers appear first)
set dev_simple_system.table_field, pos_id {
  id `$var('metadata') |> $jq('.fields[] | select(.name == "id") | .id')`
  position 10
}

set dev_simple_system.table_field, pos_customer {
  id `$var('metadata') |> $jq('.fields[] | select(.name == "customer_id") | .id')`
  position 20
}

set dev_simple_system.table_field, pos_status {
  id `$var('metadata') |> $jq('.fields[] | select(.name == "status") | .id')`
  position 30
}

set dev_simple_system.table_field, pos_total {
  id `$var('metadata') |> $jq('.fields[] | select(.name == "total_amount") | .id')`
  position 40
}

# Audit fields at the end
set dev_simple_system.table_field, pos_created_at {
  id `$var('metadata') |> $jq('.fields[] | select(.name == "created_at") | .id')`
  position 100
}

set dev_simple_system.table_field, pos_updated_at {
  id `$var('metadata') |> $jq('.fields[] | select(.name == "updated_at") | .id')`
  position 110
}
````

---

## Cross-App Table Relationships

Link tables across different apps:

````scl
var metadata {
  query ```
  query {
    source_table: dev_simple_system__tables(
      where: {
        name: {_eq: "customer"}
        application_id: {_eq: "com.mycompany.crm"}
      }
    ) {
      id
    }
    target_table: dev_simple_system__tables(
      where: {
        name: {_eq: "order"}
        application_id: {_eq: "com.mycompany.sales"}
      }
    ) {
      id
      fields(where: {name: {_eq: "id"}}) {
        id
      }
    }
  }
  ```
}

set dev_simple_system.table_relationship, customer_orders {
  name orders
  display_name "Customer Orders"
  kind has
  cardinality many
  source_table_id `$var('metadata') |> $jq('.source_table[0].id')`
  target_table_id `$var('metadata') |> $jq('.target_table[0].id')`
  target_field_id `$var('metadata') |> $jq('.target_table[0].fields[0].id')`
}
````

### Relationship Properties

| Property          | Values           | Description                 |
| ----------------- | ---------------- | --------------------------- |
| `kind`            | `has`, `belongs` | Relationship direction      |
| `cardinality`     | `one`, `many`    | One-to-one or one-to-many   |
| `source_table_id` | ID               | Table with the relationship |
| `target_table_id` | ID               | Related table               |
| `target_field_id` | ID               | Foreign key field           |

---

## Seed Data

Insert initial data into your tables:

```scl
# Simple seed records (no query needed)
set lead_source, source_website {
  name "Website"
}

set lead_source, source_referral {
  name "Referral"
}

set lead_status, status_new {
  label "New Lead"
}

set lead_status, status_qualified {
  label "Qualified"
}
```

---

→ **Next:** [Actions and Triggers](./07-actions-and-triggers.md)
