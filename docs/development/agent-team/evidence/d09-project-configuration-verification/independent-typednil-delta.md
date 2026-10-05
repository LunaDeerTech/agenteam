# D09 typed-nil channel 差量复审

2026-10-05。**原静审 F1 阻断已消除，System 原构造分支不变；其余 12 生产源可复用前次静审结论。** 本次未运行 Go/Docker、未读活动测试、未修改仓库/Git，结论不是完整 D09 行为验收。

固定基线 `81fe7427ceb4672247b3d30a51c10a2e2808ba04`。rev2 manifest SHA-256 `8e4fc45cf8396774172bfb191fb8fd22f99a5d4761f275aeb4595901639bcfaf`；13 个实际固定文件全部匹配，仅 `model/authority.go` 相对 rev1 改变。由前次自有 rev1 副本与固定 rev2 源生成的实际差量逐字节等于指定 patch，SHA `ce0d00732a6d56de48ff6745364b6ef0152c0c5dd827b261252a28b3ff9be07b`。

修复只加 reflect import 和可选 Projects 分支的 `value.Kind() == reflect.Chan && value.IsNil()`。普通 nil Projects 仍绕过新分支；typed nil channel 被拒绝；原 nilPort 已覆盖的类型继续拒绝；其它具体类型不会调用 IsNil，避免反射 panic。原 Store/Sessions/System 必需依赖 predicate、Authority 状态捕获、无 I/O 构造，以及只读 service.go 的 nilPort 均未改变。没有授权绕过或其它安全影响的复现声明。

作者原红 `typednil-channel-rev1-red.log` SHA `100bf378c97886a4964a33e706223d67748c9189a8a774be71c1eaff3b64762d`，记录指定构造测试 `got <nil>, want DEPENDENCY_UNBOUND`。修后 `pure-race-rev2.log` SHA `73ba3571f69090d3c2207b31b34c6205947746f59f66c8cdda69b8a0fd05879f`，记录 Model、Project 及二者 contract 四包 OK。这里仅核固定日志内容；未读取活动测试体、未独立重跑，不将日志名当完整命令/输入证据或扩展为集成通过。

实际执行仅 Python SHA、13 项 manifest 比对、difflib 补丁精确比对与自有报告格式检查，详情见 checks.json。前次静审的 Project receipt/Unknown、目录、引用、Secret/Audit 与 Outbox 双阶段结论及所有动态门槛保持；前次报告不改写。报告冻结后 all-stop，等最终候选与正式行为验收。
