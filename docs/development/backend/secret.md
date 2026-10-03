# Secret 存储与主密钥维护

D04 B02 的 `internal/central/secret/` 实现 envelope 加密、稳定凭据引用、lease、同事务 Audit、主密钥登记及可恢复重保护。`contract/` 拥有跨模块类型与端口。生产入口真实初始化加密存储和维护 worker；Session、系统/Project 授权、实际执行与 binding 适配仍未绑定，业务调用明确拒绝，没有匿名 Secret HTTP 或普通明文读取 DTO。后续责任见 [D01 Model/MCP 契约](../work-items/d01-contracts/model-tool.md) 和 [D04 实施规格](../work-items/d04-security-design.md)。

## 部署配置与启动

`AGENTEAM_CENTRAL_SECRET_KEYRING` 从 B02 起必填，仅从部署环境读取：

```json
{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"<部署生成的独立随机32-byte密钥的标准base64>"}]}
```

占位文本故意无效，不提供可用于部署的默认 key。最多 32 把 key，JSON ≤16 KiB；版本是规范正 int64 十进制字符串，当前版本必须存在。拒绝未知/重复字段、重复版本/材料、非规范 base64，以及与任一 cursor key 相同的材料。进程内配置不可变，修改环境后重启。key、fingerprint、canary、nonce、密文和 AAD 不进入日志或诊断。

`--check-config` 校验语法、大小、材料独立性与显式 CA，不连接 DB，也不声称 canary 已通过。正常启动先完成数据库连接/迁移/读写检查，再在独立的 30s 安全预算中验证 Audit、登记 key、真正解开 canary、验证每个必需版本至少一个真实 DEK并建立 write epoch fence。维护 worker 启动后才 bind HTTP。完整产品仍非 ready。

数据库全局迁移 `00003_secret.sql` 创建 `agenteam_secret` schema。`secret_master_registry` 永久保留版本、历史唯一 fingerprint、nonce high-water、canary 和退休标记；同一材料换新版本号也拒绝。丢失必需旧 key、同版本错材料或坏 canary 阻止启动。

## 加密与独立 nonce 预留

单值为 1–65536 个原始 byte，不 trim、不要求 UTF-8。每次创建或覆盖生成独立随机 32-byte DEK、payload UUIDv7 和 96-bit data nonce。标准库 AES-256-GCM 加密业务值；master key 只包装该 DEK。AAD 固定绑定格式、scope/Project、owner kind/ID、payload ID，wrap AAD 额外绑定 master version、data nonce 和 ciphertext digest。未知算法、格式或篡改失败，不尝试明文或旧算法降级。

所有 master 加密共用 `ATK1 || uint64 big-endian counter` nonce 域，包括 canary、业务 DEK、receipt digest DEK 和 rewrap。counter 为 1–`2^32−1`，每次独立短事务预留 1024 个，最后一个范围可不足 1024；仅确认 commit 后发布本进程可用范围。unknown 整段丢弃，回滚/失败消耗的 nonce 和退出时剩余范围均不再使用；上限拒绝加密，要求部署新 key。

**数据库备份回滚也会回退 high-water。D28 恢复必须先部署全新 master version/material，再允许写入，不能沿回滚计数继续用旧 key。** 普通进程重启保持数据库计数单调，与恢复旧备份不同。

## 写入、幂等与组合事务

`PrepareWrite` 在最外层业务事务之前预留 nonce、密封值和另一个加密语义 digest；返回的 `PreparedWrite` 只保留 sealed envelope/稳定身份，不持明文，通用 JSON/fmt/slog 不输出内容。`ApplyPreparedWriteInTx` 消费同一 Store 的 callback-local Tx，不打开新事务、不补 nonce、不借第二条连接。未准备返回 `SECRET_PREPARATION_REQUIRED`。

`WriteRequest.Kind` 为 create/update/delete，`ExecuteWrite` 是准备与事务执行的便利入口。命令 namespace 固定 `secret`，command 等于 kind；System owners 为 `[UserID]`，Project 为 `[ProjectID, UserID]`。create 不传 ref/expected_version，update/delete 使用稳定 ref 和正 expected_version。语义 digest 绑定稳定 User、scope、操作、期望版本、purpose 和原始值，排除 Session/request trace；create 的候选随机 ID 不影响同义重放。

应用持 command、`secret-write-key` shared、Project shared 与 credential aggregate exclusive 锁，重新检查当前权限和 write epoch；旧进程/迟到候选不能提交旧版本 payload。跨模块调用方须按 D01 完整锁序预收集低序锁；不得在已持高序锁后隐式补锁。成功结果、加密 receipt digest、值变更及 `secret.create/update/delete` Audit 同提交。重放先检查当前授权，再解密旧 digest 做常量时间比较，同义返第一次安全 metadata，异义为 `IDEMPOTENCY_KEY_REUSED`，不被旧 expected_version 误拒绝。

