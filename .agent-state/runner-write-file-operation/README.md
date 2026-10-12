# Runner write-file Operation 方法

本目录保存已获独立验收的显式 write-file Operation 首链方法。基础检查及真实 PG/认证 TLS/WSS 的 `native01` 已通过；当前等待 root 正式 Git 交付。本片使用受控 Model JSONCaller，Project 沿原持久 test Skills receipt；Owner、Registry、Preparation、Execution、Tool、Mount 与 Runner/Manager 走正式调用，不将这些测试边界称为真实 Model tools HTTP 或生产 Skills 完成。生产默认注册、后续 Transcript 消费及下一轮 Model 不在范围内。

精确入口 `TestWriteFileOperation`，两个场景：

- `committed-attempt-write-and-lookup`：真实 Attempt COMMIT 后 hold 原返回，零 wire；原返回后同持久 RequestID 仅一次 WSS 写入，512 KiB 独立分块读取完整比对；最终真实 COMMIT 后丢回执，仅原 Lookup 恢复，不再 Execute。
- `revoke-before-claim-no-dispatch`：Attempt 原返回后，在 Claim 原事务开始及取锁前暂停；普通 Owner 正式撤销 Mount，原旧票零 wire，原文件不变，Attempt 以 `not_dispatched` 退休。

`profile-controls.py` 只检查新 profile 和旧 AST/有效映射保留，以及真实原 gate 的正反行为；不启动资源。`run-pg-checks.py compile --output <fresh>` 使用原完整输入枚举、离线 max1/-p1/race/integration-c 和精确 list；`prepare --compile <passed> --output <fresh> --namespace <fresh-absolute>` 只生成原 supervisor 配方。生成目录中的 `record-native.py` 仅在 root 授独占窗口后运行一次，亲收外层 actual Wait；原七资源、全部尾门和 75 秒 HOST_TCP delta 双空规则原样保留。失败原件不覆盖，不自动重试。

Execution/Loop 的显式 write 构造已接入。`run-basic-checks.py` 只选 30 个新 top 和 14 个本轮受影响旧 top（7 个测试包），另 vet 8 个实际包；沿原 `go list -deps -test -json` 实际源码/测试/embed 闭包及每阶段前后稳定记录，不运行 PG/native。基础与 PG 均须 root 另授窗口后执行，未将源码完整记为通过。root 已将全部已提交冻结的同一分支从临时源码树迁到 `/workspace/agenteam-runner-write-file-operation`，基础首次执行已在此合规工作树完成；原 Identity 逐祖先 `0022` 拒绝门、driver 自动 cwd 及环境覆盖保持。旧 read-file helper、已验 Runner write WSS/local 场景不改、不重复扩大矩阵。全部 Git 由 root 保存。

`methods01` 的原 profile 1 正例、7 反例及旧 AST/有效映射保留检查实际通过。`basic01` 在 Tool runtime 测试的未使用 import 处编译失败，该包 10 个 top 未运行；其余 6 包 34 个 top、109 个 sub 实际通过，无业务测试失败，8 包 vet 当轮未启动。原整轮 FAIL、日志与输入记录保留。默认 `--scope full` 仍选择全部 44 个 top；修复后 `basic02 --scope runtime-followup` 仅补原 runtime 10 个 top、36 个 sub 与原全部 8 包 vet，实际全 0。两轮各自 829 项输入前后相同，轮间仅获授权的脚本选项和 unused import 修复改变；复用其余六包结果，合计 44 top / 145 sub 分阶段 PASS、8 包 vet PASS，不将 `basic01` 改称通过。

`compile01` 的原 race/integration 编译和精确 list 实际退出 0；技术输入 `c1530e95`，1,928 项编译输入 before/after/after-list 相同，包含 85 个同包 Go 文件与迁移69。运行闭包仅增加实际 binary 为 1,929 项；binary SHA256 `9c24f9c94bf99911652789d55351f5db571650ad793f40a024ba14978a71b171`、88,806,596 B。原工具 94027 完整退出 0，未用 list 代替业务执行。

`native01` 原工具 34580 实际退出 0：Go 858887 与 driver 857092 原 Wait 0，supervisor 857048 原 Wait 0 / 165.703 s。`TestWriteFileOperation` 两 sub 全 PASS（top 68.27 s，分别 36.08 s / 32.19 s）；首条真实 Attempt COMMIT hold-return 期间零发送，原返回后同 RID 唯一 WSS 写入，512 KiB 独立完整读取，最终真实 COMMIT 丢回执后仅 Lookup 恢复；第二条正式 Owner 撤销 Mount 后原票零发送、原文件不变、`not_dispatched` 退休。原七资源十四次精确 absence、descendants/private/runtime 双空、自动 HOST_TCP 75 秒门内差额双空及 1,929 项输入不变全部通过，唯一主审已接受该有限 whole 结果。新阶段没有后继 Model 或 Transcript 消费，readback 仅为测试后验，不参与产品提交状态判定。

完整 output 已于保全工具 94730 actual 0（PID 862381 / 1.702 s）用同文件系统 `NOREPLACE` 迁至 `/workspace/agenteam/output/ai/retired-20261012/runner-write-file-operation/`：95 个原文件、97,809,643 B、15 个目录（含根）的 path/SHA/size/dev/inode/mode/uid/gid/nsmtime 全部保持，设备号 27；原 `basic01` FAIL、后继结果与实际 binary 均在。旁目录 `runner-write-file-operation-preservation/` 的 `output-before.json`、`output-after.json`、`result.json`、`tool-terminal.json` 保存迁移证明；`source-history.json` 将 25 份 map 的 17,934 个仓库项、1,931 个唯一 blob 逐 SHA 对应到 `79d88efc` / `c1530e95`，目前只证这两提交为本分支祖先，main 完整历史可达仍待 root 集成。

原 `/tmp/rwfo01/pg-cc48de80e47f4502a2625b69f80ad9db.log` 及同名目录的 `owned.json`、`request.json`、`go-config/go/telemetry/mode` 共四文件、7,292 B 已 exclusive 复制到上述证明目录的 `external/rwfo01/`，保留原相对结构，原 tmp 未改；核验见 `external-copies.json`。`process-audit.json` 只证明定向可见范围内 17 个原已知 PID 缺席，不冒称全主机扫描。迁后原工作树 untracked/ignored 为零，后核工具 2c8a36 actual 0 见 `post-preservation-check.json`；工具链、热 Go cache 与只读 modules 未迁移。原 invocation 的历史绝对路径不改写，未来执行须从源码和现存依赖生成 fresh 配方。本任务树、本地与远端分支清理尚未发生，正式 main 交付、推送和清理由 root 后续核定。
