# ToolOperation 当前恢复点

- 树 `/workspace/agenteam-tool-operation`，分支 `ai/tool-operation`，基线 `5659d300`（含只读 Builtin receipt 窄修）。唯一写者 secret；Git 归 root。旧 Registry/Builtin 源不改。
- [有限卡](../docs/development/work-items/d18-tool-operation.md)：ToolCallBinding/Execution 反向口、固定 install profile 的 Prepare/Lookup、原输入幂等与 00037 Operation/Attempt 表首稿已落。只写 created 元数据，不派发 Backend、不造 Execution 或授权。
- Execution 真实来源由 work_ui 的 00038/Execution 树提供；双方已对齐同 Store 原 Tx/完整锁、immutable snapshot 与当前 Capability∩Policy。标准 schema validator/D19/Skill AgentRun 最终门仍待真实绑定，Actor 值或纯控不能代替。
- 仅指定源码 gofmt 与 git diff --check 实际 0；未编译/测试/迁移/资源。00037 必须连续前缀，38 将补真实 Execution 复合 FK；原 STOP 不变。当前无本域在途 Go 或资源，首稿停写待 checkpoint 后补必要基础控。
