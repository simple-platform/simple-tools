---
description: Interactive wizard for scaffolding and designing a new Simple Platform application.
---

# Workflow: Create New Applications

> [!NOTE]
> **Enterprise Standard**: Simple Platform is for ENTERPRISE business.
> **Think Big:** Plan for feature-rich, performant, scalable solutions.
> **Feedback Loop:** Prompt for user input on critical decisions.

## Phase 1: Context & Discovery

1.  **Read Context:**
    - Load `.agent/rules/platform-invariants.md`.
    - Load `.agent/skills/data-modeling/SKILL.md`.
2.  **Inquire:**
    - "What is the name of the application?"
    - "What is the unique domain ID?" (e.g., `com.acme.portal`)
    - "What is the core business problem?"

## Phase 2: Scaffolding

1.  **Execute:** `simple new app <domain_id> "<Name>"`
2.  **Verify:** Check directory `apps/<domain_id>/`.

## Phase 3: Collaborative Data Modeling (The "No-Code" Phase)

**Constraint:** Do NOT write code yet. Agree on the plan.

1.  **List Tables:** Identify nouns (singular/plural names, ID prefix).
2.  **List Fields:** For _each_ table:
    - Name, Type (`:string`, `:enum`...), Constraints (`unique`, `secret`).
    - **CRITICAL:** Identify PII fields.
3.  **List Relationships:**
    - Identify `belongs_to` and `has_many`.
    - **Rule:** Every `belongs :to` MUST have an inverse has :many (or :one) relationship.
    - **External?** Is it linking to a table in another app?
4.  **List Indexes:** Identify search patterns.

_Use the user's existing requirements and authorization. Ask only for unresolved business decisions or destructive schema changes; continue routine implementation when already authorized._

## Phase 4: Implementation

1.  **Tables:** Write `apps/<domain_id>/tables.scl`.
2.  **External Relationships:** Write `apps/<domain_id>/records/30_links.scl`.
3.  **Validate:** Parse/check SCL with the supported CLI/context tooling. Run `simple build <domain_id>` when the app has Actions or Spaces; build is not a schema-install validation gate.

## Phase 5: Logic Planning

1.  **Record Behaviors:**
    - Identify fields needing validation (`load`/`update`/`submit`).
    - Scaffold: `simple new behavior <app> <table>`.
2.  **Custom Actions:**
    - Identify complex logic (`db-event`, `time-based`, `webhook`).
    - Scaffold: `simple new action <app> <name> "<Display name>" --lang ts --scope <org>`.
