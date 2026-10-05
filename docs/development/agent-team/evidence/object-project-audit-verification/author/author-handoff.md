# Object Project Audit 作者交接

状态：实现与作者自测完成，13 个授权路径已停止写入；尚待独立动态验收。未写 Git、contract、Store、SQL migration、Project/App、旧测试或 Artifact/D09 活动源。没有占用 00017。

## 冻结输入

- 固定生产：`6658a6cb1f29299521773bc8dc86b2f607b8c809`；采纳卡：`fcae3558f00131a0bcfbd40723b36525030bee63`。
- 最终只读副本：`final-source/`，其中 `input.json` SHA256 `061a4c19791e812e68a0755059a171984bc3bf8070197ae6f034f7943f119558`。
- 路径/hash 清单：`final-source.sha256`，SHA256 `f865000d48eb625b8b727e4759f892194b301e144420c07a97daada5fbef7a8d`。
- 8 生产源精确等于已静审 `production-review-02`；其 manifest SHA256 `94647366e02f3bd544ed5a50ecc681061bf85a73b5d61a03c7ef6291124e0aeb`。review01、review02 原件及 delta 均保留，不覆盖。
- `evidence/final-scope-check.json`：最终 workspace/snapshot/冻结副本 13 源逐字节相同；其余 782 个归档文件与固定提交逐字节相同，包括全部旧测试/contract/migration、go.mod/go.sum。六旧源只改卡内 audit 接缝；S2 完整 writer 引用/Discard 排除/真实 join 保持。

## 完整行为

独立 `NewProjectAuditAuthority(Store)` 零 SQL 构造；nil、typed nil、零值与动态不可比较 Store 均拒绝。最终 witness 绑定完整 Entry、Key、九个 associations、ActorDetails、同 Store、同活 Tx，并消费原完整 Acquire/Validate 路径的私有阶段证据。checker 只读 Object 本域 canonical 行，不再 Acquire、不嵌套 Tx、不访问 Backend 或其它业务库。

已接 upload publication、writer failure、recovery failure、delete、transfer issue/complete/revoke 七阶段六 actions。publication/delete 仍在原终态 UPDATE 前 Append；Project Cleanup 的 transfer 撤销仅在原 Tx/原 predicate 内改为真实 UPDATE 后 Append。PUT 内层 publication 和外层 transfer proof 独立保留，两条 Audit/全部 publication UPDATE 同 Tx 回滚。

GET 的迟到完成保持既有 Converge：真退休后 released lease、cleanup gate 下 active lease、逻辑 deleted 后历史 metadata 三种状态均可在当前真实授权与 Completed proof 下完成；released 分支仍核精确 lease identity、released_at、retirement ID/kind/digest。Issue 与 PUT 不放宽可读/verified/publication条件。

生产 Project ObjectProducer 分派/当前 gate 映射/root 仍未绑定。本次严格测试 Project/Owner/Runner/Stop ports 隔离该后继边界，所有成功均真实调用 Object checker + Audit + PostgreSQL/MinIO；不提供生产 allow。

## 验证与逐项复用

命令、退出码：`commands.json` / `results.jsonl`；复现环境：`environment.json` / `run.py`。使用 Go 1.27.1、离线固定 modcache、独立 gocache/TMPDIR、固定 MinIO binary、唯一 fixture 窗口。真实脚本保持 `-race -count=1 -timeout=6m`；只增加 `-v` 留存子例与语义日志，没有放宽预算。

- `final-unit-race.log`：Object/Audit 单元 43 顶层、231 子例，全过；新增 checker 四顶层、30 子例包含 constructor/精确 envelope/九关联/原实际私有 Acquire。
- `final-vet.log`：Object/Audit 与 integration objects vet 通过。
- `final-integration-compile.log`：最终 13 源上的 integration objects 编译通过（`go test -tags=integration -run ^$ ./tests/objects`，没有新启 fixture）。
- `final-two-cmd-build.log`：最终快照上 `go build -o <自有 bin>/ ./cmd/agenteam ./cmd/agenteam-runner` 实际 exit 0，两个可执行文件字节/hash 见 `build-artifacts.json`；没有启动二进制。
- 新增真实共 **10 顶层、19 子例**，逐项来源与实际子例见 `coverage.json`。
- 受影响旧回归 **17 顶层、17 子例**，全部在 `final-real-1.log` 通过，包括原完整 union/foreign token、当前 actor/父资源、SourceLease actual join、Project Cleanup remote lease、旧转移与 archive Converge、S2 read/stop/真实 reader join、Downloads 原业务审计及两个真实 Account Avatar/System 组合。

| 新增真实能力 | 通过日志 |
| --- | --- |
| 外部组合 Tx publication 前态、完整 witness 篡改与 owner/attempt/digest/phase canonical 回滚 | integration-new-2.log |
| writer ordinal 0、真实 backend 缺失 recovery ordinal 1、delete 前态/原 cleanup cause 拒绝与恢复 | integration-new-2.log |
| publication 实际提交丢 ACK、同 receipt 重放零重复 PUT | integration-new-2.log |
| PUT 两条 Audit 同 Tx、拒外层全回滚、verified candidate 重放零重复 payload；GET 完成后合法 complete-phase Cancel | final-real-1.log |
| Project Cleanup 两条 revoke 先真实 UPDATE，第二条 Audit 拒绝整批回滚 | integration-new-2.log |
| archive/delete stop revoke 的 Audit 拒绝回滚、真实退休和最终 Inspect 不重复 Audit | integration-new-2.log |
| stop gate 原 Tx 同时含 stop/revoke/Audit 的 committed/rollback/pending Unknown；原 writer 终结后重验当前权限/退休/Stopped | integration-new-2.log |
| GET retired / cleaning / deleted 三种合法迟到完成 | integration-new-2.log |
| transfer issue 原 Audit Tx 三态 Unknown 不返回 material，当前 Session 撤销拒绝历史重放 | integration-new-2.log |
| 最终 PUT 双 Audit 与 transfer/upload/object 同 xmin 的实际已提交丢 ACK，同证据重放不重送字节 | final-real-1.log |

