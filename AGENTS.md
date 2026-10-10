# Repository Guidelines

本文件只保存长期有效的仓库规则，不追加逐轮进度、哈希、日志或验收流水。AI 工作流以[开发团队流程](docs/development/agent-team/README.md)为统一来源；旧任务卡和历史报告中的重复哈希、永久归档、逐层回主线程审批及禁止再委派要求不再作为日常流程。产品契约、真实验收门槛与未完成事实仍以对应工作项为准。

## 当前工作与按需读取

- 当前工作、阻塞和下一步见[任务台账](docs/development/agent-team/tasks.md)，模块依赖与完成门槛见[开发计划](docs/development/development-plan.md)。先读取当前摘要和目标工作项；使用标题或路径定位相关段落，不默认加载全部历史。
- 各工作项的阶段成果不等于整卡或生产接入完成；恢复时核对实际代码、Git 差异及相应任务记录，不按旧聊天或旧进度段落执行。
- Object runtime join、OpenAI tools 独立验收、Central SPA concurrent-publication 及 Jina/Image 来源获取的既有阻塞在任务记录中保留；工作流调整不自动恢复这些任务，也不改变生产未绑定和未验证范围。
- 原入口中的历史推进记录可通过 Git 文件历史和任务台账查询，不另复制成归档文件。只更新发生变化的正式来源，不在 AGENTS、计划和台账重复抄写同一进度。

## Project Structure & Module Organization

agenteam is an AI Agent collaboration platform with a Go backend, PostgreSQL/pgvector, Central and Runner processes, and a Vue frontend. See the relevant architecture and development guide for current contracts and supported behavior.

- `cmd/agenteam/` and `cmd/agenteam-runner/`: Central and Runner entry points.
- `internal/central/` and `internal/runner/`: respective implementations; `internal/runnerprotocol/`: shared communication contracts. Runner must not import Central business packages.
- `internal/platform/logging/` and `internal/platform/lifecycle/`: neutral process utilities; must not import Central or Runner business types. `api/openapi/common.json`: implemented shared HTTP schemas.
- `web/src/`: Vue application and shared component library; `db/migrations/`: one global migration sequence.
- `tests/`: integration and end-to-end tests; `deploy/` and `scripts/`: deployment and development tooling.
- `docs/architecture/`, `docs/frontend-design/`, and `docs/development/`: architecture, UI specifications, and repository organization. Shared controls live in `web/src/components/ui/`; Debug fixtures and presentation live in `web/src/views/debug/` and must not be imported by shared components.

Keep `.gitkeep` files until their directories contain real files.

## Build, Test, and Development Commands

The frontend remains independently runnable. The root Go module requires exactly Go 1.27.1; backend scripts set `GOTOOLCHAIN=local` and reject a different toolchain. Set `AGENTEAM_GO` to the binary path if PATH does not select it.

- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh`: Go tests, vet, race checks and both binary builds.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh`: real PostgreSQL/pgvector, MinIO and Central process integration tests using task-owned Docker fixtures; ordinary Go tests do not require Docker.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh`: database/process suite plus real controlled outbound private sockets, generated CA and nonce-owned internal Docker network fixtures.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/build-go.sh`: build `bin/agenteam` and `bin/agenteam-runner`.
- `AGENTEAM_GO=/path/to/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/task-owned/cache/minio sh scripts/test-objects.sh`: full database, security, object, Artifact, download, transfer, Outbox and real process suite. The other integration scripts also acquire mandatory MinIO; the exact source-built binary and SHA are documented in the backend guide.
- `./bin/agenteam --check-config`: validate current Central settings, including required database URL, independent cursor/Secret/download/account keyrings, `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG`, safe `PUBLIC_ORIGIN`, object endpoint/bucket/explicit credentials, spool path and any explicit CA, without connecting, creating directories or opening the recovery log. Optional outbound CA appends deployment roots. Central still emits `scope=d05`; its help/version retain the D05 label. `./bin/agenteam-runner --check-config` validates D02 Runner settings. Both report `ready=false`; help/version need no configuration.
- `npm ci --prefix web`: install locked frontend dependencies.
- `npm run dev --prefix web`: serve development-only Debug at localhost:5173/debug.
- `npm run check --prefix web`: formatting check, unit tests, type check, and production build.
- `npm run preview --prefix web`: inspect production build at localhost:4173; Debug is excluded.
- `git diff --check`: check tracked changes for whitespace errors.

See `docs/development/backend/README.md` and `docs/development/frontend/README.md` for commands, configuration and boundaries. Backend process tests use temporary directories and loopback ephemeral ports; database and outbound fixtures verify nonce, labels and exact resource IDs before connecting. Outbox process tests share the real object ProcessGuard; it must remain held until callbacks and database cleanup actually join, or the owning process exits. Outbound success tests use owned private container sockets with exact policy rules; never replace the production loopback classifier with allow. Tests must not connect to existing infrastructure or read external credentials.

## Coding Style & Naming Conventions

Preserve surrounding HTML/CSS/JavaScript formatting; the frontend uses Prettier via its package scripts. Use UTF-8, LF endings, descriptive names, and kebab-case documentation filenames. Format Go code with `gofmt` (tabs). For new Vue code, use two-space indentation and PascalCase component filenames.

