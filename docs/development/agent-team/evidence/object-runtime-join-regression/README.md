# Object Runtime join 回归证据

本目录归档固定 `42e3f7d59ceb65cb22cdd6b639608caa8830fae2` 加一个普通 PostgreSQL 锁竞争 probe 的**原始失败**。结论及边界见[正式报告](../../object-runtime-join-regression.md)。这是已知缺陷的证据，不是修复通过记录；没有使用代理、网络截断或异常连接保留。

## 输入与原件

- [input.json](input.json)保存 766 个基线源码/SQL/模块文件及唯一 probe 的 SHA-256、精确工具链、离线环境和编译 argv。其 `fixture_authorized=false` 是编译准备阶段原值；实际运行的后续授权、argv/env 保存在 [invocation.json](normal-lock-01/invocation.json)，不回写旧输入。
- [单 probe](snapshot/tests/objects/project_work_lifecycle_probe_test.go)是唯一额外测试源码；没有保存完整 snapshot、缓存、二进制或凭据。
- [原 17 项索引](normal-lock-01/SHA256SUMS.json)、原报告、完整 driver 日志、编译输出、真实运行结果、资源与进程记录均保持原字节和原相对目录。原 runner 仅作执行来源记录，保留当时绝对路径，不应直接在新环境运行。
- [baseline-check.json](baseline-check.json)记录本次纯文档归档的只读核对：766 项逐一匹配固定 Git blob，1 项匹配归档 probe；另外核对三个实际 driver 脚本与固定提交一致。没有重新编译或运行。
- [archive-provenance.json](archive-provenance.json)保存原件来源、大小和哈希；[SHA256SUMS.json](SHA256SUMS.json)绑定本目录冻结文件，不引用活动修复卡或活动业务源码。
- [static-only](static-only/report.md)保留先前因自动安全筛查中断而停止的静态任务原件。该任务没有写 probe/snapshot、没有 compile、动态测试或 fixture 资源；普通锁竞争结果不能替代其网络 ROLLBACK 场景的可达性验证。

## 仅重建输入

从仓库中固定的上述提交导出 `go.mod`、`go.sum`、`cmd/`、`db/`、`internal/`、`tests/`、`scripts/` 到自有空目录，再把归档的唯一 probe 复制到同名 `tests/objects/` 路径。按 `input.json.inputs` 逐文件计算 SHA-256，要求全部 767 项一致；三个 driver 脚本另按 `baseline-check.json` 核对。无需保存或复制整仓库文档、`.git`、缓存、运行目录或旧 fixture descriptor。

本次已实际核固定 blob 与 probe 指纹，未执行重建后的编译或运行。后续若获主线程正式运行授权，应重新核工具链/依赖、取得唯一 fixture 窗口并建立当轮自有资源与基线；沿 `invocation.json` 的 selector、`GOFLAGS` 和原 `-race -count=1 -timeout=6m`，不能把旧 runtime 路径、nonce 或权限声明复用为新授权。

## 结果与资源解释

[driver.log](normal-lock-01/driver.log)保留唯一目标顶层的四条失败断言；[result.json](normal-lock-01/result.json)为 exit 1、67.839 秒。其余 18 包的 no-tests-to-run 不计功能通过。编译成功由 [compile.json](compile.json)的实际 exit 0 记录支撑，空编译日志本身不充当退出码证明。

[resource-handoff.json](normal-lock-01/resource-handoff.json)原摘要只识别 `No such object`，没有识别 Docker 网络的 `network <exactID> not found`，因此部分布尔摘要为误判。实际两次 inspect 的 exit/stderr 原封保留；[独立解释](normal-lock-01/resource-handoff-reviewed.json)逐项核对后确认 4 容器/3 网络均不存在，原 2 容器/4 网络 ID/name/labels 不变，189 个记录到的子进程已退出，所属进程为 0、runtime/gotmp 为空。本次归档没有重新连接 Docker 或数据库。
