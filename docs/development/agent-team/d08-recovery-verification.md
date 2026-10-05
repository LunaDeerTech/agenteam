# D08 中断恢复独立核查

日期：2026-10-05。角色：`verification_worker`，未参与被核查实现。范围为当前树、恢复首卡及可用验证入口；没有运行真实数据库、MinIO 或生命周期集成测试。

## 固定输入与当前代码

初检 `HEAD`、`main` 和本地 `origin/main` 跟踪引用均为 `8872110099c84cf0600bb5b62cdcd6c0c6c843e3`，`git status --porcelain=v1` 为空。67 项相关源码、契约、迁移、脚本和规则指纹见[输入清单](evidence/d08-recovery/input-manifest.json)，SHA-256 为 `f3447ea900fc2d8994efd0d75a600dcaa763130c7a51fce7250b78b345948337`；核查结束时逐项匹配。后续新文档和实现应另立固定输入。

- Project 保留已提交 B01/B02。`service.go` 的依赖和服务仍为 B02；`commands.go` 的 Lookup 仅接受 create/update，其余返回 `DEPENDENCY_UNBOUND`。`lifecycle.go`、`participants.go`、`object_authority.go`、`outbox_authority.go`、`secret_authority.go` 在本次初检均不存在。`recovery.go` 只有 creation claim/recovery，不能作为生命周期恢复实现。
- C0 `ObjectProjectStop`、`ProjectStopAuthority`、`ProjectFactAuthority` 及 prepare-read 契约已在树上；Object/Artifact 没有项目 stop/work 运行实现，Object/Secret 没有 `ProjectAuditAuthority`/`CheckProjectAuditInTx` 实现。Project Audit Authority 尚未配置这两个 producer 的真实事实 checker，生命周期 action 和 Cleanup 分支仍明确 unbound。
- `00014_object_artifact_project_stop.sql` 已由 `30f5c29` 提交，含 Object/Artifact 四张技术表；迁移已连续到 `00015`。重建停止运行实现不能改写旧迁移或重新占用 `00014`。迁移存在本身不证明 actual join、原 writer 终局或真停止已实现。
- Project HTTP/OpenAPI/app 装配及 `scripts/test-projects.sh` 不存在。真实 Project 测试入口是 `scripts/test-objects.sh -run '<固定测试集合>'`；其 PostgreSQL fixture 已纳入 `tests/project/...`，保留 `-tags=integration -race -count=1 -timeout=6m`。脚本准备也会创建固定 PG16 版本拒绝 fixture，故仅筛选 Project 不会免除该镜像依赖。

实际符号搜索、缺失路径及提交定位见[源码命令](evidence/d08-recovery/source-commands.json)、[符号输出](evidence/d08-recovery/runtime-symbol-search.log)和[现状记录](evidence/d08-recovery/recovery-observations.json)。两份 D08 文档及最新 B03 台账引用的 16 个历史 `/tmp` 路径均不存在，见[逐路径记录](evidence/d08-recovery/old-evidence-presence.json)。旧记录仅保留历史声明，无法复核的原日志不能充当重建候选的验收证据。

## 实际检查及环境快照

原始日志与命令记录位于本报告旁的 [evidence/d08-recovery](evidence/d08-recovery/commands.json)，原始临时目录为 `/tmp/agenteam-d08-recovery-verification-tgryqyye`。没有读取部署凭据或现有 fixture 描述；Docker 只对显式本机 socket 执行 version 和固定镜像 inspect，并使用自有空配置目录。没有创建、启动或停止容器、网络或服务。

