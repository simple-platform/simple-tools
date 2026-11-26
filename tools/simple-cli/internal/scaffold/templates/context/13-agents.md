# Agents

An agent does work for a person. This page explains how to create its work, guidance, and allowed tools. See [Tasks](./12-tasks.md) to learn how a task starts.

## Start with the commands

Run commands from the repository root. They create the records and file skeletons. Do not write those by hand. Commands validate before writing, refuse if the app does not exist, and do not duplicate existing records. Existing Markdown and JSON files are kept and reused.

### `simple new agent`

Usage: `simple new agent <app> <name> "<Display name>" [--desc <description>] [--json]`

Creates an agent, its persona file, its knowledge record, and its agent record.

- `<app>`: Existing app ID.
- `<name>`: Lowercase letters, digits, or hyphens, starting with a letter.
- `"<Display name>"`: Required, one line with no double quote, up to 100 characters.
- `--desc <description>`: Optional, one line with no double quote, up to 1,000 characters.
- `--json`: Use the root JSON output format.

### `simple new task-type`

Usage: `simple new task-type <app> <name> "<Plural name>" [--agent <agent-name>] [--tools <a,b>] [--every <duration>] [--no-agent] [--desc <description>] [--json]`

Creates a kind of work, its shape file, and its states file if it cannot reuse one. By default, it also creates the playbook, knowledge record, default-agent link, carried links to the persona and playbook, and links to requested tools.

- `<app>`: Existing app ID.
- `<name>`: Lowercase letters, digits, hyphens, or underscores, starting with a letter. The record keeps the name as typed; hyphens and underscores count as the same name when looking it up.
- `"<Plural name>"`: Required, one line with no double quote, up to 100 characters.
- `--agent <agent-name>`: Set the default agent. You may omit it only when the app has exactly one agent.
- `--tools <a,b>`: Comma-separated platform tools or existing app actions. If a tool is unknown, add it with `simple new tool` first.
- `--every <duration>`: Optional interval, such as `90s`, `30m`, or `1h`. It must be whole seconds and at least one second. Interval work needs a tool that reports changes since its last turn.
- `--no-agent`: Create work that only people do. This writes the kind-of-work record, shape file, and states file. It does not create a playbook, agent link, or carried links. Do not combine it with `--agent`, `--tools`, or `--every`.
- `--desc <description>`: Optional, one line with no double quote, up to 1,000 characters.
- `--json`: Use the root JSON output format.

If the kind of work already exists, the command refuses and points to `simple new skill` or `simple new tool` for additions.

### `simple new skill`

Usage: `simple new skill <app> <name> ["<Title>"] --task-type <task-type-name> [--desc <description>] [--json]`

Creates a skill and links it to a kind of work, or adds a missing link for an existing skill.

- `<app>`: Existing app ID.
- `<name>`: Lowercase letters, digits, or hyphens, starting with a letter.
- `"<Title>"`: Required for a new skill; omit it to link an existing skill. One line with no double quote, up to 255 characters.
- `--task-type <task-type-name>`: Required kind of work to receive the skill.
- `--desc <description>`: Optional, one line with no double quote, up to 1,000 characters. For a new skill, this becomes its Purpose paragraph.
- `--json`: Use the root JSON output format.

A new skill creates `knowledge/skill-<name>.md`, its knowledge record, and a link at the next position. If the skill exists, only a missing link is added. If both exist, the command succeeds and reports that there was nothing to add.

### `simple new knowledge`

Usage: `simple new knowledge <app> <name> ["<Title>"] --task-type <task-type-name> [--json]`

Creates reference knowledge and links it to a kind of work, or adds a missing link for an existing body.

- `<app>`: Existing app ID.
- `<name>`: Lowercase letters, digits, or hyphens, starting with a letter. Do not start with `skill-`, `tool-`, or `playbook-`; those prefixes are for other file types.
- `"<Title>"`: Required for a new body; omit it to link an existing body. One line with no double quote, up to 255 characters.
- `--task-type <task-type-name>`: Required kind of work to receive this material.
- `--json`: Use the root JSON output format.

The new file is `knowledge/<name>.md`. It starts with a title line and a `TODO(simple):` line. Reference knowledge is plain written material linked to a kind of work, such as a list of terms or standing answers. It has no required sections. Use it for material the agent reads. Use a skill for instructions on how to work; a skill has eight required sections.

### `simple new tool`

Usage: `simple new tool <app> <name> ["<Display name>"] [--task-type <task-type-name>] [--lang ts|rust] [--scope <org>] [--env <env>] [--desc <description>] [--json]`

Sets up a tool and its lesson. The optional `--task-type` links it to a kind of work. Without that flag, the tool has no kind-of-work link. A platform tool requires `--task-type`, because there is no app action to set up on its own.

