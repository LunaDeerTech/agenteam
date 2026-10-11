# Execution ToolCall 持久事实

- 工作树 `/workspace/agenteam-execution-tool-call-facts`，分支 `ai/execution-tool-call-facts-20261011`，基线 `226ea38b`；root 负责全部 Git。
- [正式任务卡](../docs/development/work-items/d22-execution-tool-call-facts.md)：首轮可信模型原句柄完成与实际 Joined 后的保护参数、ToolCall 事实和非终态 checkpoint；不是完整 Agent Tool Runner 闭环。
- lead 独占 Execution/Loop 与迁移 00064；runner_read_file 子作者已实际启动，独占 Tool/Registry 与 00063。runtime_recovery 为独立审核，main current 不属本树写域。
- 首源码候选为独立版本 2 输入/轮次 codec；旧 DirectText 版本 1、默认生产 no-tools 与所有原 STOP 保留。实现中，尚未编译/测试/真实联调。
- 当前仅源码、静态与 gofmt；Go/Node/网络/服务/native 需 root 排期。不得重启 OpenAI tools 独立 A/B 停项。
