# 当前工作：D15 Runner 身份与 Control Channel

- 树 `/workspace/agenteam-runner-control` / `ai/runner-control`，基线正式 `f1c94ee5`；root掌管Git与真实资源。
- [规格卡](../docs/development/work-items/d15-runner-control.md) rev2已由未参与设计者有限复审接受；原rev1两处payload闭集/错误码缺陷已纠正。设计/实施者 `/root/service_delivery/blocker_spec_review`，不能独立验收本实现。
- 当前稳定片段：`internal/runnerprotocol/{scalar.go,types.go,json.go,message.go,payload.go,payload_methods.go,protocol_test.go}`，13消息/strict JSON/原子decode/关联/安全默认输出/深拷贝，已可构建；无Central import。
- 作者离线普通test28107、race60285均actual exit0；fuzz13404 actual0（5s配置，约38313执行）。首次无测试编译调用因本人工具包装未保留session终态，不作PASS证据；后续有完整actual终态。以上只wire局部自测，不代表auth/transport/root/真实平台。
- 固定gorilla/websocket1.5.3已下载至独占任务cache，网络窗已释放，go.mod/go.sum尚未改。00026须等真实24/25连续输入；根已授卡§2共享域。
- 本范围无PG/server/browser/TCP或后台命令。下一阶段本地Ed25519身份/原子文件/排他锁，随后设备认证与control；真实资源须fresh grant。Linux/macOS及D10/D16/D17/D18集成gate保持未验。