Follow confirmed frontend style tokens and layout specifications. Keep business lifecycle rules in architecture documents, and update related links when moving documentation.

## Testing Guidelines

Frontend interaction tests use Vitest and Vue Test Utils; no coverage threshold is configured. Go unit tests live beside source as `*_test.go`, with `TestXxx` functions; `tests/process/` builds and tests the real command binaries. Cross-module tests belong in `tests/`.

For showcase changes, verify light/dark themes, desktop/narrow widths, keyboard navigation, focus restoration, reduced motion, and overflow in a browser. Check Markdown links for documentation changes. Report actual validation and limitations.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `docs:` and `chore:`. Write concise, imperative subjects, for example `docs: clarify runner protocol boundaries`.

PRs should explain the problem, changed behavior, affected documents/modules, and validation performed. Link relevant issues; include screenshots for visual changes.

## Security & Agent Instructions

Keep credentials in ignored `.env*` files or root `secrets/`; commit sanitized example configurations only. Required product decisions stay pending until the user answers; silence is not approval.

## 功能优先的模块开发

开发方式以[团队流程](docs/development/agent-team/README.md)为正式来源，[开发计划](docs/development/development-plan.md)保存模块业务依赖与完成标准。先明确模块关系、数据和接口定义、调用方式、依赖与集成责任；默认由主线程为可独立推进的模块创建不同 worktree 和 `ai/` 功能分支并行开发。只读、小改动或紧耦合任务可按实际收益共用工作树，不以习惯性共享工作树形成全局串行。

小模块先完成可用功能和必要的正常、失败基础测试，尽早进入隔离环境中的真实调用小范围联调。可联调不等于正式完成；权限、数据完整性、迁移和 Secret 等关键门禁须在暴露相应真实风险前通过，业务契约、已知失败及正式完成门槛继续有效。测试明确问题、范围和停止条件，相同输入的通过证据复用，不用无依据的边界扩展推迟联调。旧卡片中的串行、全量测试后才联调等通用流程要求按现行团队流程解释，既有 STOP 不自动恢复。

## 主线程统筹与执行边界

主线程只负责规划、调度、滚动核对进展、纠偏、跨任务契约与资源协调，以及全部 Git 操作和最终集成；不接管编码、诊断、测试、文档正文或冲突内容修改。简单任务也交给执行者；执行者受阻时复用、补派或替换执行者，不由主线程补做。下级 `delivery_lead` 可以实施，并组织局部拆分、返修、集成和验证。

在契约准备、首条真实链路、基础自测后、首次联调、受阻或变更及交付时滚动核对，尽早确认组合基线；主线程评估方向、风险和负责人结论，不亲自重做执行检查。同一文件只有一个写者；跨 worktree 的共享文件、接口、锁文件和全局迁移由主线程协调负责人及兼容落地顺序。默认隔离环境允许并行测试，仅实际共享资源冲突的范围串行。

角色与技能、并行调度、按风险验证和证据复用均见团队流程。所有层级统一使用 `gpt-6-astra / ultra` 与 Fast 配置 `service_tier = "priority"`，禁止静默降级；实际运行时限制如实报告。项目请求 100 个子 agent 并发，实际按运行时与资源承载调度，不设角色单例或固定子树配额。无空位时复用适合的实例，续派已完成或停止实例用 `followup_task` 并确认启动，`send_message` 只用于沟通。

## 跨对话交接与 Git 交付

用户已持续授权主线程主动保存和推送开发检查点，无需逐次提醒或批准；WIP 不等于验收通过。由执行者维护必要源码、harness、复现材料及简短状态，报告可恢复范围并停写；主线程负责各工作树的创建、更新、提交、推送和集成。各级子 agent 不执行 Git 写操作，不创建或切换分支、worktree；内容冲突由指定执行者解决，主线程执行后续 Git 操作。

保存触发点、命令、多 worktree 记录和跨设备接续以[状态与恢复说明](.agent-state/README.md)为准，[当前检查点](.agent-state/current.md)只保存当前任务分支的恢复信息。恢复先核对本地改动、远端活动分支和唯一写会话；另一会话仍在运行时不得抢占其分支或覆盖 `current.md`。push 未成功须明确尚未远端保存。常规 commit/push 授权不包含部署、强推、丢弃改动或覆盖其他会话工作。

正式 `main` 交付推送确认后及交接、恢复时，主线程按[已完成分支与 worktree 清理](docs/development/agent-team/README.md#已完成分支与-worktree-清理)主动收尾；常规清理无需逐次确认，具体操作与跨环境恢复见[状态说明](.agent-state/README.md#清理操作与跨环境接续)。

首次接任务只读本文件、[团队流程](docs/development/agent-team/README.md)、目标规格及相关技能；后续补读变化。派工字段见[精简简报](docs/development/agent-team/task-template.md)。Vue 测试读取 `vue-testing-best-practices`；真实浏览器与测试 harness 使用仓库内 `agenteam-test-engineering`，环境已有 `playwright` 技能可补充使用。工作流文档变更只检查相关配置解析、链接、一致性和格式，不跑无关产品测试，也不新增验收归档。
