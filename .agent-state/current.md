# Execution ToolCall 持久事实

- 工作树 `/workspace/agenteam-execution-tool-call-facts`，分支 `ai/execution-tool-call-facts-20261011`，基线 `226ea38b`；root 负责全部 Git，main current 不属本树写域。
- [正式任务卡](../docs/development/work-items/d22-execution-tool-call-facts.md)：原 JSON 模型句柄实际 Joined 后同事务写保护参数、ToolCall / Transcript 和 running/tool-pending；真实当前 Execution / Tool 权威校验，非完整 Agent Tool Runner 闭环。
- 产品、迁移 00063 / 00064、PG 原两场景及方法已固定并静审接受；第三轮 `native03` whole PASS，原工具 52694 actual 0 / supervisor 679899 actual 0（120.995 秒），两 sub 29.28 秒。Go / driver actual Wait 0、七资源和私有 / runtime / descendants / TCP 双尾、输入未变齐全，已释放实际窗口。唯一主审已接受有限 wholePASS，等待 root 主线集成。
- 基础 race 34 top、显式 Model capture 26 top 均分段通过，适用 vet 0；原 unit01 接口编译 FAIL 与 model01 测试 scaffold FAIL 完整保留。compile03 / 精确 list 实际 0，1808 编译输入前后同，runtime 1809 仅加原 binary。原件统一在 `output/ai/execution-tool-call-facts/`。
- native01 原 Model 默认 no-tools 门 FAIL，随后增加独立显式 configuration-only capture，默认 Resolver / Capture / Runtime 不变；native02 原夹具跨域 UUID/text SQL 与缺 Runtime registry FAIL，原文件窄修真实依赖；原两次 whole FAIL 及完整尾均保留，不回填通过。
- 唯一 Model JSONCaller 为受控端口；真实 Runtime registry 仅用于退休，不调用 Chat / tools HTTP。Project 有持久 test Skills receipt 限制；无正式 Cancel producer，canonical 取消仅纯控。尚无 ToolOperation / Runner dispatch 或默认绑定；旧 v1/no-tools/nil、OpenAI tools 独立 A/B 及全部原 STOP 保留。
- 本分支 tracked 历史均属于本片，无未归属 WIP；root 拟保留完整历史后合入 main，2 个共享 harness 必须保全主线所有 profile。未经正式主线保存和完整 output / tmp 原件保全，不删除本树。
