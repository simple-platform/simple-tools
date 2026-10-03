# Tasks

A **task** is one piece of work that an agent does for a person: read a document, answer a question, write a summary. A **task type** is the kind of work it is. Your app declares task types; Spaces and other tasks create tasks from them.

> [!NOTE]
> A task is not an Action. An Action is code you write and the platform runs. A task is work an **agent** (a model) does, with the tools its task type allows, and the platform checks the answer before anyone sees it.

---

## Task types and tasks

A task type is a configuration record. It names the input the work takes, the output it must answer with, and the states the work moves through:

```scl
set dev_simple_system.task_type, packet_summary_type {
  name "packet_summary"
  plural_name "Packet summaries"
  description "Summarises a packet from what its other tasks found."
  playbook_id `$app_record("packet_summary_playbook")`
  schema `$file('records/task-types/packet_summary.json') |> $json()`
  states `$file('records/task-types/states.json') |> $json()`
}
```

`schema` holds two JSON Schemas (Draft 2020-12), `input` and `output`:

```json
{
  "input": {
    "type": "object",
    "additionalProperties": false,
    "required": ["packet_id"],
    "properties": { "packet_id": { "type": "string" } }
  },
  "output": {
    "type": "object",
    "additionalProperties": false,
    "required": ["summary"],
    "properties": { "summary": { "type": "string" } }
  }
}
```

A task is one run of that work. It has an immutable `input` that matches the schema (at most 32 KiB), a `status`, a `state`, and an `output` once the agent has answered.

An `agent_task_type` record with `role executor` says which agent does the work, and `task_type_tool` records say which tools it may call. A task type that declares no tools gets none.

---

## Statuses and states

Every task has one of six **statuses**:

| Status        | Meaning                                                                                               |
| ------------- | ----------------------------------------------------------------------------------------------------- |
| `queued`      | Created and not started yet.                                                                          |
| `in_progress` | The agent is working.                                                                                 |
| `waiting`     | Stopped. It asked a question, ran out of rounds, or a model could not be reached. A reply resumes it. |
| `completed`   | Finished. A reply reopens it.                                                                         |
| `cancelled`   | Closed for good. Nothing reopens it.                                                                  |
| `failed`      | Could not be finished. Nothing reopens it.                                                            |

`completed`, `cancelled` and `failed` are the **end** statuses.

A **state** is a name your task type gives to a status. The `states` map says which status each state means:

```json
{
  "queued": "queued",
  "working": "in_progress",
  "waiting_for_user": "waiting",
  "waiting_for_event": "waiting",
  "completed": "completed",
  "approved": "completed",
  "declined": "cancelled",
  "cancelled": "cancelled",
  "failed": "failed"
}
```

The reserved states (`queued`, `working`, `waiting_for_user`, `waiting_for_event`, `completed`, `cancelled`) must stay as shown. Add your own, as `approved` and `declined` are added here. Add a state that maps to `failed` if tasks of this type should be able to fail.

---

## Creating a task from a Space

A Space creates a task with `simple.tasks.create` and answers or resumes one with `simple.tasks.reply` (see the [SDK Reference](./11-sdk-reference.md)). It needs the id of the task type, which it can look up by name:

```typescript
const { task_types } = await simple.data.query<{ task_types: Array<{ id: string }> }>(
  `query { task_types: dev_simple_system__task_types(where: { name: { _eq: "packet_summary" } }) { id } }`,
)

const { task } = await simple.tasks.create({
  input: { packet_id: 'DOC000001' },
  taskTypeId: task_types[0].id,
  title: 'Summarise the packet',
})
// task = { id: 'TASK000120', status: 'queued', revision: 0 }
```

The platform checks `input` against the task type's `schema.input` and refuses the create if it does not match.

To carry on a task that is `waiting`, reply to it:

```typescript
await simple.tasks.reply({ content: 'Please carry on.', taskId: task.id })
```

---

## Starting a task after other tasks

Sometimes a task needs the results of others: a summary of several reads, a check over several answers. Create the others first, then list them in the new task's input, under the reserved key `_metadata`:

