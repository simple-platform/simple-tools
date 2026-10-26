---
name: create-view
description: Guide to building Custom Views, Dashboards, and Action Buttons.
---
# Create View Skill

## 1. Concepts
*   **Custom View:** A specialized UI configuration for a specific task (e.g., "Manager Dashboard", "Triage Queue").
*   **View Action:** A button placed on a view that triggers a backend Logic/Workflow.

## 2. Defining a Custom View
Records go in `records/100_metadata.scl` (typically).

```scl
set dev_simple_system.custom_view, my_dashboard {
  display_name "Manager Dashboard"
  
  # TYPE:
  # - "record": Single record detail view.
  # - "list": Collection presets (Table, List, Board, Inbox).
  # - "dashboard": Widget canvas.
  type dashboard 

  # LINK:
  # Use variable lookup to find target table ID
  target_table_id `$var('meta') |> $jq('.tables[] | select(.name == "order") | .id')`
}
```

### Collection preset JSON

Use an active `type list` Custom View for the target table. Put the preset set in its **Customization** field under `views`:

```json
{
  "views": {
    "version": 1,
    "defaultPresetId": "orders",
    "presets": [
      {
        "id": "orders", "title": "Orders", "kind": "list",
        "config": { "tile": { "version": 1, "presentation": "standard", "titleFieldName": "number" } }
      },
      {
        "id": "orders-table", "title": "Orders table", "kind": "table",
        "config": { "columns": [{ "fieldName": "number" }, { "fieldName": "amount" }] }
      }
    ]
  }
}
```

Use fields that exist on the target table. In SCL, put this JSON in a `var` value block and set `customization` with `$var('order_views_customization') |> $json()`, following existing metadata recipes. Give presets unique IDs and select a valid `defaultPresetId`.

Tile presentations are `standard`, `media`, and `message`. The platform controls tile typography, spacing, and sizing; configure field bindings and presentation rather than per-tile styles.

Platform routes support `table`, `list`, `board`, and `inbox`. Spaces support List/DataGrid/Board with built-in filters; select one supported preset for the UI Kit component. Board lanes require an enum field. Sources are database identities `{ kind: 'table', appId, tableId }`; preset/tile JSON never supplies rows. Standalone ConditionBuilder remains private.

See the [Custom Views guide](https://simple.com/docs/features/custom-views) for filters, ordering, tile fields, Board summaries, Inbox, and record-Space configuration. Keep View Actions outside the `views` preset object.

## 3. Defining a View Action (Button)
Buttons connect the UI to your **Actions** (via Triggers).

```scl
set dev_simple_system.view_action, btn_approve {
  name "approve-order"
  label "Approve Order"
  icon "check-circle"  # Feather Icon
  
  # STYLE:
  # - "primary": Solid color (Call to Action)
  # - "outline": Border only (Secondary)
  # - "danger": Red (Destructive)
  type primary
  
  # PLACEMENT:
  # Link to the Custom View ID
  custom_view_id `$var('ids') |> $jq('.views[0].id')`
}
```

## 4. View Properties
| Property | Description |
| :--- | :--- |
| `icon` | Feather icon name (e.g., `file-text`, `download`). |
| `name` | Internal identifier (kebab-case). |
| `label` | Button text shown to user. |
| `type` | `primary`, `outline`, `danger`. |

## 5. Complete Recipe: "Generate PDF" Button
1.  **Logic:** Create `actions/generate-pdf`.
2.  **Trigger:** Create `trigger, generate_pdf_trigger` (Manual type).
3.  **Bind:** Link Logic+Trigger in `logic_trigger`.
4.  **View:** Create `custom_view, order_view`.
5.  **Button:** Create `view_action` linking View+Trigger.

```scl
set dev_simple_system.view_action, btn_gen_pdf {
  name "gen-pdf"
  label "Generate PDF"
  icon "file-text"
  type outline
  
  custom_view_id `$var('ids') |> $jq('.view_id')`
  trigger_id     `$var('ids') |> $jq('.trigger_id')`
}
```

## 6. Best Practices
*   **Context:** Actions receive the ID(s) of the expected record(s).
*   **Feedback:** The UI handles loading states automatically while the WASM Action runs.
*   **Permissions:** Action visibility respects the underlying Logic permissions.
