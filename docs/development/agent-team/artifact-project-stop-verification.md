# Artifact 项目停止：本域有限验证记录

主线程已采纳 review09 的 Artifact 本域有限结论；**完整 Artifact 卡与共享 Object guard 仍为 BLOCKED，16 个候选业务/测试源尚未提交。** 本记录归档已完成的独立核对与原始执行证据，没有新增测试通过声明。[原卡 rev3](../work-items/recovery-artifact-project-stop.md)的预算、API、16 路径及更强恢复门槛保持。

固定输入为 `6658a6cb1f29299521773bc8dc86b2f607b8c809` 加 review09 的 8 生产、8 测试源；[候选清单](evidence/artifact-project-stop-verification/candidate/manifest.json) SHA-256 为 `79e5eb812c0fda2a42d390a7e2703b1ebca4ce581c055e1c2a99fda1ab76914e`。独立验收者 `skill_verification` 的[原报告](evidence/artifact-project-stop-verification/verification/report.md.txt) SHA 为 `d68a2857d75cf66c007e97c6b4e147e6734ec09407cee4fe9354393f2feffdde`；本归档由 `recovery_verification` 按冻结定位清单整理，不修改作者或 V 原件。

## 实际覆盖与复用

| 固定输入和实际证据 | 新顶层 | 旧顶层 | 可认定粒度 |
| --- | ---: | ---: | --- |
| review09 原日志 | 3 | 32 | 35 个显式顶层 PASS |
| review08 原日志适用复用 | 21 | 5 | 26 个显式顶层 PASS；旧兼容组的整体 exit 1 仍保留 |
| review07 固定六选包 | 6 | 0 | 精确 selector 与包 exit 0，原输出非 verbose |
| 合计 | 30 | 37 | 61 个显式顶层 + 6 个 selector 支持，55 个去重显式子例名称 |

这些是分版本证据，没有最终 16 源一次 67 项全绿记录。六选不补造逐 top/subtest 输出；其它 driver 包的 no-tests、compile-only 和 child helper 不计入集合。逐 67 项的 run、输入和日志 SHA 见[覆盖映射](evidence/artifact-project-stop-verification/verification/evidence/case-evidence.json)，原日志独立解析见[核对记录](evidence/artifact-project-stop-verification/verification/evidence/raw-checks.json)。

review08→09 仅在 ordinary `within` 的真实 Unknown 时设置标记，并在实际 body/Discard 已返回后跳过新增的退场确认等待；业务 CommitResult、正常 Committed/NotCommitted、权限/gate、实际 Close、source lease/原 writer 与最终 EX 确认责任不变。直接受影响的三创建入口×三结果 admission、源捕获/最终 publication、旧源恢复与 gate 恢复已在 review09 实跑。未受影响的正常、权限、撤销路径按限定语义复用，不只凭测试文件未变判断。

review07 六选另核函数身份、integration 标签/driver、无替代 TestMain、六函数与 fixture 主路径无 Skip/Short，以及相关生产算法差量。[复用记录](evidence/artifact-project-stop-verification/verification/evidence/reuse-function-checks.json)逐项说明 selector 身份、typed tuple、source planning、65 成员/ABA、公平 pre-work 取消与 local join Unknown 的适用范围；不外推到后来新增的 foreign/mixed 路径。

## 原失败、修复和独立增量

原 F1 拒绝路径 Close、F2 join Unknown、F3 身份错配、source planning、页外 selector、早期 compile/CLI 失败，以及 review08 新 Unknown 两红和旧 publication 红全部保留。原 52 个历史日志、已有 input/exit/清理沿[原索引](evidence/artifact-project-stop-verification/author/evidence-index.json)归档；review09 续跑、失败、修复及 pure24 的 76 项沿[增补索引](evidence/artifact-project-stop-verification/author/evidence-addendum-review09.json)逐 SHA 保存。没有原 argv/input/exit 的历史项仍缺证，不能凭后来成功反填。

最后 Unknown/publication 修复已独立静审，未采用只移动测试计时的方式。修后原日志为新 2 + 旧 1 的 3 顶层/6 子例 PASS；Discard `2.004773423s`、公开 Unknown `2.004977697s`，原 `3s` 预算不变。gate 已提交 cursor 1，之后 2/3/0/1 四轮保持 Pending，第 4 轮精确取消原 context，仍须真实 resolver 释放才终态；review09 admission 1 顶层/9 子例也已实跑。原红没有等待栈，旧 base 已有 cancelSource 的 2s，不把“2+2”写成唯一动态定因。相关[归因](evidence/artifact-project-stop-verification/static/unknown-attribution.md.txt)、[review09 差量核对](evidence/artifact-project-stop-verification/static/review09.md.txt)和逐字节 raw patch 均保留。

