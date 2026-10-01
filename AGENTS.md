# Repository Guidelines

## Project Structure & Module Organization

agenteam is an AI Agent collaboration platform, currently comprising design documents, a directory skeleton, and a standalone component showcase. The planned stack is Go, Vue 3, PostgreSQL/pgvector, and MinIO.

- `cmd/agenteam/` and `cmd/agenteam-runner/`: Central and Runner entry points.
- `internal/central/` and `internal/runner/`: respective implementations; `internal/runnerprotocol/`: shared communication contracts. Runner must not import Central business packages.
- `web/src/`: future Vue application; `db/migrations/`: one global migration sequence.
- `tests/`: integration and end-to-end tests; `deploy/` and `scripts/`: deployment and development tooling.
- `docs/architecture/`, `docs/frontend-design/`, and `docs/development/`: architecture, UI specifications, and repository organization. Showcase HTML/CSS/JavaScript assets live in `docs/frontend-design/component-showcase/`.

Keep `.gitkeep` files until their directories contain real files.

## Build, Test, and Development Commands

No Go module, frontend dependency manifest, build pipeline, or automated test runner exists yet. Do not describe planned commands as available.

- `python3 -m http.server 8765 --bind 127.0.0.1`: serve the repository; open `/docs/frontend-design/component-showcase/index.html` on localhost port 8765.
- Open the showcase `index.html` directly for dependency-free preview.
- `git diff --check`: check tracked changes for whitespace errors before submission.

Document build and test commands when introducing runnable modules.

## Coding Style & Naming Conventions

Preserve surrounding HTML/CSS/JavaScript formatting; no formatter, linter, or repository-wide indentation configuration is installed. Use UTF-8, LF endings, descriptive names, and kebab-case documentation filenames. Format future Go code with `gofmt` (tabs). For new Vue code, use two-space indentation and PascalCase component filenames.

Follow confirmed frontend style tokens and layout specifications. Keep business lifecycle rules in architecture documents, and update related links when moving documentation.

## Testing Guidelines

No coverage threshold or automated testing framework is configured. Place future Go unit tests beside source as `*_test.go`, with `TestXxx` functions; put cross-module tests in `tests/`.

For showcase changes, verify light/dark themes, desktop/narrow widths, keyboard navigation, focus restoration, reduced motion, and overflow in a browser. Check Markdown links for documentation changes. Report actual validation and limitations.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `docs:` and `chore:`. Write concise, imperative subjects, for example `docs: clarify runner protocol boundaries`.

PRs should explain the problem, changed behavior, affected documents/modules, and validation performed. Link relevant issues; include screenshots for visual changes.

## Security & Agent Instructions

Keep credentials in ignored `.env*` files or root `secrets/`; commit sanitized example configurations only. When asking the user to choose through a question tool, wait for their answer without an automatic timeout.
