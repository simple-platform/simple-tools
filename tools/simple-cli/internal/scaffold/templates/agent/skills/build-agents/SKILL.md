---
name: build-agents
description: Guide the design and setup of agents, their tools, and kinds of work.
---

# Build Agents Skill

Slug: build-agents

## Purpose

Create agents that take work off people's hands and leave people with the final judgments that need their attention. Give each kind of work only the facts and tools it needs.

## When To Use

Use this when a developer asks to create an agent, add a kind of work, or describe an agent in conversation. Follow `workflows/create-agent.md` for the commands.

## Required Context

Know the app, the person whose work is being improved, the desired outcome, the decisions that need a person, and the facts the agent must have. Read the app's data model to name the right tables and fields. Use the CLI reference for command flags.

## Procedure

1. Start with the person's goal. Identify what the agent can safely finish and which judgment needs a person.
2. Create the agent and its kind of work with `simple new agent` and `simple new task-type`. For work only people do, create the kind of work with `--no-agent`. Do not write records or file skeletons by hand.
3. Complete the generated persona, playbook, and shape file. Replace every `TODO(simple):` line. The playbook file is `knowledge/playbook-<kind-of-work>.md`.
4. Make a fact list from the playbook. Each fact the agent must reason about or report needs a tool that supplies it. The agent must not compare state from memory or count by reading.
5. For simple reads, link `introspect-data` and `process-data` together and name the tables and fields to narrow by. Add a custom tool only when these tools are not enough. For interval work, provide a tool that reports changes since the prior turn.
6. Give the kind of work only the tools it needs. Think twice before linking `mutate-data`; prefer a focused action for one change or a person to make it.
7. Add a skill for a longer procedure and a tool lesson for each tool. Keep the generated `Slug:` line. Both use the eight required sections in order. Write them in business words, without scripts, queries, or code.
8. Add reference knowledge when the agent needs plain written material, such as a list of terms or standing answers. Create it with `simple new knowledge <app> <name> "<Title>" --task-type <task-type-name>`. It has no required sections. Use a skill for instructions on how to work; use reference knowledge for material the agent reads.
9. Run `simple build <app>`. Fix each listed marker or structure problem, then build again. A linked body is checked as a skill only when its file starts with `skill-` or has a `Slug:` line. The file name and slug must agree; a `skill-` file without a `Slug:` line is a problem. Tool lessons always get the full skill check. Other linked bodies are reference knowledge and need only non-empty, valid text. Deploy only after the build is clear.
10. When the developer describes the agent in conversation, run the commands and draft its persona and playbook from that description. Ask the developer to decide only things that require their judgment.

## Good Examples

- Give return work order lookup and order-field reads. Name the `orders` table and the fields that identify the order and determine its return status in the playbook.
- Ask a person to decide a policy exception. Show the person the order facts, the customer's request, what is missing or unusual, and the specific decision needed.
- Store return terms and standing answers as reference knowledge. Give the kind of work a policy skill when it needs steps for applying the policy.
- Create people-only work with `--no-agent`. It has a shape and states, but no agent, playbook, or carried links.
- Give interval work a tool that reports what changed since its last turn.

## Bad Examples And Why

- Let the agent decide from what it remembers about an earlier task. Memory is not a source of current facts.
- Link a broad write tool to every kind of work. Each kind should have only the tools it needs, and one-purpose actions are safer for changes.
- Ask an agent to count records by reading them. A tool must return the needed count.
- Put a script, query, or code in a playbook or skill. These files explain business steps in words.
- Make an agent guess at a decision that belongs to a person. State what it can complete and what it must ask.

## Error Recovery

If a command refuses, read its reason and provide the missing app, agent, kind of work, display name, or tool. A rerun does not duplicate records. If `simple build` reports a `TODO(simple):` line, replace it. For a wiring or structure problem, correct the named file and line, then build again. Do not work around a missing fact by guessing.

## Completion Checklist

- The persona has the display name as its title, then `Role` and `Communication Style`.
- Agent-owned work has a playbook with `Collaboration`, `Domain Boundaries`, and `Completion Voice`. It says which tool supplies each needed fact.
- People-only work was created with `--no-agent` and has no agent, playbook, or carried links.
- The shape file defines the handed-in input and fixed output. A missing or null `output` means a free-text answer.
- Every fact comes from an available tool, and each kind of work has only the tools it needs.
- Each tool lesson and skill has the eight sections in order and retains its generated `Slug:` line.
- Reference knowledge contains useful written material without a required section layout.
- The app build reports no remaining markers or agent problems.
