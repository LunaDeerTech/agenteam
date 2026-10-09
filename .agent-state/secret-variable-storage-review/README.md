# D04 kind3 / Plan / Prepared / Read / Prepare 独立审查

Skills 未参与本片段设计或实现，只读 `/workspace/agenteam-secret-variable-storage`。**冻结输入 `372d1e94` 的有限阶段接受，无本范围 mustfix。** 不包含后继 Apply/native Audit、purpose 新实现、rotation/Cleanup；这些活动源通过 overlay 排除或恢复到该冻结输入。没有修改作者产品，没有 PG、socket、browser、网络或新大缓存。

范围是 `internal/central/secret/` 的 `envelope.go`、`storage.go`、`project_variable_envelope_test.go`，`contract/project_variable_plan{,_test}.go`，`service.go` 和 `project_variable_{prepared,read,prepare}{,_test}.go`，共12技术源。旧 `contract/project_variable{,_test}.go` 的 `92aca721`／本人 `35587/21c8a0` 有限接受复用，不重复完整 Intent 测试。依据 D04 工作项 rev2 §2–6 与 CheckPlan 已接受增量。

## 源码与现有证据

- kind3 限 Project、32-byte 明文/48-byte 密文；原 data/wrap AAD 继续绑定格式、scope/Project、kind、receipt owner、payload，kind1/2 编码没有改字节。数据库 kind 在 int16→byte 前闭集检查，不能借溢出成为合法 owner。作者 crypto 6 top `42255/f25143` 与独立 Python AESGCM literal vector `68539/b24eff` 输入未变，结果复用。
- opaque Plan 绑定私有 issuer、完整 Request/当前 Session，最低 command/User/Project EX、write-key SH、CredentialRef EX；locks 与 expected 均保持复制语义。历史 basis 不要求仍活 canonical，区分外部 Variable expected 与内部 Credential version。CheckPlan 先于 nonce/SQL，不重 Discover/create candidate；进入 Tx 后仍先同 Store InTx、完整 RequireHeldLocks、当前 ReceiptRead，再读专用 receipt。不能用结构 Matches 代替真实当前权限。作者 plan 两 contract 包 `97245/afbe81` 复用。
- native prepared 精确 private concrete/typed-nil/同 Service issuer 检查先于任意外部接口方法；所有 alias 共用 mutex 和 Destroy 状态，销毁清除自有 sealed buffers，保留 caller Intent。Preparation/locks 只是安全投影，不能自签私有能力。作者 prepared `8949/28c88f` 及 live canary `46230/60d6fb` 复用。
- Lookup 只读取精确历史 receipt 与 kind3 owner 元数据，不解密；observed 丢失安全失败，异 Ref 返回离锁重新准备。Match 才打开两份摘要、常量时间比较，不读当前 canonical 或业务 value。Prepare 真实 seal kind1/kind3，历史重放与 metadata-only 不封新业务值，错误路径 defer Destroy 与 digest/nonce 清理。作者 Lookup `8082/cdbf1b`、Prepare `10260/734ad6` 及互斥守卫强化 `13922/fa22d0` 复用。
- 冻结阶段未扩大旧 Purpose.Valid，旧 write 与 loadMetadata 的用途拒绝仍在。专用 purpose/旧入口隔离的后继实现不在本结论，不能把本次 crypto/plan 接受称为整个新用途生产写入已闭合。

## 本人有意义补控

`7872/530ae3` actual exit0、race **1.048s**，3 top/10 sub：

1. 真 Prepare 生成 kind3 密文，再作为受控历史行交真实 scan/open/Match 消费。相同原语义换 Session，旧 plan 必须在 SQL/nonce 前拒绝；新绑定 plan 可匹配原 receipt/ref/version。不同 description 为 KeyReused；observed 丢失、密文破坏、kind2 重标、receipt/Project/payload 替换均不发布 observation。当前 ReceiptRead 每次实际调用，任何读取当前 canonical/业务 value 的查询都会失败。
2. 真实 Match 已进入当前 authority、持有 native prepared mutex 时，从 alias 调 Destroy：原 Match 未返前不能退役或清缓冲。释放 barrier 后，原 Unknown/cause 或 cancellation 保留，未读取受保护历史；Destroy 返回后借出的所有 sealed buffers 为零，projection 不可再用，caller Intent 仍有效。
3. 公共 Match 拒 nil、typed-nil、自造 interface、包装真实 handle、另一真实 Service 的 Prepare 结果；不得调用其方法或到达 authority/SQL，不销毁原合法 prepared。

随后仅新 top `10894/a02a8b` actual exit0、race **1.016s**：先真实完成 kind3 seal，再让业务值的第二次 nonce 请求得到原 allocator 的 Unknown；必须返回 nil candidate、原 NonceReservationUnknown/Unknown 分类，已消费 nonce 不倒退，未知范围不接受/重试，caller 原材料保留。没有声称 nonce reservation 在真实数据库执行，错误后局部候选清除还结合真实 defer Destroy 源码核验。

四 top 是两次有限执行的合计；第一组三 top 源未改，不因新增单 top 重跑旧组合。受控 Store/authority/nonce-range 只支持调用顺序、拒绝和密码材料生命周期结论，不是 D10 真实授权、PG receipt 或事务提交证据。

## 复现与待闭合范围

```sh
# cwd: /workspace/agenteam-skills
python3 .agent-state/secret-variable-storage-review/run.py
# 仅最后新增控制：
STORAGE_REVIEW_RUN='^TestIndependentStoragePrepareSecondNonceUnknown$' \
 python3 .agent-state/secret-variable-storage-review/run.py
```

脚本核14个冻结源（含仅作输入的两旧 Intent 源）逐字 `372d1e94`，只运行自己的 `stages_test.go` overlay；新增活动 Go 文件变为空 package，已经修改的旧共享源取冻结原 blob。首次排除6个新 Apply 文件及原 project_audit；第二次另排新 maintenance 与还原原 rotation/cleanup。不消费作者活动实现。

固定 Go `/workspace/toolchains/go1.27.1/bin/go` 前置继承 PATH；`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOFLAGS=-mod=readonly`。只读 `GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，Skills 独占 `GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`，`GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp`。Go 使用 `-race -count=1 -p=1 -timeout=30s`，控制完成后实际命令均已退出。

后继门槛：真实 D10 authority 的 CheckPlan 私有 Store/binding 与 ReceiptRead/NewWrite 当前权限、原完整锁和真实 SQL；并发 create 重发现、历史 deleted Credential/receipt 重放与 KeyReused；Apply/D10 final-Tx/Audit/Outbox/Activity 原子性；正式连续迁移的约束/升级/回滚；kind3 rotation/canary/Retire、100/101 Cleanup。当前不含这些生产闭包，也不解除既有 Object Runtime 或其他停止项。
