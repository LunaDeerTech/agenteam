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
- 根已精确导入前序`00024_project_variables.sql`（334f86d0修后）与`00025_knowledge.sql`（da16d95a）；两域先main责任仍归原owner，Runner只消费依赖。当前树1..26连续组合SQL尚未运行；下一阶段准备最小迁移实际top并继续设备control。
- 本范围无PG/server/browser/TCP或后台命令；真实资源须fresh grant。Linux/macOS及D10/D16/D17/D18集成gate保持未验。
