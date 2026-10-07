# Instructions for the coding agent

Project: `brinketask`. Self-hosted tasks and reminders application.

## Sources of truth

1. `SPEC.md`: behaviour, data model and design decisions (D-xx, R-x).
2. `openapi.yaml`: API contract.
3. `DESIGN.md`: layout, metrics and colour tokens of the web client. On behaviour, `SPEC.md` wins.

Read them before any change. If a task requires departing from them, **stop and ask**; do not improvise. If the change is approved, update the document in the same commit as the code.

## Rules

- **OpenAPI first.** To change the API, edit `openapi.yaml` and regenerate the code. Generated files are never edited by hand and no routes are added outside the contract.
- **Scope.** Implement only what the current phase asks for (`SPEC.md`, section 12). Nothing from the "Out of V1" list. Do not add features, configuration options or abstractions nobody asked for.
- **Tests are mandatory.** Every behaviour change arrives with its tests in the same commit. Section 11 of `SPEC.md` lists the minimum cases. A test is never disabled, skipped or loosened to make it pass; if a test looks wrong, explain why and ask.
- **Injected clock.** Domain code (recurrence, reminders, sessions) never calls `time.Now()`; it receives a clock. Tests use a fixed clock.
- **Authorization.** Every data access is filtered by membership in `list_members`. Someone else's resource = 404.
- **Sync.** Every write to a syncable resource increments `version` and assigns a new `seq` within the same transaction (D-07). Deletion is always soft.
- **Idempotency.** Creations and actions must be repeatable with no additional effect (D-04, D-10).
- **Migrations.** Forward-only. A merged migration is never modified; add another one.
- **Dependencies.** Before adding one, justify why it is needed and check that it is maintained. Versions pinned.
- **Secrets.** None in the repository, in logs or in tests. Logs contain no task titles or descriptions.
- **i18n.** Every user-facing string, including notification texts, goes through the i18n layer. No hard-coded literals in components. V1 ships English only; do not add a language selector or other locales.

## Way of working

- Small changes: one commit per logical unit, with a message that explains the why and cites the decision (`D-10`, `R-1`) where it applies.
- Before calling anything done: lint, tests and build locally, all green.
- Everything in English: code, identifiers, comments, commit messages, documentation and UI strings.
- The code reviewer has extensive C/C++ experience and little with Go and React. Prefer explicit, direct code over clever constructs, and comment the why of any non-obvious language or framework idiom.
- If you find an ambiguity or contradiction in the specification, do not resolve it on your own: flag it and propose options.

## Planned layout

```
/api            openapi.yaml and generation config
/cmd/brinketask entry point
/internal       domain, storage, http, scheduler, notifications
/migrations     SQL
/web            React client
/deploy         Dockerfile and compose
SPEC.md
DESIGN.md
CLAUDE.md
```

## Commands

To be defined in phase 0. Document here the commands for lint, tests, code generation and local startup as soon as they exist.
