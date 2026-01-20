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
  # - "list": Table list view.
  # - "dashboard": Widget canvas.
  type dashboard 

  # LINK:
  # Use variable lookup to find target table ID
  target_table_id `$var('meta') |> $jq('.tables[] | select(.name == "order") | .id')`
}
```

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
  
  # BEHAVIOR:
  # Link to the Trigger ID (which runs the Action)
  trigger_id `$var('ids') |> $jq('.triggers[0].id')`
}
```

## 4. Best Practices
*   **Context:** Actions receive the ID(s) of the expected record(s).
*   **Feedback:** The UI handles loading states automatically while the WASM Action runs.
*   **Permissions:** Action visibility respects the underlying Logic permissions.
