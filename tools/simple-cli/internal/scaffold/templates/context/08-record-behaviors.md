# Record Behaviors

Record Behaviors are JavaScript scripts attached to tables that define form logic. They compile to WASM and execute on **both client and server**.

---

## Registering a Record Behavior

````scl
var metadata {
  query ```
  query {
    tables: dev_simple_system__tables(
      where: {
        application_id: {_eq: "com.mycompany.myapp"}
        name: {_eq: "order"}
      }
    ) {
      id
    }
  }
  ```
}

set dev_simple_system.record_behavior, order_behavior {
  table_id `$var('metadata') |> $jq('.tables[0].id')`
  script `$file('scripts/record-behaviors/order.js')`
}
````

---

## Script Location and Signature

Scripts are stored in `scripts/record-behaviors/`:

```
your-app/
├── scripts/
│   └── record-behaviors/
│       └── order.js
```

### Script Signature

```javascript
export default async ({ $ai, $db, $form, $user }) => {
  // Your logic here
}
```

---

## Events

| Event | Execution | Purpose |
|-------|-----------|---------|
| `load` | Client + Server | Set defaults for new records |
| `update` | Client only | React to field changes in real-time |
| `submit` | Client + Server | Validate before save |

---

## The `$form` API

### Form-Level Methods

```javascript
$form.event              // Current event: 'load', 'update', 'submit'
$form.record()           // Get all current field values
$form.updated('field')   // Check if field changed (update event)
$form.error('message')   // Set form-level error (blocks save)
$form.info('message')    // Set form-level info message
```

### Field-Level Methods (Chainable)

```javascript
$form('status').value()        // Get value
$form('status').set('Active')  // Set value
$form('notes').visible(false)  // Hide field
$form('email').required(true)  // Make required
$form('total').editable(false) // Make read-only
$form('name').error('Required') // Show field error
$form('name').info('Hint text') // Show field info
```

---

## The `$db` API

Secure, **read-only** GraphQL queries.

> [!WARNING]
> To prevent infinite loops and side-effects, **mutations are NOT allowed** in Record Behaviors.
> If you need to update other data when a record changes, use [Database Events](./07-actions-and-triggers.md#database-event-trigger).

```javascript
const { product } = await $db.query(
  `query getProduct($id: ID!) {
    product: com_mycompany_myapp__product(id: $id) {
      price
      tax_rate
    }
  }`,
  { id: productId }
)
```

---

## The `$user` API

```javascript
$user.id      // Current user ID
$user.name    // Full name
$user.email   // Email address
```

---

## Examples

### Auto-Calculate Total

```javascript
export default async ({ $form }) => {
  if ($form.event === 'update' && $form.updated('quantity', 'unit_price')) {
    const qty = $form('quantity').value() || 0
    const price = $form('unit_price').value() || 0
    $form('total').set(qty * price)
  }
}
```

### Default Values on Load

```javascript
export default async ({ $form, $user }) => {
  if ($form.event === 'load') {
    if (!$form('assigned_to').value()) {
      $form('assigned_to').set($user.id)
    }
    $form('status').set('Draft')
  }
}
```

### Validation on Submit

```javascript
export default async ({ $form }) => {
  if ($form.event === 'submit') {
    const start = $form('start_date').value()
    const end = $form('end_date').value()

    if (end && start && new Date(end) < new Date(start)) {
      $form('end_date').error('End date must be after start date')
    }
  }
}
```

---

## Sandbox Limitations

**Cannot use:** `window`, `document`, `fetch()`, `setTimeout()`, Node.js APIs

**Must use:** Standard JavaScript + `$form`, `$db`, `$user`, `$ai`

---

→ **Next:** [Custom Views](./09-custom-views.md)