`integration-new-2` **整组仍为 exit 1，绝未改记通过**：仅它的未变成功项被复用。其唯一红点是 nested PUT 重试把合法 staging 零 marker GET 算为 payload；该项与新增最终 PUT Unknown 在 `final-real-1` 用最终测试源重新通过。`evidence/final-vs-new2.patch` 显示其它已复用测试函数/fixture 行为不变。`final-real-1` 本轮 19 顶层/19 子例全过（2 新 + 17 旧）。

最终 wire 证据：`final-real-1.log` 的原 stage GET `1→2`、candidate PUT `1→1`；实际 staging + candidate GET payload 字节 `46→46`，非空 PUT `2→2`。按实际响应流字节计 GET；PUT 使用 Content-Length/decoded 长度，未知长度直接测试失败。原观察值仍保留，不把未知 body 当零 marker。

Unknown selector 在原 callback 成功返回后才 arm：真实 Audit action/resource/current xmin；stop 另要求同 Tx revoke + stop 行，最终 PUT 另从独立连接核 transfer/upload/object 与两条 Audit 同 xmin。日志记录实际 PID/xid。协议代理命中实际 COMMIT；committed 情形收到服务器 CommandComplete(COMMIT)+ReadyForQuery idle 后丢 ACK；pending 核原 backend_xid + idle in transaction；等待原 writer 真正终结后才看持久结果。不存在第 N 次 Tx 计数或手造 CommitUnknown。

注意作者 stop 测试的 `joined=0` 只证明当时未记录 join，**不单独声称直接观察到零 cancel**。生产控制流检查已确认 commitError 失败提前返回，独立 V 将补直接取消观察。Stop 初次 ResourceBusy 的精确返回点未唯一定位；作者只将错误的同步收敛前置修为 ResourceBusy 必须伴随 Pending，并继续原 3 秒/40 步/5ms 的正式终局验证，不推断假死。

## 保留的失败及修复

- `unit1.log`：单测 fixture 的空依赖锁被正式 constructor 拒绝；改真实非空锁，unit2 通过。早期 integration compile 缺 wrapper/缺 strings import 的日志保留。
- `late-get-red-1.log`：新 fixture 在 NewTransferService 构造失败，未触达业务 red。Observer 移入正式 concrete transfer planner 的 base，并通过配对 Runner Validate 观察；不改变生产 constructor。
- `late-get-red-2.log`：三种实际合法 canonical tuple 都已形成，review01 checker 均在 CompleteTransfer 返回 FORBIDDEN。`evidence/late-get-red2-input.json` 与原 review01 生产保存。review02 只改 GET 历史完成事实门槛，修复后真实三态过。
- 独立兼容阻断报告：`/tmp/agenteam-object-audit-static-wt8i3hz4/review.md` SHA256 `1ba968251e080746ed5fe34064bd208a293b8831eb3d10368ca54f1b0fe58aee`；修复差量静审：`/tmp/agenteam-object-audit-delta-zr0jal2v/review.md` SHA256 `32e23dafba05ec7b10d00036bfa0e0599d86b69b84cf3e5ff7a37bba91568e8f`。
- `integration-new-1.log`：HTTP 代理错误 TLS 前置、原总 PUT/GET 计数混入 cleanup marker、Pending/并发收敛前置过强及只做一次 PUT retirement checkpoint。全部原 red/输入保留。`integration-new-1-input/` 两测试副本由后续 delta 反向恢复并逐字节核对原运行 SHA；不是猜测近似副本。`evidence/new2-test-delta.patch` 为精确修订。
- 首红/测试修正独立核查：`/tmp/agenteam-object-audit-new1-review-ep8w4wh6/review.md` SHA256 `5eebf74778da9808769294f4cbbbe238d79da802af94e3121885558115298ad3`。其 pending 的 payload 分类证据已由上述最终真实字节计数补足。

## 资源与停止

窗口已交回 root。`resource-zero-1.json`、`resource-zero-2.json` 保存两次核查；最后一轮所有 4 个 container / 3 个 network 精确 ID 均 absent；全部外部已采集自有 ID absent。原 2 容器/4 网络 ID、name、labels 逐字段不变；自有运行进程 0，runtime 目录空。各轮固定脚本自身均记录 exact cleanup 完成。第三次定向短轮外部事件采集只有部分 ID，未将其误报为完整 4/3 清单；窗口末全量资源集合精确恢复基线，同时各轮 nonce 对应的资源均不在当前集合。

窗口交回后仅按 root 续授权补上述纯 integration compile 与双 cmd build，均通过、命令已退出；没有再调用 Docker。现在不再源码、Go、Docker 或 Git 操作。此交付不替代独立动态验收，也不宣称生产 Project binding 或完整 D05/D08 完成。
