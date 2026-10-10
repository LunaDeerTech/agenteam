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
