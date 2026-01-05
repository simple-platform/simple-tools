# Simple Platform Overview

Welcome to the Simple Platform. This documentation provides the context you need to build applications effectively.

## What is Simple Platform?

Simple Platform is an enterprise application development platform that transforms how you build business applications. Instead of managing databases, writing data access code, or building APIs, you declare your business model and the platform handles the rest.

## Core Architecture

The platform is built on three integrated layers:

```
┌─────────────────────────────────────────────────────────────┐
│                    YOUR APPLICATION                         │
├─────────────────────────────────────────────────────────────┤
│  LOGIC LAYER                                                │
│  ┌─────────────────────────────────────────────────────────┐│
│  │  Actions (Go/TypeScript → WASM)                         ││
│  │  • Execute on both Client (Browser) AND Server          ││
│  │  • Secure sandbox with no filesystem/network access     ││
│  │  • Record Behaviors for form logic                      ││
│  └─────────────────────────────────────────────────────────┘│
├─────────────────────────────────────────────────────────────┤
│  DATA LAYER                                                 │
│  ┌─────────────────────────────────────────────────────────┐│
│  │  SCL (Simple Configuration Language)                    ││
│  │  • Declarative schema definition                        ││
│  │  • Auto-generated GraphQL API                           ││
│  │  • Tenant-isolated PostgreSQL                           ││
│  └─────────────────────────────────────────────────────────┘│
├─────────────────────────────────────────────────────────────┤
│  FOUNDATIONS                                                │
│  • Kubernetes orchestration                                 │
│  • Multi-tenant isolation                                   │
│  • Elixir/OTP for fault tolerance                           │
└─────────────────────────────────────────────────────────────┘
```

## Learning Path

Follow these documents in order:

| Order | Document                                                 | What You'll Learn         |
| ----- | -------------------------------------------------------- | ------------------------- |
| 1     | **This document**                                        | Platform overview         |
| 2     | [SCL Grammar & Syntax](./02-scl-grammar.md)              | Syntax basics & grammar   |
| 3     | [Data Layer: SCL](./03-data-layer-scl.md)                | Schema definitions        |
| 4     | [Expression Language](./04-expression-language.md)       | `$var()`, `$jq()`, piping |
| 5     | [App Records Overview](./05-app-records-overview.md)     | All record types          |
| 6     | [Metadata Configuration](./06-metadata-configuration.md) | Display names, positions  |
| 7     | [Actions and Triggers](./07-actions-and-triggers.md)     | Server logic, scheduling  |
| 8     | [Record Behaviors](./08-record-behaviors.md)             | Form logic                |
| 9     | [Custom Views](./09-custom-views.md)                     | UI customization          |
| 10    | [GraphQL API](./10-graphql-api.md)                       | Queries and mutations     |
| 11    | [SDK Reference](./11-sdk-reference.md)                   | Complete API docs         |

## Key Concepts

### Write Once, Run Anywhere

The most powerful feature of Simple Platform. Your Actions (business logic) compile to WebAssembly and execute:

1. **In the browser** — For instant UI feedback without network roundtrips
2. **On the server** — For authoritative validation before data is saved

It is architecturally impossible for client-side validation to diverge from server-side rules.

### Application Structure

```
apps/com.mycompany.myapp/
├── app.scl              # App metadata
├── tables.scl           # Data schema (SCL)
├── actions/             # Server + Client logic (WASM)
├── scripts/             # Record Behaviors
└── records/             # Record configurations
```

---

→ **Next:** [SCL Grammar & Syntax](./02-scl-grammar.md)
