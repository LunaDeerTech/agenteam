# Execution ToolCall 持久事实

- 工作树 `/workspace/agenteam-execution-tool-call-facts`，分支 `ai/execution-tool-call-facts-20261011`，基线 `226ea38b`；root 负责全部 Git。
- [正式任务卡](../docs/development/work-items/d22-execution-tool-call-facts.md)：首轮可信模型原句柄完成与实际 Joined 后的保护参数、ToolCall 事实和非终态 checkpoint；不是完整 Agent Tool Runner 闭环。
- lead 独占 Execution/Loop 与迁移 00064；runner_read_file 子作者已实际启动，独占 Tool/Registry 与 00063。runtime_recovery 为独立审核，main current 不属本树写域。
- 首完整产品候选已接版本 2 输入、原句柄实际 join、短事务 ToolCall/保护参数与 running checkpoint；Tool 当前/历史同事务端口及 00063/00064 已落。专属 14 个基础测试入口尚未执行；旧 DirectText 版本 1、默认生产 no-tools 与所有原 STOP 保留。
- 独立 PG 方法作者 skills_milestone_review 只写 tests/projectvariable/execution_tool_call_facts_test.go；唯一替身为 Model JSON 端口，不伪造 Execution/Operation authority。
- 当前仅源码、静态与 gofmt；Go/Node/网络/服务/native 需 root 排期。不得重启 OpenAI tools 独立 A/B 停项。

- PG 唯一 top / 两 sub 源已冻结；原双 harness 仅metadata键增量，离线1正7反及旧AST/maps保留控制actual0。canonical取消标记仅纯控，无正式Cancel写API，不把该边界冒为真实PG命令；所有Go/PG仍未运行。
