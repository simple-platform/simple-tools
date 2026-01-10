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
| **[Simple CLI](./tools/simple-cli/)** | Command-line interface for scaffolding applications and actions | [📖 Full Documentation](./tools/simple-cli/README.md) |

---

## Quick Start

### Prerequisites

| Requirement | Version | Purpose |
|-------------|---------|---------|
| [Go](https://golang.org/) | ≥1.25 | CLI compilation |
| [Node.js](https://nodejs.org/) | ≥25 | Package management |
| [pnpm](https://pnpm.io/) | ≥10 | Monorepo orchestration |

### Install Simple CLI

```bash
# Clone the repository
git clone https://github.com/simple-platform/simple-tools.git
cd simple-tools

# Build Simple CLI
pnpm go:build

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
| `pnpm go:build` | Build Simple CLI binary |
| `pnpm go:tidy` | Tidy Go module dependencies |
| `pnpm test:go` | Run Go tests with coverage |
| `pnpm lint:go` | Lint Go code with golangci-lint |
| `pnpm lint` | Run all linters (JS + Go) |

---

## Repository Structure

```
simple-tools/
├── tools/
│   └── simple-cli/            # Go CLI application
│       └── README.md          # CLI documentation
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
| [Simple Platform](https://github.com/simple-platform/simple) | Core platform (Elixir backend) |
| [Simple SDKs](https://github.com/simple-platform/simple-sdks) | TypeScript SDK for Actions |

---

## License

Apache 2.0 — see [LICENSE](./LICENSE) for details.

---

<p align="center">
  <strong>Built with ❤️ by the Simple Platform team</strong><br>
  <a href="https://simple.dev">simple.dev</a>
</p>
