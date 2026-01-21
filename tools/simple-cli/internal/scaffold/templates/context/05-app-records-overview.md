# App Records Overview

App Records configure platform behavior beyond schema definitions. They are defined in SCL files within the `records/` directory.

---

## File Structure

Files are processed in alphabetical order. Use numeric prefixes:

```
records/
├── 10_seed_data.scl           # Seed data (your tables + platform config)
├── 20_actions.scl             # Logic and trigger definitions
├── 30_links.scl               # Logic-trigger bindings, relationships
├── 100_metadata.scl           # Field positions, behaviors (last)
```

---

## What Can Be Configured

| Category          | Record Types                     | Purpose                           |
| ----------------- | -------------------------------- | --------------------------------- |
| **Seed Data**     | _Any table_                      | Initial/default data              |
| **Metadata**      | `table`, `table_field`           | Display names, field positions    |
| **Relationships** | `table_relationship`             | Cross-app table links             |
| **Actions**       | `logic`                          | Register Actions (Server/Client)  |
| **Triggers**      | `trigger`, `db_event`, `webhook` | Schedule or event-based execution |
| **Bindings**      | `logic_trigger`                  | Connect Actions to Triggers       |
| **Behaviors**     | `record_behavior`                | Attach form scripts to tables     |
| **Views**         | `custom_view`, `view_action`     | Custom UI and buttons             |

---

## Seed Data

You can seed data into **your own tables** or **platform tables** (like settings and users) when the app is installed.

### App Data

Seeding records into your application's tables:

```scl
set lead_source, source_website {
  name "Website"
}

set lead_source, source_referral {
  name "Referral"
}
```

### Platform Configuration (Settings & Users)

You can also seed "platform" records to configure the environment or create default users.

**App Settings:**

```scl
# Regular config value
set dev_simple_system.setting, api_endpoint {
  name "external.api.endpoint"
  display_name "External API Endpoint"
  value "https://api.example.com"
}

# Secret/Encrypted value (use $env here if needed)
set dev_simple_system.setting, api_key {
  name "external.api.key"
  display_name "External API Key"
  secret_value "sk_test_123456"
}
```

**Default Users:**

```scl
set dev_simple_system.user, user_john_smith {
  email "john.smith@example.com"
  first_name "John"
  last_name "Smith"
  is_active true
  roles "user", "hr_manager"
}
```

---

## Basic Syntax

```scl
# Format: set [table_name], [logical_key] { fields... }

set dev_simple_system.record_type, logical_key {
  field1 value1
}
```

### Logical Keys

The `logical_key` (e.g., `user_john_smith` in the example above) is a stable identifier used to manage the record's lifecycle.

- **Stable Identity**: Unlike database IDs which change between environments (Dev/Staging/Prod), the logical key remains constant. This allows the platform to identify "Referral Source" is the same record in all environments.
- **Idempotency**: The platform uses this key to track if a record has already been created. If you change the data in your SCL file, the system uses the logical key to find and _update_ the existing record instead of creating a duplicate.
- **Uniqueness**: Must be unique **per table** within your application.

### Using Variables

````scl
# Define a variable with GraphQL query
var metadata {
  query ```
  query { ... }
  ```
}

# Reference in record definition
set dev_simple_system.record_behavior, my_behavior {
  table_id `$var('metadata') |> $jq('.tables[0].id')`
}
````

---

## Detailed Documentation

Each configuration type has its own documentation:

| Document                                                 | Topics                                    |
| -------------------------------------------------------- | ----------------------------------------- |
| [Expression Language](./04-expression-language.md)       | `$var()`, `$jq()`, piping                 |
| [Metadata Configuration](./06-metadata-configuration.md) | Display names, field positions            |
| [Actions and Triggers](./07-actions-and-triggers.md)     | Logic, triggers, webhooks, scheduling     |
| [Record Behaviors](./08-record-behaviors.md)             | Form scripts with `$form`, `$db`, `$user` |
| [Custom Views](./09-custom-views.md)                     | Custom views and action buttons           |

---

→ **Next:** [Metadata Configuration](./06-metadata-configuration.md)
