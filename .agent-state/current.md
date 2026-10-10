# 当前执行检查点

- 工作树 `/workspace/agenteam-project-variable-lifecycle`，分支 `ai/project-variable-lifecycle`，基线 main `33903460`；由 root 创建的稀疏树，正式源码/文档及本任务恢复源完整。
- 目标：普通 Variables 与 Secret Variables 两个现有真实 Service 的 Project 精确停止子能力；同 Store/唯一 Authority、真实 StopPhase 授权确认后取消原 call/confirmation，原调用实际返回才 LocalJoined。Archive 保合法读，Delete 纳入本 Project 读，另一 Project 不受影响。
- 当前只有既有 `recovery-project-domain-bindings.md` 的有限 SPEC 附段与本记录；产品/测试未写、未 Go/资源。SPEC 等未参与实现的 skills_http 有限审查，之后实现必要正常/失败基础并尽早准备首真实 PG。
- coordination 唯一写本任务 projectvariable 实现/测试及上述两文档；不写 app、Project、Audit facts、共享 harness、迁移、Schema 或依赖锁。00031 属 Model Runtime owner。全部 Git 由 root 执行。
- 首 PG 的 stopping operation/manifest 是规范 phase fixture，不冒生产 Archive API；生产 phase worker、foreign join、cleanup 和完整 participant 未完成。LocalJoined 不等于 Project stopped；生产 initializer unbound、Object Runtime join STOP 保持。
- 运行前固定 Go1.27.1、私有实际 telemetry off/去旁路、共享只读 module cache/自有 build cache、同进程 fresh≥5GiB。当前无在途命令或自有真实资源；PG/socket 必须 root fresh grant。
