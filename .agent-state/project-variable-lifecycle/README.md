# Project Variables 本实例精确停止：作者 PG 入口

本目录复用 [原 PG supervisor](../task-planning-recovery/pg_only_supervisor.py) 与 [PG driver](../task-planning-recovery/pg_only_driver.go)，在私有 namespace 绑定唯一 `^TestProjectVariableLocalLifecycleStop$`：1 top / 3 sub。没有修改共享源或预算，没有 native HTTP listener、MinIO 或浏览器。

- `archive-calls-and-project-isolation`：原 BEGIN 后、Project 锁前持有普通/Secret 写、合法读和另一 Project 写；真实 stopping phase fixture 提交后只取消目标写，原调用返回前不 joined。错误 acceptedVersion 与原回调导致的真实回滚均零取消。
- `delete-read-calls`：两个 Service 的 Get/List/Lookup 共六个原调用，取消之后仍等待真实 callback/Commit/call 退出。
- `confirmed-phase-and-rejection`：真实 Project EX 持未提交 phase，原 Stop 的 SH 等待以 exact PID/key/blocker 的 PostgreSQL 事实证明；COMMIT 前零取消，确认后取消并观察原调用退出。

fixture 复用正式 Account/Project 创建，原 Skills 初始化 receipt 明确是受控上游；stopping operation/manifest/participants 是规范阶段夹具，不冒生产 Archive/Delete worker、完整 participant、foreign join 或 cleanup。生产 initializer unbound 与 Object Runtime join STOP 不变。Unknown 的零取消仅由作者纯控制覆盖，本轮不冒真实 COMMIT 代理恢复矩阵。

## 固定调用与输入

先在本树以固定 Go1.27.1、private actual telemetry off、共享 module cache readonly 和自有 GOCACHE 离线编译两个产物；同进程 fresh >=5GiB。候选 `output/ai/project-variable-lifecycle/candidate-01/project-variable-lifecycle.test` 与 `pg-only-driver`。精确 list 必须只返回上述 top。

```sh
python3 -B .agent-state/project-variable-lifecycle/run.py \
  --driver "$PWD/output/ai/project-variable-lifecycle/candidate-01/pg-only-driver" \
  --binary "$PWD/output/ai/project-variable-lifecycle/candidate-01/project-variable-lifecycle.test" \
  --run '^TestProjectVariableLocalLifecycleStop$' \
  --output "$PWD/output/ai/project-variable-lifecycle/pg-author-01"
```

真实运行必须 root 独占资源 fresh grant；PG17/vector0.8.1 原 container/network 两资源、Go6m、driver105+cleanup15、sup123+TERM/KILL3、原资源/desc/runtime 双尾、host TCP75s 连续双空、原 Wait 与输入初末重新枚举全部保留。启动前私有 actual telemetry off、空 Docker config 和 >=5GiB；无自动重试。

生产依赖、完整同包 Go fixture、DDL/SQL、弱密码 embed、原 supervisor/driver 源及两个固定 binary 和本域三恢复源一并由 run.py 枚举；正规文件/无 symlink，缺项或新增输入、改变 bytes 均拒。只执行固定 selector，root/native/别名/重复 flag/未知 flag 在加载原 supervisor 前拒绝。

## 当前证据范围

首纯测试编译因测试直接比较非 comparable LockKey 而 FAIL；窄修为 Mode+CompareLockKeys 后 9 top / 20 sub race actual0。candidate01 race-c / exact list 与原 PG driver race build actual0，原业务尚未运行。首次实际 input 枚举因复用时误改 commitproxy 目录字面值而 FAIL；已恢复 main 的真实原路径，未修改原 helper 或扩大依赖。`entry-controls.py` 使用显式 OS 边界替身调用真实 supervisor.main/observer 与输入双检查；其结果仅为离线方法控制，不能当作 PG 或独立验收。
