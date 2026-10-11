# 当前执行检查点

## 本次 Variables 接续

2026-10-11 在 `/workspace/agenteam-project-variables-resume`、分支 `ai/project-variables-resume-20261011` 接续普通 Project Variables 创建、编辑与回读。root 以 `1b6ad53e` 建树，已组合 UI 来源 `c9101845`、`11f46b63` 与 fixture 来源 `6e73fddd`、`4308e91d`；本轮自查起点为 `5e04ef71`，控制样本修复保存于 `bcf1d880`；修复保存于 `bb468f12` 并经独立复核；root 窄同步 Skills Host/App handler 与 logo fixture 修复后，真实链冻结于 `481f526d`。

`frontend_checks` 独占本树 Variables 22 条产品/测试/fixture 路径及必要恢复记录，直接向 root 报告并抄送 `recovery_lead`。root 独占全部 Git 操作；本轮属于作者实现自查，`variables_review` 已独立接受两项 must-fix 修复及本轮 native01 正常链、原退出尾和两图；本轮实现者不兼任独立验收者。`recovery_lead` 继续负责另一树 Skills/Milestone 真实链，本树不继承其资源窗口或通过结论。

`tests/account-captcha-web/e2e/task-review.helpers.ts` 的 Skills 观测段冻结，已由 root 经 `32f32a97`、`17b27896` 窄同步主链 Host/实际 App handler 与 logo 关联修复；本实现者不独立修改该段。

## 限定结果与文件

已有 initialized Project 的当前 Human Owner 可从 `/:username/:project_name/settings/variables` 读取普通变量摘要目录与完整详情，明确确认创建或编辑，按字段 presence 保存原文并允许显式空 description/value。确认命令后必须取得当前 Get/List；结果不确定时只查询原 key 与原意图，不自动重发。摘要不含 value，身份或 Project 切换清理旧材料。

不含删除、Secret、UI 原命令重放、Agent/Runner 环境注入、生产 Project 初始化或完整 Variables 生命周期。契约来源为[普通 Variables Owner 后端卡](../docs/development/work-items/d10-project-variables-owner-http.md)、[正式 API](../api/openapi/project-variables.json)与[变量架构](../docs/architecture/project-work-management/project-environment-variables.md)；后端接受不代表本片 UI 验收。

- 前端 14 路径：`web/src/App.vue`、`api/{client,project-variables}.ts`、`composables/{useSession,useProjectVariables}.ts`、`router/{auth,index}.ts`、`views/projects/{ProjectSettingsView,ProjectVariablesView,ProjectVariableEditor}.vue`、`tests/project-variables{,-client,-session,-state}.spec.ts`；省略完整前缀者均在 `web/src/` 下。
- Fixture 8 路径：`tests/projectvariable/project_variables_web_test.go`、`tests/account-captcha-web/project-variables.config.js`、其 `e2e/{project-variables.helpers.ts,project-variables.spec.ts,task-review.helpers.ts}`，以及 `.agent-state/{agent-system-integration/schema-entry-controls.py,task-planning-recovery/pg_only_supervisor.py,work-owner-http/root_chain_driver.py}`。
- 不新增依赖、迁移或后端产品；root 另授 `docs/development/frontend/README.md` 普通 Variables 小节、`docs/development/agent-team/tasks.md` 对应一行及本页写权，不改其他模块全局摘要。

## 当前检查事实

组合冲突已由 root 保存到上述起点：`schema-entry-controls.py` 同时保留 Variables selector/env/profile 和 driver 的本树 `REPOSITORY` 私有依赖路径。合入时的 Skills header 安全布尔诊断、原门禁及 Milestone agents 观察保留。三个 Python AST、三个 TypeScript fixture/config.js 语法、两入口 SOURCE_HUNKS 原基线反向投影及差异空白检查已通过；只证明静态范围。

本轮发现 `schema-entry-controls.output()` 未为 `^TestProjectVariablesWeb$` 生成必需的单条 Node wait 正常样本。只补 source、label、单 PID 与 selector 四处分支；supervisor 的真实接受条件及预算不变。

本树已按锁定版本独立执行 `npm ci --prefix web`（146 包，exit 0），缓存仅为本树 `output/ai/project-variables-web/npm-cache`；未改锁、未新增版本、未借其他树 node_modules。所有前端命令设置 `NODE_DISABLE_COMPILE_CACHE=1`，Vitest 最多两个 worker，临时文件、Vite 缓存与产物均在本树 `output/ai/project-variables-web/frontend/`。后续另行授权的 Go/browser/PG 原真实链见下节；主链及本轮 native 窗口内均没有外网操作。

作者基础检查保留各次真实结果，不覆盖原 FAIL。以下日志均相对上述 frontend 输出根：