`LookupWrite` 只在当前授权下查原命令的最小安全结果；`Observed=false` 不证明之前的事务已结束或未提交。unknown 不返回成功值，也不自动宣布回滚。Secret 的原始值或裸低熵语义 digest 不存 receipt 列。

## 引用、lease 与请求材料

`RetainReferenceInTx` / `ReleaseReferenceInTx` 由实际配置 owner 的 `UsageAuthority` 检查 consumer、owner、scope 和持久 binding。引用保存与所属配置变化必须同 Tx。未知端口不以“没有引用”放行删除。value update 保持 CredentialRef，推进 Secret version；删除或改变 purpose 前检查 live references 和 active leases，否则 `RESOURCE_BUSY`。删除配置不自动删除共享 Secret。

D01 三个 lease 端口：

- `AcquireCredentialLeaseInTx` 在 ref 锁下按 `(ref, owner_kind, owner_id)` 去重；正式 UsageAuthority 验证受信任服务角色、实际 binding/执行 owner、用途和当前 gate。
- `ReleaseCredentialLeaseInTx` 按原 owner 幂等。已释放记录暂留 owner tombstone，使存在的凭据支持重复释放；合法凭据/Project 删除清除 tombstone，此后未知 lease 拒绝。没有 TTL 或断连自动回收。
- `ReadCredentialForRequest` 在短 Tx 重新验证 lease/gate/用途，读取 stable ref 的**当前值**，解密并追加真正的 `secret.resolve` Audit。仅 commit 已确认后给后端 `SecretMaterial`；Audit 失败或 unknown 均不发出材料。每次解析有独立 resolution ID。

调用角色为已注册 SecretService，cause 绑定 lease owner；UsageAuthority 返回经验证的实际审计主体及有限关联 ID。注册角色本身不是授权。Model lease 不固定 value revision：下一次解析读更新后的值，已发出材料不热换。MCP retained binding 的 revalidate 由 D20 正式适配独立决定，不能套用 Model 规则。SecretMaterial 的 `Use` 只借本次副本，callback 返回或 panic 后清零；`Destroy` 清零持有副本，禁止放 Snapshot/Transcript/DTO/日志。Go/下游自行复制的字节不承诺完全擦除。

## 可恢复重保护与 Project 清理

部署提高 current_version 时保留所有必需旧 key，重启在 exclusive write fence 下推进持久 epoch。每批最多 100 个非目标 payload（包括 receipt digest），事务外解 DEK、生成新 wrap，短 Tx 按旧 master_version/wrap_revision CAS，并同提交 checkpoint 和实际成功计数。值被覆盖/删除则丢弃候选；重复批次不重复计数。只改变 wrap，不重加密业务 ciphertext、不推进 Secret version。

`secret_rotation_runs` 保存 running/failed/completed、checkpoint、processed、固定 safe_error 和失败序号；独立失败 Audit 使用不重复 ordinal。进程重启仅依赖 DB；到尾后重新从头扫。最后 exclusive fence 的全表反查确认没有旧 payload/ref、没有能提交旧 epoch 的 writer，才原子标完成并标旧 key retireable，之后才可在下一次重启移除旧 key。不得凭内存游标或行数宣布退休。

损坏记录不跳过；失败保持原密文/进度，Secret unavailable、拒新写、ready 503。已初始化进程只对无关且实际 AEAD 验证成功的当前值，在原权限/gate/lease 和成功 Audit 下允许有限读；这不放宽重启的缺 key/坏 canary 门槛。维护 failure 无法记录或 commit 仍 unknown 时不伪报成功。

`CleanupProject` 接受 ProjectLifecycle actor、匹配 operation/version 的 deleting cause 和 checkpoint，由正式 ProjectAuthority 在独占 Project gate 内核验**持久 deleting + stopped**。live references/leases 返回 pending；每批清 100 个凭据和 100 个 receipt 及其 payload，最后完整检查剩余数据。System registry/数据与维护 Audit 保留；归档不清理；迟到写入仍经 gate 拒绝，不复活数据。

## 进程关闭与验证

第一停止信号停止新维护 batch；当前 batch 和已有 HTTP 使用独立 serving context，在原 shutdown deadline 内完成。二者结束后才停止 DB admission/drain。超时或第二信号的 owned SQL/socket 取消、HTTP 关闭和维护 worker join 共用额外 1s 总预算。只留下已提交 checkpoint，不从强关推断副作用不存在。

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh -run '^TestSecret'
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh -run '^(TestRealSecret|TestCentralSecret)'
```

普通 suite 无 Docker；专用脚本只使用经 nonce/label/exact-ID 核实的临时 PG fixture。`tests/security/secret_*` 覆盖实际并发 nonce、真实 COMMIT 回包丢失、SIGKILL 在准备/未提交/已提交 checkpoint 边界、旧 key 移除、AEAD/receipt/lease/Audit/Project gate；入口 integration 覆盖真实命令配置、启动 canary、诊断、HTTP+SQL worker drain 和 force。测试同步端口/路由仅在测试可执行文件内，未来身份/Model/MCP/Runner 产品集成仍由对应模块负责。
