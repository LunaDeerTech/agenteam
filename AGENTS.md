# Repository Guidelines

## Project Structure & Module Organization

agenteam is an AI Agent collaboration platform, currently comprising design documents, a backend directory skeleton, and a runnable Vue frontend with shared components and a development-only Debug page. The planned stack is Go, Vue 3, PostgreSQL/pgvector, and MinIO.

- `cmd/agenteam/` and `cmd/agenteam-runner/`: Central and Runner entry points.
- `internal/central/` and `internal/runner/`: respective implementations; `internal/runnerprotocol/`: shared communication contracts. Runner must not import Central business packages.
- `web/src/`: Vue application and shared component library; `db/migrations/`: one global migration sequence.
- `tests/`: integration and end-to-end tests; `deploy/` and `scripts/`: deployment and development tooling.
- `docs/architecture/`, `docs/frontend-design/`, and `docs/development/`: architecture, UI specifications, and repository organization. Shared controls live in `web/src/components/ui/`; Debug fixtures and presentation live in `web/src/views/debug/` and must not be imported by shared components.

Keep `.gitkeep` files until their directories contain real files.

## Build, Test, and Development Commands

The frontend is runnable independently; no Go module or backend build pipeline exists yet.

- `npm ci --prefix web`: install locked frontend dependencies.
- `npm run dev --prefix web`: serve development-only Debug at localhost:5173/debug.
- `npm run check --prefix web`: formatting check, unit tests, type check, and production build.
- `npm run preview --prefix web`: inspect production build at localhost:4173; Debug is excluded.
- `git diff --check`: check tracked changes for whitespace errors.

See `docs/development/frontend/README.md` for individual commands and component boundaries.

## Coding Style & Naming Conventions

Preserve surrounding HTML/CSS/JavaScript formatting; the frontend uses Prettier via its package scripts. Use UTF-8, LF endings, descriptive names, and kebab-case documentation filenames. Format future Go code with `gofmt` (tabs). For new Vue code, use two-space indentation and PascalCase component filenames.

Follow confirmed frontend style tokens and layout specifications. Keep business lifecycle rules in architecture documents, and update related links when moving documentation.

## Testing Guidelines

Frontend interaction tests use Vitest and Vue Test Utils; no coverage threshold is configured. Place future Go unit tests beside source as `*_test.go`, with `TestXxx` functions; put cross-module tests in `tests/`.

For showcase changes, verify light/dark themes, desktop/narrow widths, keyboard navigation, focus restoration, reduced motion, and overflow in a browser. Check Markdown links for documentation changes. Report actual validation and limitations.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `docs:` and `chore:`. Write concise, imperative subjects, for example `docs: clarify runner protocol boundaries`.

PRs should explain the problem, changed behavior, affected documents/modules, and validation performed. Link relevant issues; include screenshots for visual changes.

## Security & Agent Instructions

Keep credentials in ignored `.env*` files or root `secrets/`; commit sanitized example configurations only. When asking the user to choose through a question tool, wait for their answer without an automatic timeout.
