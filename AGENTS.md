<!-- sdlc-anchor begin -->
## SDLC

This project tracks features, requirements, specifications, and decisions under [`.sdlc/`](.sdlc/).

Before starting work on a feature:
- Read `.sdlc/context/` (`project-overview.md`, `architecture.md`, `conventions.md`, `vocabulary.md`, `schema.dbml`, `infrastructure.md`) for project context, and apply the style rules in `conventions.md` to anything you write.
- Run `/sdlc status` to see feature progress, or `/sdlc continue` to resume in-progress work.
- Run `/sync-sdlc` to reconcile `.sdlc/` with the current codebase.

Never commit local-only state: `.sdlc/state.yml`, `.sdlc/features/*/progress.md`, and `status-report.html`.
<!-- sdlc-anchor end -->
