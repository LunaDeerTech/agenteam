# 当前工作：D15 Runner 身份与 Control Channel

- 树 `/workspace/agenteam-runner-control` / `ai/runner-control`，基线正式 `f1c94ee5`；root掌管Git与真实资源。
- [规格卡](../docs/development/work-items/d15-runner-control.md) rev2已由未参与设计者有限复审接受；原rev1两处payload闭集/错误码缺陷已纠正。设计/实施者 `/root/service_delivery/blocker_spec_review`，不能独立验收本实现。
- 当前稳定片段：`internal/runnerprotocol/{scalar.go,types.go,json.go,message.go,payload.go,payload_methods.go,protocol_test.go}`，13消息/strict JSON/原子decode/关联/安全默认输出/深拷贝，已可构建；无Central import。
- 作者离线普通test28107、race60285均actual exit0；fuzz13404 actual0（5s配置，约38313执行）。首次无测试编译调用因本人工具包装未保留session终态，不作PASS证据；后续有完整actual终态。以上只wire局部自测，不代表auth/transport/root/真实平台。
- 固定gorilla/websocket1.5.3已下载至独占任务cache，网络窗已释放，go.mod/go.sum尚未改。00026须等真实24/25连续输入；根已授卡§2共享域。
- 新稳定片段：`internal/runner/identity/{identity.go,file_decode.go,file_unix.go,file_unsupported.go,identity_test.go,file_unix_test.go}`，pending/active Ed25519身份、安全默认输出、离线只读检查、FD相对no-follow目录验证、稳定flock与fsync/rename原子持久化。Linux真实文件普通场景/损坏/并发和受控I/O失败已由作者race75518实际exit0；首次普通77258因测试0644创建受umask收紧导致权限反例失败，现测试显式chmod纠正，原失败保留。未运行跨UID、真实进程Crash、macOS/其它CPU、登记或网络场景，不冒完整credential验收。
- 本范围无PG/server/browser/TCP或后台命令。下一阶段共享精确认证签名与设备认证/control；真实资源须fresh grant。Linux/macOS及D10/D16/D17/D18集成gate保持未验。