- `<app>`: Existing app ID.
- `<name>`: Platform tool or app action name. An unknown name can be created as a new app action.
- `"<Display name>"`: Optional for a known platform tool or existing action. Required to create a new action. One line with no double quote, up to 100 characters.
- `--task-type <task-type-name>`: Optional kind of work to receive the tool. Required for a platform tool.
- `--lang ts|rust`: Language for a new action. Defaults to TypeScript.
- `--scope <org>`: TypeScript package scope; required for a new TypeScript action. Rust does not use a scope.
- `--env <env>`: New action environment: `server`, `client`, or `both`. Defaults to `server`.
- `--desc <description>`: Optional, one line with no double quote, up to 1,000 characters.
- `--json`: Use the root JSON output format.

A platform tool gets a lookup and a kind-of-work link. An existing app action gets a lesson if it has none, and a kind-of-work link when `--task-type` is set. A new action uses the regular action scaffold, is marked as a tool, and gets a lesson. If the name is neither a platform tool nor an app action and no display name was given, the command refuses and shows the command to create it. When there is nothing left to add, it succeeds and says so.

### Output and safe retries

Text output gives a success line, files waiting for the developer's words, and a reminder to run `simple build <app>`. JSON output has `status`, `app_id`, `name`, `created`, `records`, and `fill_in`. Paths are relative to the repository root. The three lists are empty arrays when empty, never `null`.

Names use lowercase naming rules. Display names, plural names, titles, and descriptions are one line with no double quote and must fit their limits. Record keys must be unique across the app because a key is looked up without its table name.

## Design the work for the person

An agent should take work off a person's hands. Decide what it can safely finish and which final judgments belong to a person. When it asks for help, show the person the facts, the requested outcome, and the exact decision needed.

- **Agent:** Who does the work. Its persona says who it is and how it speaks.
- **Kind of work:** The input, answer, procedure, and allowed tools for one kind of work. Its record is named `task_type`.
- **Playbook:** The business procedure for a kind of work. Write it for the business person who maintains it.
- **Skill:** Instructions for a longer procedure linked to a kind of work.
- **Reference knowledge:** Plain written material linked to a kind of work, such as terms or standing answers. It has no required sections.
- **Tool:** A source of facts or a focused action. An app-declared agent can use only tools linked to its kind of work.
- **Tool lesson:** Guidance for one tool. It says what the tool takes, refuses, and returns.
- **Shape file:** JSON that defines what the kind of work receives and returns.
- **Task:** One piece of work of one kind, assigned to one accountable person. The agent reads and writes with that person's access.

## Files and records

All paths below are under `apps/<app>/`. A missing records file is created with a two-line comment saying what it holds and that `simple new` wrote it. New records follow one blank line. Existing files are not overwritten.

| Command or item | File | Record or content |
| :--- | :--- | :--- |
| `simple new agent` | `knowledge/<kebab-agent>-persona.md` | Persona text. |
| `simple new agent` | `records/70_agent_knowledge.scl` | `persona_<agent>` knowledge record; title is the display name. |
| `simple new agent` | `records/71_agents.scl` | `agent_<agent>` agent record. |
| Agent-owned `simple new task-type` | `knowledge/playbook-<kebab-task-type>.md` | Playbook text. |
| `simple new task-type` | `records/task-types/<snake-task-type>.json` | Input and output shapes. |
| `simple new task-type` | `records/task-types/states.json` | Standard states, created if no reusable states file is available. |
| Agent-owned `simple new task-type` | `records/70_agent_knowledge.scl` | `playbook_<task-type>` knowledge record; title is `<Plural name> playbook`. |
| `simple new task-type` | `records/71_agents.scl` | `task_type_<task-type>` and, for agent-owned work, its default-agent link. |
| `simple new task-type` | `records/72_agent_links.scl` | For agent-owned work, persona and playbook links plus requested tool links. |
| New skill | `knowledge/skill-<skill>.md` | Skill text. An existing file is reused. |
| New skill | `records/70_agent_knowledge.scl` | `skill_<skill>` knowledge record. |
| `simple new skill` | `records/72_agent_links.scl` | Skill link. Existing skill: only a missing link is added. |
| New reference knowledge | `knowledge/<name>.md` | Title and marker line followed by reference text. An existing file is reused. |
| New reference knowledge | `records/70_agent_knowledge.scl` | `knowledge_<name>` knowledge record. |
| `simple new knowledge` | `records/72_agent_links.scl` | Link to the kind of work. Existing body: only a missing link is added. |
| App action used as a tool | `records/72_agent_links.scl` | Tool link, when `--task-type` is provided. |
| Tool without a lesson | `knowledge/tool-<tool>.md` | Tool lesson text. An existing file is reused. |
| Tool without a lesson | `records/70_agent_knowledge.scl` | `tool_<tool>` knowledge record. |
| Tool without a lesson | `records/72_agent_links.scl` | `logic_skill` link to its lesson. |
| New app action used as a tool | Standard action scaffold files | Action files, marked as a tool. |

