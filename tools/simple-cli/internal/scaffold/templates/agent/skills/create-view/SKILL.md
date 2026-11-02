---
name: create-view
description: Configure platform Custom Views, collection presets, record Spaces, and trigger-backed View Actions.
---

# Create a view

Use platform configuration before adding a Space. Read
[Custom Views](https://simple.dev/docs/features/custom-views/#configure-table-view-presets) for supported preset
schemas and [View Actions](https://simple.dev/docs/features/view-actions/#where-actions-appear) for
placement and invocation context. Inspect existing metadata and installed package
exports; a source-only component is not automatically available in customer apps.

## Configuration records

For a target table, there is one Custom View per `(target_table, type)`; update
its existing record rather than creating competing list/record configurations.
Resolve table IDs by application and exact name. `priority` defaults to 100;
`is_active` defaults to true. Follow the app's metadata query pattern:

```scl
# meta must contain the intended application's table metadata.
set dev_simple_system.custom_view, order_list {
  display_name "Orders"
  type list
  target_table_id `$var('meta') |> $jq('.tables[] | select(.name == "order") | .id')`
}
```

Store collection presets under `customization.views`, with unique preset IDs and
a valid `defaultPresetId`. The envelope uses `version: 1`. Preset filters require
native `RecordWhere`; shorthand and predicate-tree filters are rejected without
conversion. Tile layouts also use `version: 1`.
Use real logical fields and public preset validators where available. Tile
presentations are `standard`, `media`, or `message`.
For the complete SCL example, including the `var` block and `$json()` assignment
to `customization`, see `.simple/context/09-custom-views.md`.
A minimal list tile config for a declared `number` field is:

```json
{ "tile": { "version": 1, "presentation": "standard", "titleFieldName": "number" } }
```

Presets configure fields/filters/order, not record arrays or per-tile CSS.
Board lanes require an enum field. Platform routes support table/list/board/inbox;
Spaces can use the managed collection exports supported by their installed UI Kit
and host. Standalone condition editors and Inbox are platform-only.

For a record Space, bind `type record` and `space_id` to the app's Space record,
then follow [Record Space Builder](../record-space-builder/SKILL.md). Host tabs,
header actions, managed forms, and record sessions have distinct ownership.

## View Actions

Scaffold the Action with the CLI. For a UI button, declare a bare trigger and
`logic_trigger` in SCL; use the [button wiring recipe](https://simple.dev/docs/features/view-actions/#configure-an-action-button).
Do not add an unrelated DB event, schedule, webhook, or invented `type manual`.
Then link the button to the existing Custom View and trigger. The following
example uses previously declared logical keys:

```scl
set dev_simple_system.view_action, approve_order_button {
  name "approve-order"
  label "Approve order"
  icon "check-circle"
  type primary
  position 10
  custom_view_id `$app_record('order_list')`
  trigger_id `$app_record('approve_order_trigger')`
}
```

Types are `primary`, `secondary`, `destructive`, `outline`; icon names use the
platform's Lucide identifiers. Position must be unique within the Custom View.
A collection action receives table/view context, **not selected record IDs**.
Dashboard View Actions are not currently rendered. A Record Space supplies
its main buttons through the SDK header actions instead of persisted View Actions.
Treat visibility as UI configuration, never proof of authorization. Verify the
Action's actual input contract and server refusal behavior before exposing it.
