# Skill 受控包安装：当前检查点

- 工作树 `/workspace/agenteam-skill-install`，分支 `ai/skill-install`，基线 `728cd45a`。公开安装/00036/共享生命周期来源作者 cleanup；普通读取/清理 helper 作者 secret，文件边界分离。Git 与资源调度归 root。
- 源检查点：首包请求/命令表 `ff820f7b`，私有仓储/计划事务 `cff8a11d`，canonical 来源与 work FK 的 00036 增量 `b34a87e4`，Object authority/maintenance 与原生命周期接缝 `748ac4a7`。
- 00036 只归本模块；必须在真实连续 00032/33/34/35 组合后验证。未执行迁移、PG、Object 或浏览器测试。

## 已实现的有限源码

输入只接真实 `BuildPackage` / `ParseCanonicalPackage` 产生的既有不可变 Package。普通新建固定 revision/version 1，保留受保护 Add Skills，同名默认冲突；没有更新、Runner source、自动分配或工具 schema。

安装 command 绑定原 User、Project、Skill、key、metadata 与 package/manifest digest。私有计划事务先当前 Owner Read，再对新写/未完成写要求 Mutate；未知提交返回零计划并保留原 physical AttemptID。canonical Skill 与 revision 通过完整安装来源 FK 关联，原 protected 初始化约束保留。

Object owner/access 与 maintenance 按实际安装行、当前 attempt、原 process 关联；初始化路径保留。安装 work 纳入原 Service 容量、Stop、Drain 与有界 recovery；取消不代表返回，实际本地返回或原 foreign process 停止证明加原完整锁后才可退役。本段对应已验 748 片段；后续公开调用与普通读取/清理组合见下，尚未运行。

## 首次纯基础检查

`pure-01` 在冻结 `748ac4a7` 输入上 whole PASS：

- 新 8 top / 9 sub 一次 race：Go 727521，实际 Wait 0，16.651 秒。
- 原 `TestInitializationWriterCommitBeforePhysicalAndAtomicPublication/success`：Go 727851，Wait 0，1.875 秒。
- 原 `TestSkillLifecycleStopKeepsActualDiscardAndReaderOwners/discard`：Go 727891，Wait 0，1.926 秒。
- 单包 `internal/central/skill` vet：Go 727959，Wait 0，8.829 秒。

原 outer 727520、session 50185 → 416489，最终 exit 0。每阶段同 process fresh ≥5 GiB、私有 telemetry off、只读离线 modules；原 Go Wait、进程组两次 absent、runtime 两次 empty、adopted [] 齐。1056 个 Go/mod 输入前后相同。原日志/result/输入清单位于 `output/ai/skill-install/pure-01/`，保留；没有在途进程。共享热 cache 已交回 root 调度。

本结果只证明当前源码纯基础及上述旧路径兼容，不代表 migration 00036、安装 publication、Project 完整删除或 Registry callable Backend 已通过。生产 install source 保持 unbound，旧 Object runtime join 等 STOP 不变。

## 公开安装与普通来源组合：源码冻结，未运行

公开 `Service.Install(ctx, actor, meta, project, request)` 已落同 caller Prepare → 已知 Reserve commit → Upload → 同 Tx canonical Skill/revision 与 Object Publish。当前仅 Human 当前 Owner；AgentRun 仍 unbound。原 key/input/User/Project/Skill 与包/manifest 摘要不变，published replay 与 `LookupInstall` 经当前 Owner Read/同 Tx canonical 验证返回原 receipt；reserved 只返回 ResourceBusy/lookup，不重发，不制造 CommitUnknown。真实 Unknown 仍保存原 CommitResult，返回零 receipt。

安装原 Discard 失败保留在 owned work，原调用实际返回之后才能执行同一尾；Drain/Recover/InspectStop 有界接续，取消及 caller 返回不能冒清理返回。已提交 publication 后 tail 失败返回 committed 故障且无成功 receipt，可通过原 key 查实际结果。新增 `install_service_test.go` 受控 Service/事务/Object 顺序、Unknown 零重发/lookup、held Discard 判据，尚未执行，不代替真实 Object/PG。

