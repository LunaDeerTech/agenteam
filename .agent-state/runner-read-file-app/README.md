# Runner read-file App 方法

范围见 [任务卡](../../docs/development/work-items/d15-runner-read-file-app.md)。方法仅增加显式 Runner CLI `read-file` 的必要控制与真实 PG/TLS/WSS 小链；不扩 Central 授权、默认操作集合或原 tools HTTP STOP。原方法已独立静审接受；当前组合尚未执行 Go/basic、两份编译或 native。

## 基础控制

[run-basic-checks.py](run-basic-checks.py) 复用 [原 basic](../round-model-call-reservation/run-basic-checks.py) 的实际依赖发现、输入前后图、原 PID/Wait、JSON 精确选择与 vet 门。只选三个包的 7 top / 20 sub；三个包各 vet，不重复旧 WSS/Manager 全矩阵，不给 cmd help 文案增加镜像控制。

| 包 | 精确 top | sub |
| --- | --- | --- |
| `internal/runner/config` | `TestRunnerOperationsClosedConfiguration`、`TestRunnerOperationsStoredIdentityCheckStaysOffline` | 8 |
| `internal/runner/app` | `TestRunnerRootPassesClosedOperations` | 3 |
| `internal/runner/control` | `TestOwnedReadFileStoredIdentityAndDefault`、`TestOwnedReadFileRejectsBeforeDevice`、`TestOwnedReadFileOriginalReturnKeepsIdentity`、`TestOwnedReadFilePartialOwnerAndCancelledHandoff` | 9 |

```sh
python3 -B .agent-state/runner-read-file-app/run-basic-checks.py --output output/ai/runner-read-file-app/basic01
```

以上为后续授权窗口的命令，执行必须记录实际 JSON 的 7/20 与三包 vet。基础控制里的受控原返回屏障不冒充真实 kernel close 阻塞；真实进程/网络/身份退出由 PG 首链核。

## 两份编译及独立 prepare

[run-pg-checks.py](run-pg-checks.py) 复用 [原 PG 方法](../round-model-call-reservation/run-pg-checks.py) 的成功后 prepare、原 `Popen.wait`、fresh output 与 FAILSTOP 规则。唯一必要扩展是两个真实 `go list` 编译闭包及两个产物：

- `go list -race -mod=readonly -p=1 -deps -json ./cmd/agenteam-runner`，随后 `go build -race -mod=readonly -p=1` 构建 `agenteam-runner`。
- `go list -race -tags=integration -mod=readonly -p=1 -test -deps -json ./tests/projectvariable`，随后 `go test -race -tags=integration -mod=readonly -c -p=1` 构建 `projectvariable.test`，只列出 `^TestRunnerReadFileApp$`。

完整同包源码、所有现存 migrations/embeds、原 conservative native provenance 保留；另将实际两组编译 Go/C/assembly/embed/module/stdlib 文件纳入 union。生成的 `.test` main 排除并单独记录。仓库源在发现前绑定，外部只读工具链/modules 在发现后、任何 build 前冻结；两 build 和 list 后各核同图。候选 provenance 与实际两个 compiler 子集分别记录，不将候选集合冒充真实编译集合，不预设文件数量。

编译成功生成 `runner-read-file-inputs.json`：严格本 selector/schema、两 sibling artifact 的 path/SHA/size、source union、两个实际 compiler 子集。源图不含自身或产物；最终 runtime 图增加两 binary 和 descriptor 自身 SHA。`AGENTEAM_RUNNER_BINARY` 必须等 Runner artifact，PG 参数必须等另一个 artifact。仅本 selector 的 [driver](../work-owner-http/root_chain_driver.py) helper 校验它们；缺 descriptor、Go 占位产物、symlink、身份不符、源或产物漂移均拒绝。compile 私有源收集器与正式 native 入口分开，没有 skip 标志。

```sh
python3 -B .agent-state/runner-read-file-app/run-pg-checks.py compile --output output/ai/runner-read-file-app/compile01
python3 -B .agent-state/runner-read-file-app/run-pg-checks.py prepare --compile output/ai/runner-read-file-app/compile01 --output output/ai/runner-read-file-app/native01 --namespace /tmp/rrfa01
```

