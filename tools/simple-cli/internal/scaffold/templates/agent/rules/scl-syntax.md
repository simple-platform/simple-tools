trigger: glob
globs: "**/*.scl"
---
# SCL Syntax Rules

> [!TIP]
> Use these rules when writing Simple Configuration Language (SCL) files.
> SCL is a declarative, whitespace-sensitive, line-oriented language.

## 1. Block Structure
*   **Format:** `type [name] { ... }` or `type arg1, arg2 { ... }`
*   **Opening:** The opening brace `{` must be on the same line as the declaration.
*   **Closing:** The closing brace `}` must be on its own line.

**Correct:**
```scl
table user {
  # ...
}

set dev_simple_system.setting, my_key {
  # ...
}
```

**Incorrect:**
```scl
table user
{ # WRONG: Brace on new line
}
```

## 2. Lists & Arrays (CRITICAL)
*   **Constraint:** SCL **does NOT** use square brackets `[]` for lists.
*   **Syntax:** Lists are defined as **comma-separated values**.
*   **Interpretation:** If a key has multiple values separated by commas, the parser treats them as a list.

**Correct:**
```scl
roles "admin", "editor", "viewer"
tags "urgent", "v1"
```

**Incorrect:**
```scl
roles ["admin", "editor"] # WRONG: Syntax error
tags ["urgent", "v1"]     # WRONG: Syntax error
```

## 3. String Quoting
*   **Double Quotes (Standard):** `"Hello World"` (Recommended for most values).
*   **Single Quotes:** `'Hello World'` (Valid alternative).
*   **Block Quotes:** Triple backticks for multiline strings (e.g., Queries, JSON).
    ```scl
    query ```
    query {
      users { id name }
    }
    ```
    ```
*   **Atom:** Colon prefix for keywords or types (e.g., `:string`, `:id`).

## 4. Comments
*   **Syntax:** `#` acts as the comment character.
*   **Scope:** Runs from the `#` to the end of the line.

```scl
# This is a comment
display_name "User" # Inline comment
```