| 日志 | 实际结果与范围 |
| --- | --- |
| `npm-ci-01.log` | 独立锁定安装 exit 0，146 包 |
| `unit-01.log` | exit 1；17 文件中 14 个加载失败，3 文件 15 项通过；首因为 useSession 重复导入 shape |
| `type-01.log` | exit 2；重复 shape 及取消按钮不支持的 secondary variant；构建尚未启动 |
| `unit-02.log` | 两编译修复后仅重跑受影响 14 文件，81/81 通过 |
| `type-02.log` / `build-01.log` | 类型、首次实际生产构建 exit 0 |
| `unit-03-restore-repro.log` | 修复前真实 Session + ProjectWorkspace 复现：同身份 checking 临时空路径丢 Unknown；1 FAIL / 6 PASS |
| `unit-04-conflict-repro.log` | 修复前实际页面复现：VERSION_CONFLICT/not_committed 清空 editor/target/version；1 FAIL / 3 PASS |
| `unit-05-must-fix.log` | 两 must-fix 首轮三文件 15/15 通过 |
| `unit-06-must-fix-final.log` | 最终三文件 19/19 通过，exit 0，4.36s；含拒绝保留 presence、冲突重读 401/403/404 清理 |
| `type-03.log` / `build-02.log` | 最终类型 exit 0、生产构建 exit 0（2.11s）；dist 已更新 |

最终变化为去掉重复 shape 导入、按钮改用既有 ghost variant；拒绝保留 draft/touched/target/version，冲突须显式重读并采用当前版本后再确认保存，既不自动更新版本也不自动重发。重读 401/403/404 仍清空输入与结果。同 Session checking 暂时隐藏/清除页面内容但保留 Session 内原 Unknown 意图；匹配身份和项目恢复后只有原 key/full body Lookup，真实 Session 或 Project 切换仍清理。三个既有测试文件新增 8 项。合并未受影响的原证据，共有 17 文件 104 项作者定向覆盖，**并非一次最终全 17 文件运行，亦非独立/真实浏览器验收**。

前端七路径修复已由 root 保存到 `bb468f12`，独审接受后复用证据；Skills 技术路径由 root 单独同步，本实现者未修改其观测段。旧 Variables `60dbcee7` 的 CRUD03 有限通过和 Authority03 整体失败不代本轮验收。

## 原真实链与退出事实

本树另以锁执行 `npm ci --prefix tests/account-captcha-web`，85 包、exit 0；三个 Playwright 包均为 1.56.1，使用 `/usr/bin/chromium` 151.0.7922.173，实际二进制 SHA256 为 `d387400aaf740ccb75e5e996a34aa0940e6e97683a4eabd6ae34c1eed6804723`。没有重试旧浏览器下载。MinIO 从已释放 Skills 树只读普通复制到本树 `output/ai/deps-minio/bin/minio`，精确 SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 验证通过，未重编。

`output/ai/project-variables-web/native-prep/controls-01.log` 保留首次 8 项中 1 ERROR：原 exec mock 遗漏 Variables 环境，导致 `_EVIDENCE` KeyError。仅补该 selector 的环境/原 exec 断言及浏览器 wait/asset 负例枚举，`controls-02.log` 的两项受影响检查 PASS；其余六项证据复用。root 已保存为 `481f526d`，原 supervisor 接受条件、资源与预算未改。

