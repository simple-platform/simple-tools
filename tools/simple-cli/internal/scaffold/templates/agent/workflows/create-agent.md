---
description: Create and connect an agent, its kinds of work, tools, and guidance.
---

# Workflow: Create an Agent

## Steps

1. Start with the commands. From the repository root, create the agent and the kind of work it handles:

   ```bash
   simple new agent <app> <agent-name> "<Display name>" --desc "<description>"
   simple new task-type <app> <task-type-name> "<Plural name>" --agent <agent-name> --tools <tool-a,tool-b>
   ```

   Add `--every <duration>` only when the work must wake on an interval. If the app has one agent, `--agent` may be left out; otherwise name the agent. For work only people do, use `--no-agent` instead. That mode cannot be combined with `--agent`, `--tools`, or `--every`.

2. Open the files the commands created: the persona, playbook, and shape file. Replace each `TODO(simple):` line. Do not write these files or their records from scratch. If a command refuses, read its reason. Running it again does not duplicate records or files.

3. Write the persona from the person's point of view. Use the display name as the title, followed by `Role` and `Communication Style`. Say who the agent is, what work it handles, and how it speaks.

4. Write the playbook for the business person who maintains it. The file is `knowledge/playbook-<kind-of-work>.md`. Do not add a title. Use `Collaboration`, `Domain Boundaries`, and `Completion Voice`. Explain what the agent can finish, what it brings to a person, what the person sees when asked, and which tool supplies each fact. Never put a script, query, or code in a playbook.

5. Set the shape file's `input` to the information handed to the work. Set `output` to the fixed result the agent must return. Leave `output` out or set it to `null` for a free-text answer.

6. Add a tool for each fact the playbook needs that its linked tools cannot provide:

   ```bash
   simple new tool <app> <tool-name> "<Display name>" --task-type <task-type-name> --lang ts --scope <org> --env server --desc "<description>"
   ```

   Use `--task-type` to link the tool to this kind of work. Without that flag, the command sets up the tool and its lesson without linking it to a kind of work. A platform tool needs `--task-type`. For simple reads, first link `introspect-data` and `process-data` together. Name the tables and fields to narrow by in the playbook. Build a custom tool only when those platform tools are not enough. Give each kind of work only the tools it needs. Think twice before linking `mutate-data`; prefer an action for one change or ask a person to make it. The tool lesson says what the tool takes, refuses, and returns.

7. Add a skill for a longer procedure that should travel with this kind of work:

   ```bash
   simple new skill <app> <skill-name> "<Title>" --task-type <task-type-name> --desc "<purpose>"
   ```

   Keep the generated `Slug:` line. Use the eight required sections in order: Purpose, When To Use, Required Context, Procedure, Good Examples, Bad Examples And Why, Error Recovery, Completion Checklist. Never put a script, query, or code in a skill.

8. Add reference knowledge for material the agent should read, such as a list of terms or standing answers:

   ```bash
   simple new knowledge <app> <name> "<Title>" --task-type <task-type-name>
   ```

   Reference knowledge is plain written material. It has no required sections. Use it when the agent needs facts or reference material, not a procedure for how to work. A skill describes a way of working and uses the eight sections above.

9. Run `simple build <app>`. Replace every `TODO(simple):` item it reports, then build again. Fix any reported wiring or file-structure problem before continuing. A linked body is checked as a skill only when its file name starts with `skill-` or it has a `Slug:` line. The file name and slug must agree; a `skill-` file without a `Slug:` line is a problem. Tool lessons always get the full skill check. Other linked bodies are reference knowledge and only need non-empty, valid text.

10. Deploy the app after the build is clear:

    ```bash
    simple deploy apps/<app> --env <env>
    ```

    The deploy command runs the same agent checks before it collects files.

## Worked example

Create a support agent for return questions. Its kind of work can read order facts with the two platform tools. It also needs a focused order lookup, a longer refund policy procedure, and reference material with return terms.

1. Run the agent and kind-of-work commands first:

   ```bash
   simple new agent com.acme.shop support "Support Agent" --desc "Answers customer questions about orders."
   simple new task-type com.acme.shop returns "Returns" --agent support --tools introspect-data,process-data --desc "Decides what happens to one returned order."
   ```

2. Replace the TODO lines in the created files:

   - In `knowledge/support-persona.md`, use `Support Agent` as the title. Under `Role`, say the agent handles one customer's return question. Under `Communication Style`, ask it to be clear, calm, and direct.
   - In `knowledge/playbook-returns.md`, add the three required headings. Under `Collaboration`, say it completes requests that meet the published policy and brings policy exceptions, conflicting order details, or missing identity proof to a person. When asking, show the order, the facts found, the requested outcome, and the exact decision needed. Say that `find-order` supplies matched order facts, while `introspect-data` and `process-data` supply order fields from the `orders` table, narrowed by `id` and `customer_id`.
   - In `records/task-types/returns.json`, make `input` an object with the order reference and the customer's question. Make `output` an object with the decision and customer reply. Include no extra fields the work does not need.

3. Add the focused lookup tool, policy skill, and reference knowledge:

   ```bash
   simple new tool com.acme.shop find-order "Find Order" --task-type returns --lang ts --scope acme --env server --desc "Finds one order and returns its return facts."
   simple new skill com.acme.shop refund-policy "Refund Policy" --task-type returns --desc "Explains how to apply the return and refund policy to one order."
   simple new knowledge com.acme.shop return-terms "Return Terms" --task-type returns
   ```

   Complete the generated tool lesson with its inputs, refusal cases, and returned order facts. Write the skill's eight sections in business words. Put the terms and standing answers in the reference knowledge body. Keep scripts, queries, and code out of the playbook and skill.

4. Replace every remaining `TODO(simple):` line. Run `simple build com.acme.shop` again after each reported fix until it reports no problems, then deploy with the environment chosen for the app.

## When the developer describes an agent

Run the commands and draft the persona and playbook from the developer's description. Ask the developer to decide only things that require their judgment, such as the person-facing escalation choices or the agent's business boundaries.
