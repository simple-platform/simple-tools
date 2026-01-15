<h1 align="center">Simple Tools</h1>

<p align="center">
  <strong>Developer tooling for the Simple Platform</strong><br>
  <em>Build enterprise applications faster</em>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
</p>

---

## What is Simple Platform?

**Simple Platform** is an enterprise application development platform built on schema-first principles. Define your data model in human-readable configuration files, and the platform automatically generates:

- ✅ **GraphQL APIs** — Full CRUD operations, filtering, pagination
- ✅ **UI Forms & Lists** — Auto-generated from your schema
- ✅ **Validation** — Same rules run on client AND server (via WebAssembly)
- ✅ **Triggers & Webhooks** — Schedule actions or respond to events

This repository contains the official developer tools to build, test, and deploy Simple Platform applications.

---

## Available Tools

| Tool | Description | Documentation |
|------|-------------|---------------|
| **[Simple CLI](./tools/simple-cli/)** | Command-line interface for scaffolding and building applications | [📖 Full Documentation](./tools/simple-cli/README.md) |
| **[SCL Parser CLI](./tools/scl_parser_cli/)** | Standalone CLI for parsing SCL files to JSON | [📖 Full Documentation](./tools/scl_parser_cli/README.md) |
| **[SCL Parser](./packages/scl_parser/)** | Elixir library for SCL parsing | [📖 Full Documentation](./packages/scl_parser/README.md) |

---

## Quick Start

### Prerequisites

This project is configured with [Devbox](https://www.jetify.com/devbox) to ensure a consistent development environment.

1. Install Devbox.
2. Run `devbox shell` at the root of the repository to load all necessary tools (Go, Node.js, Elixir, etc.).

### Install Simple CLI

```bash
# Clone the repository
git clone https://github.com/simple-platform/simple-tools.git
cd simple-tools

# Build all tools (Simple CLI, SCL Parser CLI)
pnpm build

# Verify installation
./tools/simple-cli/simple --help
```

### Create Your First Project

```bash
# Initialize a workspace
./tools/simple-cli/simple init my-first-app
cd my-first-app

# Create an application
../tools/simple-cli/simple new app com.mycompany.hello "Hello World"

# View generated structure
tree apps/
```

**For detailed usage**, see the [Simple CLI Documentation](./tools/simple-cli/README.md).

---

## Development Commands

Run these from the monorepo root:

| Command | Description |
|---------|-------------|
| `pnpm build` | Build all tools (JS/Go/Elixir) via Turbo |
| `pnpm test` | Run all tests (Go/Elixir) via Turbo |
| `pnpm lint` | Run all linters (JS/Go/Elixir) via Turbo |
| `pnpm tidy` | Tidy dependencies |

---

## Repository Structure

```
simple-tools/
├── tools/
│   ├── simple-cli/            # Go CLI application
│   └── scl_parser_cli/        # Elixir CLI for SCL Parser
├── packages/
│   └── scl_parser/            # Elixir parser library
├── AGENTS.md                  # AI coding guidelines
├── package.json               # Monorepo scripts
└── pnpm-workspace.yaml        # Workspace config
```

---

## For Contributors

We follow strict engineering standards. Before contributing:

1. **Read** [AGENTS.md](./AGENTS.md) for coding guidelines
2. **Write tests** — 90%+ coverage on business logic
3. **Run checks** — `pnpm lint && pnpm test`
4. **Use conventional commits** — `feat:`, `fix:`, `docs:`, etc.

### Pull Request Checklist

- [ ] Tests pass locally (`go test ./...`)
- [ ] Linting passes (`golangci-lint run ./...`)
- [ ] Coverage maintained/improved
- [ ] Documentation updated if needed

---

## Related Projects

| Project | Description |
|---------|-------------|
| [Simple SDKs](https://github.com/simple-platform/simple-sdks) | TypeScript SDK for Actions |

---

## License

Apache 2.0 — see [LICENSE](./LICENSE) for details.

---

<p align="center">
  <strong>Built with ❤️ by the Simple Platform team</strong><br>
  <a href="https://simple.dev">simple.dev</a>
</p>
