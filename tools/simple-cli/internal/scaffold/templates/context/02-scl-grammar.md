# SCL Grammar & Syntax

The Simple Configuration Language (SCL) is a declarative language for defining schema and application records. It is whitespace-sensitive and line-oriented.

---

## Basic Structure

SCL files consist of **blocks** and **key-value** pairs.

### Blocks

Blocks define objects like tables, records, or fields. They start with a type, an optional name, and an opening brace `{`.

```scl
# Format: type [name] { ... }

table user {
  # Block content
}
```

Blocks can have multiple arguments after the type:

```scl
# Format: type arg1, arg2 { ... }

set dev_simple_system.setting, my_setting {
  # ...
}
```

### Key-Value Pairs

Properties are set using `key value` syntax.

```scl
table user {
  # Key Value
  display_name "System User"
  
  # Boolean
  is_active true
  
  # Number
  timeout 5000
}
```

---

## Value Types

| Type | Syntax | Example |
|------|--------|---------|
| **String** | Double quotes `""` | `"Hello World"` |
| **String (Single)** | Single quotes `''` | `'Hello World'` |
| **String (Block)** | Triple backticks | ``` `Line 1\nLine 2` ``` |
| **Atom** | Colon prefix `:` | `:string`, `:enum` |
| **Boolean** | `true` or `false` | `true` |
| **Number** | Integers or Floats | `42`, `3.14` |
| **Expression** | Backticks `` `...` `` | `` `$var('x')` `` |

---

## Lists & Arrays

SCL **does not** use square brackets `[]` for lists. Instead, it uses **comma-separated values**.

### Incorrect ❌
```scl
roles ["admin", "editor"]
tags ["urgent", "v1"]
```

### Correct ✅
```scl
roles "admin", "editor"
tags "urgent", "v1"
```

If a key has multiple values separated by commas, the parser interprets them as a list of values.

---

## Comments

Comments start with `#` and run to the end of the line.

```scl
# This is a comment
name "foo" # Inline comment
```

---

→ **Next:** [SCL Schema Definition](./03-data-layer-scl.md)
