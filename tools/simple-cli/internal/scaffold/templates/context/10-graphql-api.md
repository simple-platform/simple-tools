# GraphQL API

The platform auto-generates a GraphQL API from your SCL schema.

## Introspection Endpoint

AI agents and LLMs can inspect the full schema, available operations, and types using the introspection endpoint:

**Endpoint:** `https://graph.<instance-domain>`

**Browser Interface:** `https://graph.<instance-domain>/graphiql.html`

### Fetching Schema

To fetch the full schema JSON (introspection), send a POST request with the operation name `IntrospectionQuery`:

```json
{
  "operationName": "IntrospectionQuery"
}
```

> [!TIP]
> This endpoint supports standard GraphQL introspection queries. You can point tools like curl, GraphQL Playground, Insomnia, or Postman to this URL to explore your API interactively.

---

## Query Patterns

### List Records

```graphql
query GetOrders($limit: Int!, $offset: Int!) {
  orders: com_mycompany_myapp__orders(
    limit: $limit
    offset: $offset
    order_by: { created_at: desc }
  ) {
    id
    total
    status
    customer { name }
  }
}
```

### Single Record

```graphql
query GetOrder($id: ID!) {
  order: com_mycompany_myapp__order(id: $id) {
    id
    total
    status
    line_items { product_name quantity }
  }
}
```

### Filtered Query

```graphql
query GetPendingOrders($status: JSON!) {
  orders: com_mycompany_myapp__orders(
    where: { status: { _eq: $status } }
  ) {
    id
    total
  }
}
```

---

## Mutation Patterns

### Insert Single

```graphql
mutation CreateOrder($data: JSON!) {
  order: insert_com_mycompany_myapp__order(object: $data) {
    id
  }
}
```

### Insert Batch

```graphql
mutation CreateOrders($data: [JSON!]!) {
  orders: insert_com_mycompany_myapp__orders(objects: $data) {
    id
  }
}
```

### Update

```graphql
mutation UpdateOrder($id: ID!, $data: JSON!) {
  order: update_com_mycompany_myapp__order(id: $id, _set: $data) {
    id
    status
  }
}
```

### Delete

```graphql
mutation DeleteOrder($id: ID!) {
  order: delete_com_mycompany_myapp__order(id: $id) {
    id
  }
}
```

---

---

## Aggregation Queries

Perform calculations like counts, sums, or averages directly in the database. You can request both aggregations and the actual data nodes in a single query.

### Supported Functions

| Function | Description | Example |
|----------|-------------|---------|
| `count`  | Count records (defaults to `*`) | `count` or `count(columns: id)` |
| `sum`    | Sum of numeric values | `sum { total }` |
| `avg`    | Average of numeric values | `avg { rating }` |
| `min`    | Minimum value | `min { price }` |
| `max`    | Maximum value | `max { created_at }` |

### Basic Aggregation

Get simple stats about a table.

```graphql
query OrderStats {
  orders_agg {
    aggregate {
      count
      sum { total }
      avg { rating }
    }
  }
}
```

### Aggregation + Nodes

Fetch both statistics AND the records in a single request. This is highly efficient as it avoids two separate network calls.

```graphql
query OrdersAndStats {
  orders_agg(where: { status: { _eq: "pending" } }) {
    aggregate {
      count
      sum { total }
    }
    nodes {
      id
      total
      status
    }
  }
}
```

### Nested Aggregation

You can also aggregate related records nested within a parent query.

```graphql
query UserPostStats {
  users {
    id
    name
    posts_agg {
      aggregate {
        count
        avg { rating }
      }
    }
  }
}
```

---

## Advanced Querying: Filters & Pagination

All list and aggregation fields (root or nested) support `where`, `limit`, `offset`, and `order_by`.

### 1. Regular List with Controls
Fetch active orders, most recent first, with pagination.

```graphql
query RecentActiveOrders {
  orders(
    where: { status: { _eq: "active" } }
    order_by: { created_at: desc }
    limit: 10
    offset: 0
  ) {
    id
    total
    created_at
  }
}
```

### 2. Regular Nested List
Fetch users and their specific recent orders.

```graphql
query UsersWithRecentOrders {
  users(limit: 5) {
    name
    # Filter and paginate nested relationships
    orders(
      where: { total: { _gt: 100 } }
      order_by: { total: desc }
      limit: 3
    ) {
      id
      total
    }
  }
}
```

### 3. Root Aggregation with Filters
Count only the orders that match a specific criteria.

```graphql
query CountHighValueOrders {
  orders_agg(
    where: { total: { _gt: 1000 } }
  ) {
    aggregate {
      count
    }
  }
}
```

### 4. Nested Aggregation with Filters
For each user, calculate stats only for their "completed" orders.

```graphql
query UserCompletionStats {
  users {
    name
    # Aggregation on filtered relationship
    posts_agg(
      where: { status: { _eq: "published" } }
    ) {
      aggregate {
        count
        avg { rating }
      }
    }
  }
}
```

### 5. Mixed: Pagination + Aggregates
**Powerful:** Filter a dataset, get the TOTAL count of matches (aggregate), but only fetch the FIRST PAGE of actual data (nodes).

```graphql
query SearchAndPaginate {
  orders_agg(
    where: { status: { _eq: "pending" } }
    order_by: { created_at: desc }
    limit: 20  # Applies to 'nodes'
    offset: 0  # Applies to 'nodes'
  ) {
    # 1. Total count matching the filter (ignoring limit!)
    aggregate {
      count
    }
    # 2. The actual page of data (respected limit)
    nodes {
      id
      status
      created_at
    }
  }
}
```

---

## Filter Operators

| Operator | Example |
|----------|---------|
| `_eq` | `{ status: { _eq: "active" } }` |
| `_neq` | `{ status: { _neq: "deleted" } }` |
| `_gt` | `{ amount: { _gt: 100 } }` |
| `_gte` | `{ amount: { _gte: 100 } }` |
| `_lt` | `{ amount: { _lt: 1000 } }` |
| `_lte` | `{ amount: { _lte: 1000 } }` |
| `_in` | `{ status: { _in: ["a", "b"] } }` |
| `_is_null` | `{ email: { _is_null: false } }` |
| `_and` | `{ _and: [{ a: ... }, { b: ... }] }` |
| `_or` | `{ _or: [{ a: ... }, { b: ... }] }` |

---

## Naming Convention

| Operation | Pattern |
|-----------|---------|
| List query | `app__plural_name` |
| Single query | `app__singular_name` |
| Insert single | `insert_app__singular_name` |
| Insert batch | `insert_app__plural_name` |
| Update | `update_app__singular_name` |
| Delete | `delete_app__singular_name` |

---

## Using in Actions

```typescript
import simple, { type Request } from '@simpleplatform/sdk'
import { query, mutate } from '@simpleplatform/sdk/graphql'

simple.Handle(async (request: Request) => {
  // Query
  const result = await query<{ orders: Order[] }>(
    `query { orders: com_mycompany_myapp__orders { id total } }`,
    {},
    request.context
  )

  // Mutation
  await mutate(
    `mutation UpdateOrder($id: ID!, $data: JSON!) {
      order: update_com_mycompany_myapp__order(id: $id, _set: $data) { id }
    }`,
    { id: 'ORD000123', data: { status: 'Complete' } },
    request.context
  )
})
```

---

→ **Next:** [SDK Reference](./11-sdk-reference.md)
