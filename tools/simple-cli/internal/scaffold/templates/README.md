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

| Command                                                            | Description                          |
| :----------------------------------------------------------------- | :----------------------------------- |
| `simple new app <app> <name>`                                      | Create a new application             |
| `simple new action <app> <name> --lang ts\|rust`                   | Scaffold a new Action                |
| `simple new agent <app> <name> "<Display name>"`                   | Create an agent and persona          |
| `simple new task-type <app> <name> "<Plural name>" [--no-agent]`   | Create a kind of work                |
| `simple new tool <app> <name> [--task-type <type>]`                | Set up a tool and optionally link it |
| `simple new skill <app> <name> ["<Title>"] --task-type <type>`     | Add a skill to a kind of work        |
| `simple new knowledge <app> <name> ["<Title>"] --task-type <type>` | Add reference knowledge              |
| `simple build <app>`                                               | Build all Actions in an app          |
| `simple lint [target]`                                             | Lint the workspace or a target       |
| `simple deploy <app>`                                              | Deploy application (Schema + Logic)  |

## Getting Started

### 1. Create a New App

Scaffold a new application package:

```bash
simple new app com.mycompany.crm CRM
```

This creates the app structure in `apps/com.mycompany.crm` with a default `app.scl`.

### 2. Define Schema

Edit `apps/com.mycompany.crm/tables.scl` to define your data model:

```scl
table contact {
  required name, :string
  required email, :string
}
```

### 3. Add Logic (Optional)

Create a server-side action, in TypeScript or in Rust:

```bash
simple new action com.mycompany.crm import-contacts --lang ts --scope mycompany
simple new action com.mycompany.crm import-contacts --lang rust
```

### 4. Deploy

Deploy your changes to the active environment:

```bash
simple deploy com.mycompany.crm
```

## Documentation

- **AI Agents:** See [AGENTS.md](./AGENTS.md) for detailed coding rules.
- **Platform Ref:** See `.simple/context/` for SCL grammar, SDK reference, and more.
- **Agent Guidance:** See `.simple/context/13-agents.md`, `.agent/workflows/create-agent.md`, and `.agent/skills/build-agents/SKILL.md` for command options and steps.

## Git Checks

The Git hooks check staged files before a commit, validate commit messages, and check the whole workspace before a push. Checkout, merge, and rewrite hooks also scan the whole tree. The supply-chain guard blocks hidden code after long whitespace runs and package install scripts; the pre-push hook also runs `simple lint`.

Never bypass a check with `--no-verify`. If the guard names a source file, restore a trusted version; if it names an install script, remove it unless the guard has an explicit reviewed exception. Fix ESLint or `simple lint` findings and stage the corrected files. Use commit messages such as `feat(com.acme.demo): add a greeting action`; keep the subject lower-case, at most 72 characters, and without a final period. If `node`, `npx`, or `simple` is unavailable, enter the development environment with `devbox shell` before running Git.
