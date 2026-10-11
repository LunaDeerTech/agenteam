# Execution ToolCall 持久事实

- 工作树 `/workspace/agenteam-execution-tool-call-facts`，分支 `ai/execution-tool-call-facts-20261011`，基线 `226ea38b`；root 负责全部 Git，main current 不属本树写域。
- [正式任务卡](../docs/development/work-items/d22-execution-tool-call-facts.md)：原模型句柄实际 Joined 后写保护参数、ToolCall 与 running/tool-pending 事实；不是完整 Agent Tool Runner 闭环。
- 首完整源码、迁移 00063/00064、唯一 PG 两场景与原 harness metadata 已冻结并通过独立静审。Tool 与 PG 子作者已停写，runtime_recovery 为唯一主审。
- 基础 race 分段 34 top 通过（14 个新入口、20 个适用旧入口），五包 vet 通过。首轮 unit01 因新私有接口漏声明已有 LogValue 编译失败；只补该接口两行，四个已过包复用，只补 Execution 13 top，原 FAIL 与实际 Wait 保留。100 个有界输入前后相同；结果在 `output/ai/execution-tool-call-facts/checks/`。
- 原 profile 1 正 / 7 反及旧 AST/maps 保留控制实际 0。PG 首次 race-c/list 已实际 0，1806 项编译输入前后同；native01 两 sub 在真实 Model 捕获的 no-tools 门返回 CAPABILITY_UNSUPPORTED，整轮 actual 1；原 Wait、七资源、TCP 双空和输入门完整，未到 ToolCall 写入验证。
- PG 唯一替身为 Model JSON 端口。当前无正式 Cancel 写 API，canonical 取消标记仅纯控；生产 no-tools/nil/unbound、旧版本 1、OpenAI tools 独立 A/B 及所有原 STOP 保留。

- 显式 `model.NewToolCallFactsCapture` 已由独立主审接受：只捕获真实 ToolCalls 配置与原 Secret lease，独立 configuration-only revision；默认 Resolver/Capture/Runtime 和 STOP 不变。Model 控制实际 26 top 分段通过 / vet 0，原 25 PASS + 1 测试 scaffold FAIL 保留，修后仅补该 1 top。
- `compile02` / 精确 list 实际 0，1808 编译输入前后同。`native02` 原工具 24135 actual 1，全资源 / Wait / TCP 双空 / 输入尾齐；两个场景已到原 Model Close barrier，在夹具跨域 UUID/text 计数 SQL 失败，异常清理缺真实 Runtime registry。PG 唯一作者正窄修两查询及正式 Runtime 生命周期装配，不调用 tools HTTP。原两轮 whole FAIL 保留，尚不交付 main；源码不涉及生产或迁移返修。