准确 Go 1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 CGO_ENABLED=1 GOFLAGS='-mod=readonly -p=2'` 下执行：

```sh
/workspace/toolchains/go1.27.1/bin/go test -race -tags=integration -c -o output/ai/project-variables-web/compile-01/project-variables-race.test ./tests/projectvariable
output/ai/project-variables-web/compile-01/project-variables-race.test -test.list '^TestProjectVariablesWeb$'
```

会话 `73009` 实际 exit 0，`compile-01/compile.log` 空，`list.log` 精确命中该一顶层。仅在 root 与 Skills lead 释放的窗口复用 Skills 树 `output/ai/skills-milestone-resume/{go-build,go-mod}`，本树 `go-tmp/go-config/docker-config` 私有；native 结束后已向 root 明确释放缓存写窗口。

原运行命令为：

```sh
/usr/bin/python3 .agent-state/task-planning-recovery/pg_only_supervisor.py --driver .agent-state/work-owner-http/root_chain_driver.py --binary /workspace/agenteam-project-variables-resume/output/ai/project-variables-web/compile-01/project-variables-race.test --run '^TestProjectVariablesWeb$' --output /tmp/pvr-v1 --root-chain
```

由同一外层 Python 进程先核 fresh 磁盘不少于 5 GiB，再以 `subprocess.run` 取得实际返回；完整无凭据环境与命令在 `output/ai/project-variables-web/native-01/invocation.json`，退出在 `result.json`，原 stdout 在 `supervisor.log`。Variables DIST 为本树 `frontend/dist`，EVIDENCE 为新建 `native-evidence-01`；`NODE_DISABLE_COMPILE_CACHE=1 PYTHONDONTWRITEBYTECODE=1`，没有外网、并行 native 或输入写入。

`native01` 的唯一 `create-update-and-clear` 正常链全尾 PASS：Playwright 一项 6.2s，Go 顶层 17.43s；13 个已观察 API 请求全部 finished，10 份 Variables proof 覆盖初始目录、三次写和三对 Get/List。浏览器一次 Create、两次 PATCH（含显式空值）、零 Lookup；服务后验以原 key Lookup 三条历史 receipt，验证 canonical/Audit/Outbox 及其他 Project/Work/Secret/runtime 事实不变。正常链不代真实权限/Unknown 全矩阵或完整 CRUD。

Node PID 50255 actual_wait success=true、driver PID 48214 actual wait code 0。`/tmp/pvr-v1/ui-cafa3797ce3f44cb.log` 记录七资源 14 条双轮 absent=True、private/runtime/descendants 双清、exact top/metadata 门 true、HOST_TCP 两次空差量与 `inputs_unchanged=True terminal=0`（107.642s）。外层工具会话 `3954` 实际 exit 0（109.217s）；不是以后验无进程替代原 Wait。资源窗口已释放，无后台任务。

两图为 `native-evidence-01/project-variables-create-update-and-clear-{1440-light-updated,390-dark-empty}.png`，浏览器还核键盘确认、焦点、减少动效及无横向溢出；作者和独立审查均已实际查看。root 已回传 native01 最终有限接受，无 must-fix。本次未新增验收归档。

## 精确前端复跑方式

cwd 固定为 `/workspace/agenteam-project-variables-resume`。最终单元命令用 `NODE_DISABLE_COMPILE_CACHE=1 NO_COLOR=1`，`TMPDIR=$PWD/output/ai/project-variables-web/frontend/tmp-unit`、`XDG_CACHE_HOME=$PWD/output/ai/project-variables-web/frontend/xdg-unit`，执行 `node --input-type=module`，原 Node API 表达式为：

```js
import {startVitest} from './web/node_modules/vitest/dist/node.js'
const base='/workspace/agenteam-project-variables-resume'
const names=['project-variables-session','project-variables-state','project-variables']
const ctx=await startVitest('test',names.map(name=>`src/tests/${name}.spec.ts`),{root:`${base}/web`,config:`${base}/web/vite.config.ts`,watch:false,cache:false,maxWorkers:2,configLoader:'native'},{cacheDir:`${base}/output/ai/project-variables-web/frontend/vite-unit-cache`})
if(ctx.state.getFiles().length!==names.length) process.exitCode=1
await ctx.close()
```

首次 17 文件的 `names` 为 `project-variables-client, project-variables-session, project-variables-state, project-variables, skill-owner-client, skill-owner-state, skill-owner-view, milestone-create-page, milestone-create-state, work-milestone-planning-client, work-milestone-planning-session, sprint-create-state, sprint-start-state, work-review-state, work-review-session, work-sprint-lifecycle-session, session`，路径均为 `web/src/tests/<name>.spec.ts`。unit-02 只排除首次已通过的 `project-variables-client`、`skill-owner-client`、`work-milestone-planning-client`，其余 14 同表达式执行。

类型及构建使用相同 NODE_DISABLE/NO_COLOR，TMPDIR/XDG 分别改为 `frontend/tmp-build`、`frontend/xdg-build`。实际按顺序执行（类型非零不进入构建）：

```sh
node web/node_modules/vue-tsc/bin/vue-tsc.js --noEmit --project web/tsconfig.json
```

随后 `node --input-type=module` 使用：

```js
import {build} from './web/node_modules/vite/dist/node/index.js'
const base='/workspace/agenteam-project-variables-resume'
await build({root:`${base}/web`,configFile:`${base}/web/vite.config.ts`,configLoader:'native',cacheDir:`${base}/output/ai/project-variables-web/frontend/vite-build-cache`,build:{outDir:`${base}/output/ai/project-variables-web/frontend/dist`}})
```

日志各次独立重定向到上表文件，后续运行须用新文件名保留历史。最终 dist 为 `/workspace/agenteam-project-variables-resume/output/ai/project-variables-web/frontend/dist`。

## 交付与下一步

本片限定候选、两项风险修复和 native01 原正常链已独立接受。当前只待 root 保存与正式 Git 集成；本轮新增待保存内容仅三个文档范围：前端 README 普通 Variables 小节、任务台账对应有限 UI 一行及本页。文档链接、差异空白与锁未变检查通过后停写；不新增测试或扩大真实矩阵，不因 Git 哈希再改产品输入。后继 Knowledge 真实联调另行派工，不复用本次 native 资源。

driver 的 owned 输出根为 `output/ai/project-variables-web/`，dist/evidence 与 npm/tmp/config 保持本树隔离，共享 Go 缓存仅在已授权窗口使用；原短 runtime、实际 Wait/退出及资源清理门不变。历史沿[组合说明](agent-system-integration/README.md)、[前置协调记录](agent-prerequisites/coordination.md)及[任务台账](../docs/development/agent-team/tasks.md)引用；继承摘要可从 `1b6ad53e:.agent-state/current.md` 查看，不复制旧物理树布局。

默认生产 Project initializer、完整 participant/F1、Loop/Tool 与平台 ready 未完成，`ready=false`/`readyz` 503 保持。Object Runtime join、OpenAI tools 独立动态验收、Central SPA concurrent-publication、Jina/Image 来源及其他显式 STOP 不恢复；本片不改变整体进度或 E01 未开始事实。

实例要求沿仓库 `gpt-6-astra / ultra / priority`；工具没有 `service_tier` 参数，本轮实际 Fast 档位未确认。