```typescript
const { task: summary } = await simple.tasks.create({
  input: {
    _metadata: {
      start_after: [
        { task_id: 'TASK000101' }, // Waits until this task has completed.
        { on: ['approved', 'declined'], task_id: 'TASK000102' }, // Waits until it is approved or declined.
      ],
    },
    packet_id: 'DOC000001',
  },
  taskTypeId: summaryTypeId,
  title: 'Summarise the packet',
})
```

**`_metadata` belongs to the platform.**

- It is checked by the platform, not by your schema. Do not declare it in `schema.input`: a task type that does is refused. The rest of `input` is checked against your schema as if `_metadata` were not there.
- Its shape is fixed: only `start_after`, and in each entry only `task_id` and `on`. Anything else is refused.
- It is stored with the input and counts toward the 32 KiB limit.

**`start_after`** is a list of entries. Each `task_id` names an existing task in your tenant, listed once. The order of the list means nothing.

**`on`** says which end states of that task let the new task start. Its names are states of the **listed task's own task type**, and each must map to an end status in that type's `states` map. Leave it out and the new task starts only when the listed task is in a state that maps to `completed`.

### What the platform does

| The listed tasks                                                   | The new task                                           |
| ------------------------------------------------------------------ | ------------------------------------------------------ |
| All ended, each in a state its entry allows                        | Starts, as any task starts.                            |
| Not all ended yet                                                  | Waits. No agent runs and no model is called.           |
| One ended in a state its entry does not allow, or no longer exists | Is cancelled. Its cancellation reason names that task. |

- A scheduled task stays `queued`. Tell it apart from one waiting for a worker by the `start_after` list in its `input`.
- There is no time limit on the wait.
- A listed task that only stopped (status `waiting`) is not ended. The new task keeps waiting until you reply to that task, or cancel it.
- The platform looks for tasks that can start about once a minute, so allow up to about a minute between the last listed task ending and the new one starting.
- A message written to the task while it waits is kept, and the agent reads it as an earlier message when the task starts.
- A cancelled task stays cancelled. To try again, create another task.

### When a create is refused

A create the platform refuses rejects with `SpaceProtocolError` code `task_rejected`. `details.code` is `TASK_INPUT_INVALID` and `details.pointers` says where:

| Pointer                                  | Cause                                                            |
| ---------------------------------------- | ---------------------------------------------------------------- |
| `/input/_metadata/start_after/1/task_id` | Not a task id, listed twice, or no such task.                    |
| `/input/_metadata/start_after/1/on/0`    | Not a state of that task's type that maps to an end status.      |
| `/input/_metadata/start_after/1`         | That task has already ended in a state the entry does not allow. |
| `/input/_metadata/<name>`                | A member the platform does not know.                             |

---

## Reading the listed tasks: `read-task-output`

The new task's agent reads a listed task with the tool `read-task-output`. You do not bind it: the platform adds it to the tools of every task that has a `start_after` list, and to no other task.

- It takes `{ "task_id": "TASK000101" }`, and only the ids on the task's own list are accepted.
- It returns `{ "task": { "id", "status", "state", "output" } }`: the status, the state in the listed task's own vocabulary, and its typed output exactly as stored. `output` is `null` when the task wrote none.
- The agent cites a value by pointing into the result, for example `/task/output/summary`.
- A large output is shown cut. Bind `read-tool-result` to your task type so the agent can read the rest.

Tell the agent to use it in the task type's playbook:

```markdown
Call `read-task-output` once for each `task_id` under `_metadata.start_after`.
Tell the tasks apart by what their outputs hold. Do not write a value you did
not read.
```

---

## Checklist

- [ ] The listed tasks are created before the task that lists them.
- [ ] `schema.input` does not mention `_metadata`.
- [ ] Each name in `on` is a state of the **listed** task's type that maps to an end status.
- [ ] The task type binds `read-tool-result` if a listed output can be large.
- [ ] The Space shows a `queued` task with a `start_after` list as scheduled, not as stuck.
