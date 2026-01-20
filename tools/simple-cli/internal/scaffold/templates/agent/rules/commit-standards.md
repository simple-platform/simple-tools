---
trigger: always_on
---
# Commit Standards

> [!TIP]
> Your commit messages quantify the value you deliver. Make them count.

## 1. Conventional Commits
*   **Requirement:** All commit messages **MUST** follow the `type(scope): description` format.
*   **Reasoning:** This enables automated release notes and semantic versioning.

## 2. Atomic Commits
*   **Guideline:** One logical change per commit.
*   **Antipattern:** `feat: update everything` (Do not mix refactors with features).

## 3. Contextual Analysis
*   **Before Committing:** Analyze the diff.
*   **Before Committing:** Analyze the diff.
    *   Did I add a new file? -> `feat`
    *   Did I fix a typo? -> `docs` or `fix`
    *   Did I change a variable name? -> `refactor`
    *   Did I optimize a query? -> `perf`
    *   Did I add a unit test? -> `test`
    *   Did I update whitespace/indentation? -> `style`
    *   Did I update .gitignore or build scripts? -> `chore`
