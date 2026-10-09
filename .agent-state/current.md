# 当前工作：D15 Runner 身份与 Control Channel

- 树 `/workspace/agenteam-runner-control` / `ai/runner-control`，基线正式 `f1c94ee5`；root掌管Git与真实资源。
- [规格卡](../docs/development/work-items/d15-runner-control.md) rev2已由未参与设计者有限复审接受；原rev1两处payload闭集/错误码缺陷已纠正。设计/实施者 `/root/service_delivery/blocker_spec_review`，不能独立验收本实现。
- 当前稳定片段：`internal/runnerprotocol/{scalar.go,types.go,json.go,message.go,payload.go,payload_methods.go,protocol_test.go}`，13消息/strict JSON/原子decode/关联/安全默认输出/深拷贝，已可构建；无Central import。
- 作者离线普通test28107、race60285均actual exit0；fuzz13404 actual0（5s配置，约38313执行）。首次无测试编译调用因本人工具包装未保留session终态，不作PASS证据；后续有完整actual终态。以上只wire局部自测，不代表auth/transport/root/真实平台。
- 固定gorilla/websocket1.5.3已由离线缓存加入go.mod/go.sum，无新增下载。根已授卡§2共享域。
- 新稳定片段：`internal/runner/identity/{identity.go,file_decode.go,file_unix.go,file_unsupported.go,identity_test.go,file_unix_test.go}`，pending/active Ed25519身份、安全默认输出、离线只读检查、FD相对no-follow目录验证、稳定flock与fsync/rename原子持久化。Linux真实文件普通场景/损坏/并发和受控I/O失败已由作者race75518实际exit0；首次普通77258因测试0644创建受umask收紧导致权限反例失败，现测试显式chmod纠正，原失败保留。未运行跨UID、真实进程Crash、macOS/其它CPU、登记或网络场景，不冒完整credential验收。
- 认证签名片段 `internal/runnerprotocol/{auth.go,auth_test.go}` 与 `internal/runner/identity/{sign.go,sign_test.go}` 已闭合：严格Auth header/nonce/canonical Unix seconds、固定签名字节与Ed25519向量、pending同key恢复签名、安全默认输出；两包race59862实际exit0。向量用本地Python cryptography独立算法生成作作者oracle，非独立审查。当前未验证nonce数据库消费/时窗/真实TLS。
- 新control私有FIFO队列含32条/256KiB控制保留、已租借writer仍计容量、实际release前不清buffer；`outbox.go/outbox_test.go`作者race14933实际0，尚未接WebSocket consumer。
- `00026_runner_control.sql`六表/Audit增量及卡§3/8持久代际、metadata细化已获独立有限接受。首审发现同代际清key后重绑及既有enrolled_at可改写，仅增两条拒绝谓词；最终独审6个定向控制及7组Audit闭集核通过，原缺陷保留。作者12个布尔/元组控制为SQLite共同子集（首次漏business_changed绑定的setup错误已纠正）；均无PostgreSQL语法/迁移结果，本树前序待根精确同步。前序00024旧project guard冲突已由原作者修复并窄审接受。
- 私有gorilla wire adapter及双worker实际join已实现，control作者race71687实际exit0：取消后原writer/callback未返回时不能报joined，原buffer保留到writer实际返回。尚未接生产hello/session/root，无native socket运行证据。
- 根已精确导入前序`00024_project_variables.sql`（334f86d0修后）与`00025_knowledge.sql`（da16d95a）；两域先main责任仍归原owner，Runner只消费依赖。当前树1..26连续组合SQL已由下述最小top实际运行。
- 最小`tests/runnercontrol/migration_test.go`已freeze；race-c98020实际0、精确发现`TestRunnerControlMigration`恰1。首次独占依赖cache缺pgx/goose的setup FAIL保留，根授权只读共享cache后编译闭合。测试只证明DDL/约束，SQL造约束刺激不代当前Admin/typed producer。私有`output/ai/runner-control/pg-only-driver`沿原跟踪driver编译14378实际0；supervisor由根精确导入已接受ca9 bounded版本。实际跟踪driver参数为Go6m、driver总105s，outer123s与完整尾不变；旧口述90s不符此源码，不作本轮预算证据。
- 本人沿上述原工具实际运行唯一`TestRunnerControlMigration`，外层22736 actual0，三子body9.98s，Go628693/driver628035实际Wait0；精确2ID双退役、runtime双空/TCP双delta空、inputs_unchanged齐，sup80.401s。原件`output/ai/runner-control/pg/pg-d5b237f4f6154e6881d07348ed1ce069.log`；资源已完整释放。限定作者schema/DDL结果，不冒Admin/service/typed producer/完整D15。
- §4登记已知200五字段响应已获原审者有限差异接受，双端可按该固定shape实施；不代网络验证。
- control会话新片段`session.go/session_state.go/session_test.go`及原队列/wire的有界fatal诊断写出已闭合；最新作者race17034 actual0（14.111s）、vet实际0。含hello/heartbeat自然timer、4096/8192关联限额、原deadline、未知执行结果、mask/stream终态顺序、原runtime回调未返不得join；纯channel端点不是nativeWSS。生产操作仍未绑定，默认空注册明确unsupported。下一阶段接identity/challenge/严格TLS client及Central真实service，不把此私有端口当完整生产消费者。
- 设备HTTP四源`runnerprotocol/device.go/device_test.go`与`runner/control/device_client.go/device_client_test.go`已闭合：登记/挑战严格DTO、五字段已知回执、原Runner/公钥绑定、32KiB及完整EOF/单次Close、拒redirect/编码/原始transport错误；取消仍等原body返回。作者协议race18250、客户端race56643实际0；仅私有RoundTripper/heldBody和原生TLS配置检查，无真实TLS/hostname/WSS结果。下一步公开client持久身份、重连及root接线，共享13消息及control传输/会话仍待独立风险验证。
- identity/auth十源已获未参与实现者有限独审；发现`https://:443`空hostname误收的原overlay42223红，身份层仅改Hostname非空并补7正反。作者race94352实际0，原审者相同失败入口99383实际0；签名字节/nonce绑定、实际FD权限与hardlink控制有效，文件安全静审接受。原未验跨UID/crash/macOS/其它CPU、DB nonce与TLS/WSS边界仍保留，不称完整凭据验收。
- 本范围无PG/server/browser/TCP或后台命令；真实资源须fresh grant。Linux/macOS及D10/D16/D17/D18集成gate保持未验。