00036 普通 cleanup 分支指向完整 installation tuple，允许 reserved/failed 有 Object 而尚无 canonical Skill；初始化保原 canonical 与初始化 FK。追加真实源 FK 反查和历史有界访问索引。cleanup authority/maintenance/audit 只按真实旧映射缺失分派普通来源，原 lifecycle gate/同 Tx D05 checker 不替代。secret 的 `install_read.go`/`install_cleanup.go` 为实际 helper，借入 00032–35 与 Skill34 引用 guard 后仍须组合验证。

此批仅 gofmt 与 diff-check；未执行 Go、迁移36、PG/Object 或后台安装。原 pure-01 PASS 只绑定748，不能外推新源码。下一先保存当前源码并有限独审，再按 root 资源窗口运行新增必要纯检查和首次真实调用；不重复未变矩阵。完整 Agent/F1、Registry install source/Backend、Runner source、自动分配、更新与旧 STOP 均未声明完成。

## 公开链 pure-02：启动前磁盘门失败

公开安装/普通清理 `74342fdd` 已获 coordination 有限源码独审接受；真实新 fixture `tests/projectvariable/skill_installation_test.go` 已保存 `46a95480`，精确 `TestSkillInstallationPersistentObject` 两 sub 为 `install-read-replay`、`ordinary-cleanup`，尚未编译或执行。该 fixture 从真实 Project Create/Skill initializer 开始，后继清理阶段显式使用上游生命周期 fixture；不宣称完整 Project participant 调度已接通。

2026-10-10 13:09:46 UTC，原 `pure-02` outer 755841（exec `0f9c88`）在同进程 fresh 磁盘门失败：5,144,195,072 B < 5,368,709,120 B。outer 实际 exit 1，零 Go、零 cache 写、零资源；race 与 vet 均未执行。1069 个 Go/mod 输入前后相同，私有 runtime 为空；原 `output/ai/skill-install/pure-02/{result,inputs}.json` 保留，未自动重试，热 cache 已交回 root 调度。

待资源条件满足后仍只运行既定 8 新 top、原 bounded cleanup 1 top、Stop 的 discard/reader 两 sub 与单 skill 包 vet。准确合计 10 top / 27 sub；此前 20 sub 的准备计数漏记 `TestInstalledCleanupFinalAnchorsRequireCompletedOriginalGate` 内既有 7 sub，选择器范围未扩大。源仍冻结，下一 fresh 轮须由 root 调度。

## 公开链 pure-03：限定 race / vet whole PASS

root 对三棵停驻旧树的已保存 archive 工作副本可逆 sparse 后，2026-10-10 13:11:35 UTC 启动 fresh `pure-03`，首阶段同进程可用 6,210,076,672 B。运行时 HEAD `5569c668`，Skill 产品与既定测试未改；1069 个 Go/mod 输入每阶段及整轮前后相同。共享 Python 入口不属于本轮编译输入，可由 content 独立维护。

- 新 8 top / 25 sub：Go 757738，race Wait 0，15.525 秒。
- 原 bounded cleanup 1 top：Go 757970，race Wait 0，3.872 秒。
- 原 Stop `discard|reader` 1 top / 2 sub：Go 758097，race Wait 0，2.712 秒。
- 单 `internal/central/skill` 包 vet：Go 758184，Wait 0，2.936 秒。

exact 合计 10 top / 27 sub，无 FAIL/SKIP；outer 757734、session 41809 → `d109a6` 实际 exit 0。每阶段原 Go Wait、进程组双 absent、runtime 双 empty、adopted [] 齐。原日志、result 与输入位于 `output/ai/skill-install/pure-03/`；全部尾已释放，热 cache 已直接交 work_ui 后继窗口。pure-02 原预飞 FAIL 保留。

本轮只证明公开安装/读取/清理受控基础与受影响旧路径；迁移 00036、真实 Human Install → Get/Open → Cleanup 首链尚未执行，AgentRun / Registry callable Backend 与原 STOP 边界保持。
