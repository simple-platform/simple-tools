# Expression Language

App Records use a powerful expression language for dynamic values. This is the foundation for all configuration in `records/*.scl` files.

---

## Syntax Overview

Expressions are enclosed in backticks and consist of function calls prefixed with `$`, optionally piped together with `|>`.

```scl
# Simple function call
script `$file('scripts/my-script.js')`

# Piped expression
id `$var('metadata') |> $jq('.tables[] | select(.name == "order") | .id')`
```

---

## Functions

### `$var(name)`

Retrieves a variable defined earlier in the file.

````scl
# Define a variable
var metadata {
  query ```
  query { ... }
  ```
}

# Use the variable
table_id `$var('metadata') |> $jq('.tables[0].id')`
````

### `$jq(query)`

Applies a [jq](https://jqlang.org/) query to transform JSON data.

```scl
# Extract a specific field
id `$var('data') |> $jq('.tables[] | select(.name == "order") | .id')`

# Get first element
first `$var('data') |> $jq('.[0]')`

# Filter by condition
active `$var('data') |> $jq('.items[] | select(.is_active == true)')`
```

**Common jq patterns:**
| Pattern | Description |
|---------|-------------|
| `.field` | Get field value |
| `.[0]` | Get first array element |
| `.[]` | Iterate array |
| `select(.x == "y")` | Filter by condition |
| `.field1, .field2` | Extract multiple fields |

### `$json(text)`

Parses a JSON string into an object.

```scl
schedule `$var('schedule_text') |> $json()`
```

### `$file(path)`

Reads file contents from the app directory.

```scl
# Read a script file
script `$file('scripts/record-behaviors/order.js')`
```

### `$encode_image(path)`

Reads an image and returns a Base64 data URI.

```scl
icon `$encode_image('assets/logo.png')`
# Returns: data:image/png;base64,iVBORw0KGgo...
```

### `$trim()`

Removes leading/trailing whitespace from a string.

```scl
value `$var('text') |> $trim()`
```

---

## Pipe Operator `|>`

Chain functions together. Output of the left side becomes input to the right.

```scl
# Step by step:
# 1. Get 'metadata' variable
# 2. Apply jq to extract table ID
# 3. Use as field value
table_id `$var('metadata') |> $jq('.tables[] | select(.name == "order") | .id')`
```

---

## Variables with GraphQL

Variables store query results for use in record definitions.

````scl
var metadata {
  query ```
  query get_metadata {
    tables: dev_simple_system__tables(
      where: {application_id: {_eq: "com.mycompany.myapp"}}
    ) {
      id
      name
    }
    fields: dev_simple_system__table_fields(
      where: {
        table: {
          application_id: {_eq: "com.mycompany.myapp"}
          name: {_eq: "order"}
        }
      }
    ) {
      id
      name
    }
  }
  ```
}

# Use query results
set dev_simple_system.record_behavior, order_behavior {
  table_id `$var('metadata') |> $jq('.tables[] | select(.name == "order") | .id')`
  script `$file('scripts/record-behaviors/order.js')`
}
````

---

## JSON Block Variables

For inline JSON data:

````scl
var schedule {
  value ```
  {
    "recurrence": {
      "frequency": "minutely",
      "interval": 15,
      "timezone": "America/New_York"
    }
  }
  ```
}

set dev_simple_system.trigger, my_trigger {
  time_schedule `$var('schedule') |> $json()`
}
````

---

→ **Next:** [App Records Overview](./05-app-records-overview.md)