prepare 不运行 Go、binary 或服务。它重核两 actual build、exact list、源及两 artifact/descriptor，然后生成原四身份：`record-native.py`、`invocation.json`、`input-hashes.json`、`plan.json`。recipe environment 显式给 PG 父进程 `AGENTEAM_RUNNER_BINARY` 与 `AGENTEAM_RUNNER_READ_FILE_APP_INPUTS`；descriptor proof 也进入 plan。原 recorder→supervisor→driver→PG 的继承 env 链传递它们，不增加共享任意 extra-input 接口。PG 只 `exec` 预编译 Runner；Runner 子进程使用 fixture 原显式配置白名单，未继承该两个方法变量，不在 native 内构建。

## Native 与方法离线控制

精确 1 top / 3 sub：`TestRunnerReadFileApp/explicit-cli-range-and-identity-restart`、`default-empty-capability-unsupported`、`invalid-root-and-explicit-config-before-connect`。以 [真实 fixture](../../tests/projectvariable/runner_read_file_app_linux_test.go) 为准，覆盖显式 CLI enrollment/stored identity 的真实 range、默认 capability 拒绝、配置错误先于连接及原退出。Runner CLI 的实际 Wait/身份/Manager 退出属于 fixture 与原 descendants 链，不虚构第八个 PG 资源。

`record-native.py` 与 [原 recorder](../round-model-call-reservation/record-native.py) 逐字相同，SHA `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`。必须另获 native 窗口才运行：

```sh
python3 -B output/ai/runner-read-file-app/native01/record-native.py
```

[原 supervisor](../task-planning-recovery/pg_only_supervisor.py) 保有 540+60 秒、Go 6m、原七资源双缺席、private/runtime/descendants 双尾、75 秒自动 TCP baseline delta 双空及原 actual Wait；FAIL 本轮停止，保原件，不重试、换 namespace、预热 baseline 或过滤 TIME_WAIT。方法不解除任何已有产品 STOP。

[profile-controls.py](profile-controls.py) 离线检查 metadata 1 正/7 反，以及专属 descriptor 正/反控；仅 synthetic 小文件，不执行它们。剥离本 key/helper/两 branch 后的完整共享 AST 与 baseline `33feecbd6e50ec94fb32c258f980f3332e5c70ee` 的预先记录 hash 相同，旧 effective maps/函数/预算保持；`TARGETS` 原推导不改。运行命令如下，不涉及 Go/服务：

```sh
python3 -B .agent-state/runner-read-file-app/profile-controls.py --output output/ai/runner-read-file-app/profile01
```

当前组合头为 `88dfa691582480cc10f3e6bf081431c1c84184ea`：Runner `4c715a5e` 普通合入 main `33feecbd` 后，`256e27ac` 曾遗漏原 descriptor helper/两分支；静态 prewrite 检查 `4f98bf` 在修改前拒绝，`88dfa691` 仅恢复原 `4c715a5e` 三段字节。原提交与 [静态发现](../../output/ai/runner-read-file-app/baseline-prewrite01.json) 保留，不属于 Go/PG 运行失败。

本次仅将 `BASE` 与两枚 `BASE_AST_SHA` 对齐该 main；剥除 Runner 唯一 key/helper/两 branch 后，完整共享 AST 与 main 原件相同，包含既有 Model、Agent、App profiles。selector、descriptor、预算和 Wait 算法不变。

旧 [profile01](../../output/ai/runner-read-file-app/profile01/) 与 [profile02](../../output/ai/runner-read-file-app/profile02/) 都在原 `3a495115` 基线实际离线通过（各 metadata 1 正/7 反、descriptor 1 正/17 反）。profile02 绑定最终 README 字节；其间只整理 recorder 调用示例，未发生 profile01 失败。原小证据保留，不等于新组合已运行；新组合实际离线控制及 Go/PG 仍待独立窗口。