Record keys use underscores; file names use hyphens, except shape files. Playbook file names start with `playbook-`.

## Write the guidance

The commands leave `TODO(simple):` lines for the developer. Replace every one. `simple build <app>` reports each remaining line and fails until none remain.

Write the persona from the person's point of view. Use the display name as its title, then these headings:

- `Role`
- `Communication Style`

The playbook file is `knowledge/playbook-<kind-of-work>.md`. It has no title and uses these headings:

- `Collaboration`
- `Domain Boundaries`
- `Completion Voice`

Under `Collaboration`, say what the agent completes, which decisions it brings to a person, what the person sees when asked, and which tool supplies each fact. Use business words for the person who maintains the playbook. Name the tables and fields to narrow by. Do not put a script, query, or code in a playbook or skill.

The shape file's `input` must be an object. Set `output` to the fixed result object, or use `null` for a free-text answer. You may leave `output` out; the build treats that the same as `null`.

A skill and a tool lesson use these eight sections in order:

1. Purpose
2. When To Use
3. Required Context
4. Procedure
5. Good Examples
6. Bad Examples And Why
7. Error Recovery
8. Completion Checklist

Keep the generated `Slug:` line. Reference knowledge is for material to read, such as a list of terms or standing answers. It has no required sections.

## Choose tools for the facts

Every fact the playbook asks the agent to reason about or report must come from a tool. The agent does not work out current state by comparing what it remembers, and it does not count by reading records. For simple reads, link both `introspect-data` and `process-data` first. Name the tables to look in and fields to narrow by in the playbook. Add a custom tool only when these are not enough. Give each kind of work only the tools it needs. Think twice before linking `mutate-data`; prefer a focused action for one change, or ask a person to make the change.

Known platform tools, provided by app `dev.simple.system`:

- `introspect-data`
- `process-data`
- `mutate-data`
- `load-knowledge`
- `read-tool-result`
- `read-task-output`

## Build and deploy checks

When building an app or all apps, `simple build` checks agents after building actions. A single action or space build does not run these checks. An app with agent records and no actions or spaces is still a valid build target. `simple deploy` runs the same checks before collecting files and refuses if they find a problem.

The marker scan runs for every app. It reports each `TODO(simple):` line in `knowledge/**/*.md`, `records/**/*.scl`, `records/**/*.json`, and app action sources under `actions/*/src/**`.

Wiring and structure checks run when the app has at least one agent or kind-of-work record:

- Each agent points to a knowledge record in the app with a body.
- A kind of work with no linked agent has no default-agent, playbook, or carried-link checks. It may be work for people or have its agent chosen when the task starts.
- Other kinds of work have exactly one default link with role `executor` and `is_default true`, and that agent is in the app. They point to a playbook knowledge record with a body and carry both the default agent's persona and the playbook.
- Each shape file exists and is valid JSON. Its `input` must be an object. Its `output`, when present, must be an object or `null`; missing `output` means the same as `null`. A referenced states file must be valid JSON and name `queued`, `working`, `waiting_for_user`, `waiting_for_event`, `completed`, `cancelled`, and `failed`.
- Each tool link names a kind of work in the app and a known platform tool or an app action whose `action.json` marks it as a tool. Each app action marked as a tool has a lesson with a body.
- Each skill and lesson link points to a record that exists. An unreadable reference is reported with a rule that explains how to write it.
- Persona and playbook structure are checked together for each kind of work, with the persona first. A carried body that is neither half is checked as a skill only when its file name starts with `skill-` or it has a `Slug:` line. The file name and slug must agree; a `skill-` file without a `Slug:` line is a problem. Other linked bodies are reference knowledge and need only be non-empty, valid text. Every tool lesson gets the full skill check.

Each problem is printed as `<file>:<line>: <rule>`. The path is relative to the repository root. Lines start at 1, or use 0 for a whole-file problem. Problems use plain words, are sorted by file and line, and contain no duplicates. Fix the named file and line, then build again.

## Worked example

For `com.acme.shop`, create agent `support` with display name `Support Agent` and kind of work `returns` with plural name `Returns`. Start with `simple new agent`, then `simple new task-type`; link `introspect-data` and `process-data` for order reads. Name the `orders` table and the identifying and return-status fields in the playbook. Add the app tool `find-order` for a focused lookup the platform tools do not provide. Add `refund-policy` as a skill for the longer policy procedure, and `return-terms` as reference knowledge for terms and standing answers. Complete every generated `TODO(simple):` line and repeat `simple build com.acme.shop` until it reports no problems.
