# {{.ProjectName}}

A Simple Platform monorepo.

## Directory Structure

```
{{.ProjectName}}/
├── .simple/context/  # Platform documentation & AI context
├── apps/             # Application packages
├── AGENTS.md         # AI coding standards & guides
└── README.md
```

## Commands

| Command | Description |
| :--- | :--- |
| `simple new app <app> <name>` | Create a new application |
| `simple new action <app> <name> --lang ts` | Scaffold a new Action |
| `simple build <app>` | Build all Actions in an app |
| `simple deploy <app>` | Deploy application (Schema + Logic) |

## Getting Started

### 1. Create a New App

Scaffold a new application package:

```bash
simple new app com.mycompany.crm CRM
```

This creates the app structure in `apps/com.mycompany.crm` with a default `app.scl`.

### 2. Define Schema

Edit `apps/com.mycompany.crm/tables.scl` to define your data model:

```ruby
table contact {
  required name, :string
  required email, :string
}
```

### 3. Add Logic (Optional)

Create a server-side action:

```bash
simple new action com.mycompany.crm import-contacts --lang ts
```

### 4. Deploy

Deploy your changes to the active environment:

```bash
simple deploy com.mycompany.crm
```

## Documentation

*   **AI Agents:** See [AGENTS.md](./AGENTS.md) for detailed coding rules.
*   **Platform Ref:** See `.simple/context/` for SCL grammar, SDK reference, and more.
