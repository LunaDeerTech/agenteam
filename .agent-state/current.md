# Skill 受控包安装：当前检查点

- 工作树 `/workspace/agenteam-skill-install`，分支 `ai/skill-install`，基线 `728cd45a`。源码唯一作者 cleanup；Git 与资源调度归 root。
- 源检查点：首包请求/命令表 `ff820f7b`，私有仓储/计划事务 `cff8a11d`，canonical 来源与 work FK 的 00036 增量 `b34a87e4`，Object authority/maintenance 与原生命周期接缝 `748ac4a7`。
- 00036 只归本模块；必须在真实连续 00032/33/34/35 组合后验证。未执行迁移、PG、Object 或浏览器测试。

## 已实现的有限源码

输入只接真实 `BuildPackage` / `ParseCanonicalPackage` 产生的既有不可变 Package。普通新建固定 revision/version 1，保留受保护 Add Skills，同名默认冲突；没有更新、Runner source、自动分配或工具 schema。

安装 command 绑定原 User、Project、Skill、key、metadata 与 package/manifest digest。私有计划事务先当前 Owner Read，再对新写/未完成写要求 Mutate；未知提交返回零计划并保留原 physical AttemptID。canonical Skill 与 revision 通过完整安装来源 FK 关联，原 protected 初始化约束保留。

Object owner/access 与 maintenance 按实际安装行、当前 attempt、原 process 关联；初始化路径保留。安装 work 纳入原 Service 容量、Stop、Drain 与有界 recovery；取消不代表返回，实际本地返回或原 foreign process 停止证明加原完整锁后才可退役。该源码尚没有公开 Install 完整调用，普通目录/包读取与删除清理尚未接通。

## 首次纯基础检查

`pure-01` 在冻结 `748ac4a7` 输入上 whole PASS：

- 新 8 top / 9 sub 一次 race：Go 727521，实际 Wait 0，16.651 秒。
- 原 `TestInitializationWriterCommitBeforePhysicalAndAtomicPublication/success`：Go 727851，Wait 0，1.875 秒。
- 原 `TestSkillLifecycleStopKeepsActualDiscardAndReaderOwners/discard`：Go 727891，Wait 0，1.926 秒。
- 单包 `internal/central/skill` vet：Go 727959，Wait 0，8.829 秒。

原 outer 727520、session 50185 → 416489，最终 exit 0。每阶段同 process fresh ≥5 GiB、私有 telemetry off、只读离线 modules；原 Go Wait、进程组两次 absent、runtime 两次 empty、adopted [] 齐。1056 个 Go/mod 输入前后相同。原日志/result/输入清单位于 `output/ai/skill-install/pure-01/`，保留；没有在途进程。共享热 cache 已交回 root 调度。

本结果只证明当前源码纯基础及上述旧路径兼容，不代表 migration 00036、安装 publication、Project 完整删除或 Registry callable Backend 已通过。生产 install source 保持 unbound，旧 Object runtime join 等 STOP 不变。

## 下一步

先接受本片有限非作者 source review，再完成同原 caller 的 Prepare → Reserve → Upload → Publish、严格当前 canonical 读取及必要失败清理。只有具备正式工具契约和真实安装 Backend 后才能提供 Registry install source；不以空目录、DTO 或 stub 宣告 ready。下一 Go/真实资源运行需 root 明确窗口；不重复已通过且输入未变的矩阵。
