# Data Layer: Simple Configuration Language (SCL)

The Simple Configuration Language (SCL) is a declarative, human-readable language for defining your business data model. It is the **single source of truth** that drives the entire platform.

---

## App Definition (`app.scl`)

```scl
id com.mycompany.invoicing
version 1.0.0
display_name "Invoice Management"
description "Track and manage customer invoices"
```

---

## Table Definition (`tables.scl`)

```scl
table employee, employees {
  default :id, :timestamps, :userstamps

  id_prefix "EMP"
  display_field full_name

  required first_name, :string {
    length 1..50
  }

  required email, :string {
    length 1..100
    unique true
  }

  optional ssn, :string {
    length 9..11
    unique true
    secret true
  }

  optional notes, :string {
    multiline true
  }

  optional wage, :decimal {
    digits 10
    decimals 2
  }

  required gender, :enum {
    values "Male", "Female"
  }

  optional is_active, :boolean {
    default true
  }

  belongs :to, department {
    required false
  }

  has :many, documents {
    table employee_document
  }

  index hired_date { }
  index email { unique true }
}
```

---

## Field Types

### `:string`

```scl
required name, :string {
  length 1..100          # Min..Max characters
  unique true            # Unique constraint
}

required code, :string {
  length 4               # Exact length
}

optional notes, :string {
  multiline true         # Textarea in UI
}

optional ssn, :string {
  length 9..11
  secret true            # Masked in UI, encrypted
}
```

### `:integer`

```scl
required quantity, :integer {
  default 1
}

optional max_occupancy, :integer {
  default 4
}
```

### `:decimal`

```scl
required rate, :decimal {
  digits 10              # Total digits
  decimals 2             # Decimal places
}

optional commission, :decimal {
  digits 4
  decimals 2
  default 3.0
}
```

### `:boolean`

```scl
optional is_active, :boolean {
  default true
}

optional overtime_exempt, :boolean {
  default false
}
```

### `:date`

```scl
required effective_date, :date

optional expiry_date, :date
```

### `:datetime`

```scl
optional check_in_time, :datetime

optional response_deadline, :datetime
```

### `:enum`

> ⚠️ **Enum values MUST NOT contain spaces.** Use underscores.

```scl
# Quoted values (recommended)
required status, :enum {
  values "Pending", "In_Progress", "Completed"
  default "Pending"
}

# Unquoted values (valid if no special chars)
required listing_type, :enum {
  values For_Lease, For_Sale, Sublease
}

required employee_type, :enum {
  values "Regular", "Temporary", "Seasonal", "Contractor"
}
```

### `:json`

```scl
optional validation_errors, :json

optional metadata, :json
```

### `:document`

```scl
required image, :document {
  allowed_types "image/jpeg", "image/png", "application/pdf"
  max_size 5MB
}

optional attachments, :document {
  multiple true
  max_size "5MB"
  allowed_types "application/pdf", "image/jpg", "image/jpeg", "image/png"
}
```

---

## Relationships

### `belongs :to` (Many-to-One)

```scl
# Simple relationship
belongs :to, department {
  required true
}

# Cross-app reference
belongs :to, employee {
  table com_bnv_employee_hub.employee
  required true
}

# With cascade delete
belongs :to, employee {
  required true
  on :delete, :delete
}
```

### `has :many` (One-to-Many)

```scl
# Simple
has :many, invoices

# Explicit table
has :many, documents {
  table employee_document
}

# Self-referential with target_field
has :many, supervised_employees {
  table employee
  target_field supervisor_id
}

# Many-to-many via junction table
has :many, benefits {
  table benefit
  via employee_benefit
}
```

### `has :one` (One-to-One)

```scl
has :one, identity_map {
  table identity_map
}
```

---

## Indexes

```scl
# Simple index
index status { }

# Unique constraint
index email {
  unique true
}

# Composite index
index employee, effective_date {
  unique true
}

# Multi-column composite
index period_start, period_end {
  unique true
}
```

---

## Table Options

```scl
table order, orders {
  default :id, :timestamps, :userstamps

  id_prefix "ORD"          # Custom ID prefix
  display_field name       # Field shown in UI references

  # ... fields ...
}
```

---

## Default Fields

| Directive             | Fields Added               |
| --------------------- | -------------------------- |
| `default :id`         | `id`                       |
| `default :timestamps` | `created_at`, `updated_at` |
| `default :userstamps` | `created_by`, `updated_by` |

Common pattern:

```scl
default :id, :timestamps, :userstamps
```

---

→ **Next:** [Expression Language](./04-expression-language.md)