| 检查 | 结果与证据 |
| --- | --- |
| 精确工具链 | `/workspace/toolchains/go1.27.1/bin/go version` 为 `go1.27.1 linux/amd64`；binary SHA-256 `30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624`。必须显式选择该路径，不能直接假定 PATH 的 `/usr/bin/go` 是该工具链。 |
| 三个纯契约包 | 在仓库根执行下述命令，exit 0，Project/Object/Audit contract 均通过；[原日志](evidence/d08-recovery/pure-contract-tests.log) SHA-256 `963ea95d0df59f2826e47d41296ba28621fabe2560f7a25387ae1ab21b049d0f`。未运行 race/vet。 |
| Project 包离线编译预检 | 同一环境执行 `go test -run '^$' -count=1 -timeout=2m ./internal/central/project`，exit 1：默认 module cache 缺 `pgx/v5 v5.11.0`、`goose/v3 v3.28.0`，`GOPROXY=off` 拒绝查找；未进入任何业务测试。[原失败](evidence/d08-recovery/project-compile-only.log) SHA-256 `aa96a66ff0205fd4fd63a1726d96838cf695ffca918a4f3b2537b9e72658eb5f`。 |
| Docker | 显式 `unix:///var/run/docker.sock` 的 client/server 均 `28.4.0`；两个固定 pgvector digest 在初次 inspect 时均不存在。[PG17](evidence/d08-recovery/image-pg17.log)、[PG16](evidence/d08-recovery/image-pg16.log) 是恢复前快照。 |
| MinIO | 要求的历史 `/tmp` binary 缺失。发现的 onboarding binary SHA 为 `fe58d3d9dcc652ae655b77e11a96549d9d76707246054dcc985580a3b48313d6`，不等于 fixture 要求的 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，没有执行或复用它。 |

纯检查的完整环境和参数分别保存在[契约命令](evidence/d08-recovery/pure-contract-tests-command.json)与[编译预检命令](evidence/d08-recovery/project-compile-only-command.json)：

```sh
GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off \
GOFLAGS='-mod=readonly -p=2' \
GOCACHE=/tmp/agenteam-d08-recovery-verification-tgryqyye/gocache \
GOTMPDIR=/tmp/agenteam-d08-recovery-verification-tgryqyye/tmp \
/workspace/toolchains/go1.27.1/bin/go test -count=1 -timeout=2m \
  ./internal/central/project/contract \
  ./internal/central/object/contract \
  ./internal/central/audit/contract
```

缺失缓存可以恢复：固定 MinIO source/module sums、ZIP 和精确构建参数已记录在 [D05 研究第 2 节](../work-items/d05-object-storage-research.md#2-校验和与构建复现)。主线程已另派依赖恢复负责人，独占固定镜像、MinIO 与根 `go.mod/go.sum` 的自有 module cache 恢复；该工作的后续结果单独举证，本报告不把恢复前快照视为永久阻塞。真实 fixture 仍需明确资源所有权及对应实现冻结后再运行。

## B03-R1 首卡独立静审

审查输入为[恢复首卡](../work-items/recovery-d08-b03.md) rev1，SHA-256 `8f0297714cf6104784db55c8bc8c2adfe87449b6febe3b058bb761d7a64892e4`，作者已停写。对照 `RequiredManifest`、`ProjectLifecycleParticipant` 和既有 `nilPort`/`fault`，结论为**卡片静审通过，无硬阻断**。

卡片明确复用唯一 manifest 验证与摘要、按 `(Name, ContractVersion)` 和全部元数据精确匹配、当前 required 完整解析、历史缺版本拒绝且无自动回退。typed nil 在 `Name()` 前拒绝；零 Registry/Plan 返回 unbound；输入和输出切片复制；原 manifest/digest 保持；清理按原图确定性排序。所有错误保持 `not_started`，注册/解析不调用业务方法、不产生事务或 I/O。Scope 仅允许两个新文件，没有绕过真实适配和停止证据。

因此首个可执行完整结果是 `project/participants.go` 与测试：不可变 adapter 注册及原版本恢复解析。实现后仍须在固定输入和已恢复的精确 Go 依赖上通过作者测试、race/vet及独立验证；当前仅通过规格静审。后续 B03-P 持久编排、D05 真停止/清理、Secret/Object fact checker、真实 PG/MinIO/ProcessGuard/Outbox 组合和 B04/D10 均保持未验证。

本次无产品源码或 Git 写操作。实际写入仅本报告和 19 个轻量证据文件；临时 Go cache 不纳入提交。本次命令均已结束，未持有 Docker fixture 资源。
