# Runner write-file Operation 方法

本目录是已落齐的首链源稿；尚未运行 Go、PG、服务或网络。只验证显式组合，生产默认注册、后续 Transcript 消费及下一轮 Model 不在范围内。真实 Model JSONCaller 是唯一模型结果替身；Owner、Registry、Preparation、Execution、Tool、Mount、WSS/Runner/Manager 与 PostgreSQL 均走正式实现。

精确入口 `TestWriteFileOperation`，两个场景：

- `committed-attempt-write-and-lookup`：真实 Attempt COMMIT 后 hold 原返回，零 wire；原返回后同持久 RequestID 仅一次 WSS 写入，512 KiB 独立分块读取完整比对；最终真实 COMMIT 后丢回执，仅原 Lookup 恢复，不再 Execute。
- `revoke-before-claim-no-dispatch`：Attempt 原返回后，在 Claim 原事务开始及取锁前暂停；普通 Owner 正式撤销 Mount，原旧票零 wire，原文件不变，Attempt 以 `not_dispatched` 退休。

`profile-controls.py` 只检查新 profile 和旧 AST/有效映射保留，以及真实原 gate 的正反行为；不启动资源。`run-pg-checks.py compile --output <fresh>` 使用原完整输入枚举、离线 max1/-p1/race/integration-c 和精确 list；`prepare --compile <passed> --output <fresh> --namespace <fresh-absolute>` 只生成原 supervisor 配方。生成目录中的 `record-native.py` 仅在 root 授独占窗口后运行一次，亲收外层 actual Wait；原七资源、全部尾门和 75 秒 HOST_TCP delta 双空规则原样保留。失败原件不覆盖，不自动重试。

Execution/Loop 的显式 write 构造已接入。`run-basic-checks.py` 只选 30 个新 top 和 14 个本轮受影响旧 top（7 个测试包），另 vet 8 个实际包；沿原 `go list -deps -test -json` 实际源码/测试/embed 闭包及每阶段前后稳定记录，不运行 PG/native。基础与 PG 均须 root 另授窗口后执行，未将源码完整记为通过。root 已将全部已提交冻结的同一分支从临时源码树迁到 `/workspace/agenteam-runner-write-file-operation`，首次执行将在此合规工作树进行；原 Identity 逐祖先 `0022` 拒绝门、driver 自动 cwd 及环境覆盖保持。旧 read-file helper、已验 Runner write WSS/local 场景不改、不重复扩大矩阵。全部 Git 由 root 保存。