独立 F1 纯 probe 的[原命令](evidence/artifact-project-stop-verification/independent/f1/evidence/command.json)为 Go 1.27.1，`go test -race -count=1 -timeout=6m -run '^TestIndependentArtifactRunningCancelledUploadTracksActualClose$' -v ./internal/central/artifact`。running service 上已取消 context、未 StopAdmission，实际 Close 仍被持有时 Read/DB 为 0、原 control 不退出，Force 原 `30ms` 到期仍 Joined=false；放行后 Close 一次并收敛。[报告](evidence/artifact-project-stop-verification/independent/f1/report.md.txt)保留原 setup 失败、修后命令和通过日志。旧纯快照未在 review09 重跑，按零 DB 拒绝分支和实际 Close 生命周期未变限定复用，不扩成 PG/guard 证明。

独立 foreign 在 review09 有效 overlay 上真实 1 顶层 PASS（测试 `3.28s`，objects 包 `4.305s`）。真实 Kill+Wait、原 guard/技术 work、同 Tx 完整 tuple/Authority 与实际 join Committed 后，正式 Store 装饰器**模拟返回 Unknown**；另一笔普通 SH 阻止完整 EX 确认期间不能 Joined，放行后 Drain 成功。[原报告](evidence/artifact-project-stop-verification/independent/foreign/real/report.md.txt)、[command/env](evidence/artifact-project-stop-verification/independent/foreign/real/command.json)、probe、实际日志与两次资源清零均保存。这不是网络 Unknown 或原 writer 仍活的证明，也不计入作者 30 项。

最终作者 race24/vet24/compile24 的 16 项输入均为 review09 且 exit 0；race 的 artifact/contract 为 `1.159s`/`1.015s`，compile 的 `1.017s` 明确是 no-tests。独立 foreign 在 review09 重新 race 编译 `7.046515s` 后实跑。两 cmd build23 只有 review08 的实际通过记录；review09 未扩公共 API/import 图，可复用兼容构建证据，不声称最终 16 源重新构建过两 cmd。所有实际 argv/env/exit 保留原元数据，不另造简化执行值。

## 资源与未验边界

V 只读核对作者 12 轮续跑的 first/second 清理 JSON：每轮自有 4 容器/3 网络 exact ID 不存在，原 2 容器/4 网络的 ID/name/labels 不变，自有进程与 runtime 为空，末检源匹配。foreign 同样双清理，并补跨 process group 的实际 owned 程序为 0。这里引用冻结原记录，没有再次触碰已归还 fixture 窗口；作者及 V 均已 all-stop。

- [Object 普通锁竞争回归](object-runtime-join-regression.md)已证明 Drain/claim/flock 可能早于实际 join 终局，修复尚未整合。原实施任务因自动安全筛查中断而停止，不改派或重试；旧 S2/Runtime 局部通过不否定该反例。Artifact 共享 guard、完整卡及后继领域绑定继续 BLOCKED。
- failed ROLLBACK 后原 backend 仍持部分 Acquire 锁的动态证明未执行；已暂停方法未恢复。普通锁 canary、纯 Store 结果与 foreign 模拟不能替代它。
- 先真实 Unknown、后 NotCommitted 的混合聚合只有 pure 回归，没有新增真实混合矩阵；历史六选不外推到该场景。
- Project/app 正式装配、未来 source router、Cleaner 最终退休及完整 D05/D08 不在本次结果内。

## 持久化与复建检查

[证据入口](evidence/artifact-project-stop-verification/README.md)含完整索引、原路径映射、唯一 final16 副本、关键历史输入的小型差量、完整 F1/foreign 探针材料。原 `.go/.md` 只改归档文件名后缀，内容不改；原 patch 与日志逐字节保留；当前空白检查只排除[精确清单](evidence/artifact-project-stop-verification/whitespace-exceptions.json)中的 15 个原 patch 和 1 个原日志。没有缓存、binary、完整 snapshot、数据库、runtime、Docker 配置或已停止的 protocol 草稿。

文档阶段实际运行 Python 原件 SHA 核验、源码差量恢复/16 源重建、相对链接/fragment 与格式检查、限定路径的只读 Git diff 检查；结果见[归档检查](evidence/artifact-project-stop-verification/archive-checks.json)。未运行 Go/Docker/网络或产品测试，未执行 Git 写操作。检查与归档完成不解除上述阻塞，不自动授予业务重启或源码提交权。

归档空白检查补记：主线程首次检查已暂存 315 路径时，在 15 个 raw patch 之外发现唯一额外警告 `author/logs/join-pending-01.log:4`：`space before tab`。该原日志 SHA `e103146598e81a3f518742a4c46eeaad83dc66f54a0cf7e10e6a4d26241a95ea` 保持，按独立 `raw_log` 类型加入精确例外。此前检查覆盖全证据的尾随空白及限定文档的 Git 检查，未覆盖该原日志的缩进空格后接 Tab；[原检查记录](evidence/artifact-project-stop-verification/history/archive-checks-before-whitespace-correction.json)原字节保存，不能据此前 PASS 声称全 315 路径无警告。本次补记仅作 Python 原件/格式核对，没有执行 Git，主线程刷新暂存后另行复核。
