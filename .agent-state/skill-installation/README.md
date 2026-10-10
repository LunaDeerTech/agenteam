# Skill Install：首次真实 PG/Object 调用

本入口只执行 `^TestSkillInstallationPersistentObject$`，1 top、2 个直接 sub：`install-read-replay` 与 `ordinary-cleanup`。fixture 为 `tests/projectvariable/skill_installation_test.go`（作者冻结来源 `46a95480`），已获有限独立源码审查。cleanup 继续独占树 current 和纯检查记录；本文件只记录 PG 入口准备与后继实际结果。

复用原 metadata/schema 同包 PG family：driver 的 `METADATA_INPUTS` 只声明每项必需输入，supervisor 的 `METADATA_GROUPS` 只声明完整预期用例集。所有条目共用精确模式、结果/唯一原 Wait、初末输入重枚举和资源退出实现。旧 metadata 保留历史输入，schema 与安装条目不将本 README、current 或纯控制加入运行清单。缺必需源码或不匹配的 selector 明确拒绝；不得为缺少的 schema fixture 制造空替身。

运行沿固定 Go 候选、原 `scripts/test-objects.sh` 七资源链及其 nonce/原实际 Wait、私有目录/runtime、后代进程、HOST_TCP 和初末输入门。预算仍为 Go 6m、root 540s、终止宽限 60s、kill Wait 3s、TCP 尾 75s。安装未增加环境变量、浏览器、Node、Schema 解释器或新监督器。配置命令保持：

```text
python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver <本树>/.agent-state/work-owner-http/root_chain_driver.py \
  --binary <本树固定 integration race candidate> \
  --run '^TestSkillInstallationPersistentObject$' \
  --output <本轮新 owned 目录> --root-chain
```

源码冻结后只做所需 integration/race 编译与精确 1 top 列举，再一次收集当前运行依赖；本条命令须等 root 的资源窗口，不在准备阶段执行。当前入口离线新增 4 控、受影响旧 metadata 6 控实际通过，均为受控输出/文件/资源观察替身，无 Go、Docker、PG 或 Object 调用。

真实断言范围：当前普通 Owner Install → 原 package EOF/Close/Joined → 正式 Lookup 与同 key replay → 非 Owner 拒绝及原持久事实；随后明示的生命周期 fixture 进入真实 Skill/Object stop 和普通来源清理，核 SQL anchors/Audit 与原对象 `NoSuchKey`。它不证明完整 Project 删除、Agent/F1、AgentRun 安装、Registry install Backend 注册、外进程恢复或解除旧 Object Runtime STOP。连续 00032–00036 SQL 与真实安装/删除尚未执行；失败时保留原事实及全部尾，不自动重试。

首次编译 `compile-01` 已实际 FAIL（2026-10-10 13:19:00–13:19:27 UTC，session38864→4240b9）：fixture 第180/184行把 Object ProcessID 直接传给 Outbox ProcessID，两个不同类型不能赋值。原 Go Wait1/26.457s，outer Wait1；未执行 list、无候选、未启动 PG/Object。538 个编译输入和原方法输入初末一致，原进程组双 absent、后代与 adopted 均空、runtime 双空。输出位于 `output/ai/skill-install/compile-01/{compile.log,result.json,inputs-before.json,inputs-after.json}`；终端 `tails_closed=false` 同时包含原非零 exit 判断，不能据此反推有遗留进程。该 fixture 差异交原作者窄修，原 FAIL 不回填。

原作者仅把同一个 ProcessID 字符串经正式 Outbox typed parse 转换（`f5945be0`）。修后 `compile-02` 实际 wholePASS：session11185→c01554，13:21:50–13:22:01 UTC，compile/list 原 Wait0（9.084s/1.051s）、精确列举上述1top，538个输入及方法初末相同，原进程组/runtime双空、后代/adopted空。候选为 `output/ai/skill-install/install-race.test`，47,749,377B，SHA256 `9f2a44bdb449191b072a75d527317a00608738b3d57cd1b8dd44d0d74257732c`；编译通过不代表业务执行。

首次真实 `native01` 整体 **FAIL**（session38390→a44356，13:23:13–13:24:51 UTC）。原 top 6.36s，在 `skill_installation_test.go:291` 的目标 Project/Skills 初始化报固定 `INVALID_ARGUMENT`；两个 sub 均未开始，不能据此接受安装/读取/重放/清理，也不凭该安全错误猜测具体根因。原 Go773290、driver771390、supervisor771388、outer771342 的实际 Wait 均1；supervisor 96.272s。七个精确资源14次 absent，private/runtime各两次 clean，原后代及 adopted 空，supervisor 与 outer HOST_TCP 各双空，STOP0。运行闭包1219项、SHA256 `cb86d5af68ebd79c6623302fbb51b0c3c20442f75f478de8c97056af0b196ee8`，初末一致并完整包含原538个同字节编译输入。

原启动计划/结果在 `output/ai/skill-install/install-01-inputs.json` 与 `install-01-control/result.json`；原日志为 `/tmp/ski01/pg-19926d70da544933ae74869adb4bfcd5.log`。该轮已释放完整实际窗口；未修改生产、fixture、预算或成功门，未自动重试。后继先对上述唯一前置失败做有界分类。

全尾后作者对原 fixture 的有限源码定位：目标 Project 名称 `Skill install fixture` 含空格；正式 `CreateDigest → CreateProjectRequest.Validate → NormalizeName` 仅接受 ASCII 字母、数字及 `._-`，因此在接受事务/Skills 初始化之前即拒绝。原作者仅把该名称改为合法 `skill-install-fixture`；随后只读核对还确认 foreign Owner 按正式 Project 隐藏语义应得到 `NotFound`，将原 `Forbidden` 期望对齐。两处 fixture 修复保存为 `f6ebeb84`，产品与判定强度不变；原 native01 FAIL 与未执行子用例不回填。

修后 `compile-03` 实际 wholePASS（session76795→819b72，13:42:23–13:42:31 UTC）：原 compile794229/list794346 Wait0（6.299s/1.068s），精确1top；538个编译输入及方法初末相同，原进程组/runtime双空、后代/adopted空。新候选 `output/ai/skill-install/install-race-03.test` 为47,749,377B，SHA256 `d19566e8d1bd4cafe35fb74eec7e23f682afc646207010952109d64fecea0a41`；旧候选与失败记录均保留。

第二次真实 `native02` 整体 **FAIL**（session56196→ce5b31，13:43:06–13:44:43 UTC，source `f6ebeb84`）。fixture 已返回，随后 `skill_installation_test.go:295` 的 `sc.NewTextFiles` 返回固定 `INVALID_ARGUMENT`；top 6.98s，两个sub仍未开始，未到 `BuildPackage` 或 `Install`。这只定位原失败调用，不凭安全错误猜测具体材料谓词。原 Go796624、driver794821、supervisor794799、outer794754 Wait均1；supervisor95.538s。七个精确资源14次absent、private/runtime各双clean、原后代及adopted空，supervisor与outer HOST_TCP各双空，STOP0；运行1219输入初末一致，SHA256 `9a0c7fa68149f443f5072844692ae62191b4a076f81344cdcff9dd95bc4d9ddf`，完整包含原538个同字节编译输入。

本轮计划/结果位于 `output/ai/skill-install/install-02-inputs.json`、`install-02-control/result.json`，原日志 `/tmp/ski02/pg-fa64332eb25b4e2c98d807c568044951.log`。全部实际尾已释放，未自动重试；材料构造契约由原 fixture 作者有限诊断，仍不接受安装/读取/重放/清理或完整 Agent/F1。
