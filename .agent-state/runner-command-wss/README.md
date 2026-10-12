# Runner run-command WSS

当前是源码阶段，未执行 Go、服务或真实 WSS。以已验本地 Command owner/Workspace 桥为依赖；对应[专属卡](../../docs/development/work-items/d15-runner-command-wss.md)与正式[Control 协议](../../docs/architecture/runner/control-protocol.md)、[Execution Runtime](../../docs/architecture/runner/execution-runtime.md)。

本片只能显式构造 env-free run-command Handler/Control executor。输入 argv/cwd 与真实退出事实不能用来授予 Central 权限；default New/cmd 仍未绑定。已知调用报告 success 与命令 exit 0 分开，取消后实际丢弃的输出必须标不完整。字节计数只代表原 outbox 接纳，取消 sentinel 不掩盖真实发送失败。

当前 writer 范围：Handler/stream/Control 接线和本卡由 recovery_lead；codec/正式语义由 skills_milestone_review；真实 WSS fixture 与方法另有独立作者，精确 selector、实际全输入和验证结果在其冻结后补入。不预先声称可编译、通过或原资源已退出。

后继实际验证须保原 child Wait、pipe/sink 返回、Owner/Handle/Manager Drain、session workers、wire 和 TLS peer/server 全部真实收尾。没有 PG 资源；现有 WSS 自动 HOST_TCP 差额和 Command 的本 SID 观测按实际依赖组合，不把可见 SID 空等同逃逸整树不存在。无新业务默认 timeout、不改旧 STOP。
