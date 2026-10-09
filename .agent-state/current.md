# 当前分支：普通 Project Variables Owner 后端

- 工作树 `/workspace/agenteam-project-variables`，分支 `ai/project-variables`，正式基线 `f1c94ee5`；root 负责 Git 与真实资源窗口。
- 唯一规格：[普通 Project Variables Owner 服务与 HTTP](../docs/development/work-items/d10-project-variables-owner-http.md) 已独立有限接受，卡 §2 写域已授权。迁移00024独占；不预消费 Knowledge00025/Runner00026，精确前序复制由root协调。
- 目标仅已有 initialized Project 当前 Human Owner 的普通变量CRUD、有界分页/expected_version、原key Lookup/重放、正式HTTP/defaultroot退出。复用Account/Project/Store/Audit/Outbox/cursor；排除Secret、白名单、Agent F1/Runtime注入、UI、Project创建及完整生命周期参与者，不称完整D10。
- 产品三阶段已保存：契约/Audit与三前端兼容；00024/领域事务/分页/事实Authority与Project分派；六能力HTTP/OpenAPI/defaultroot装配。相应pure/race/vet、前端99控/严格类型和两入口build实际通过。HTTP/root15源另经独立静审+7项实际纯控接受。原失败及修复边界见卡 §9；无整体产品验收。
- 00024曾遗漏旧Project Audit guard与新三动作兼容，已精确保留旧谓词并增合法tuple，独立窄审接受；正式ca9有界supervisor由root导入。首 Persistence 完整真实PASS：CRUD/no-op/delete/历史恢复、Audit/Outbox，所有Wait/自有资源/desc/TCP双尾闭合并释放窗口，不能外推其它top。
- 核心四源/四契约有限独审提出唯一 Create foreign-ID 分类缺口；作者先红控、最小修、领域race通过，独立差异复核接受。仅Create查Project marker及INSERT主键并发兜底改NOT_FOUND，同Project ID_CONFLICT和普通错误分类不变。真实双Project竞争尚未运行。
- 集成12个top已形成，Authority/Concurrency/HTTP/Recovery四新文件和Migration旧Work前置已保存于71ed2228；首 Persistence函数不改。当前 `variable-matrix-race-04.test` 最终离线race-c 70333实际exit0，精确三top及全12top发现通过。native3top已race-c/发现且保存，尚未执行；默认root两源现已构建，独立验收仍待派工/排窗。
- 下一组合精确 `^TestProjectVariable(Migration|PaginationAndLimits|Atomicity)$`，独立数据库顺序执行。原driver仅增加这一常量例外，15作者/10独立输入控接受；新私有 `pg-only-driver-storage-02` 已build。Go标志6m，受driver整体105s+15s cleanup、supervisor123s+3s退役+75s TCP尾约束；不扩大预算，不在freshgrant前运行。
- 当前无在途真实资源。产物位于 `output/ai/project-variables/implementation/`，真实首轮日志在 `output/ai/project-variables/pg/`。卡/current与本卡产品由本实例唯一写；独立probe归未参与者。全局状态见[任务台账](../docs/development/agent-team/tasks.md)，不复制UI/Model流水。

- 新默认root两源 `internal/central/app/project_variables_process_test.go`、`tests/process/project_variables_http_test.go` 可构建freeze：app race-c85715/list1实际0；process最终race-c36258、list52726及其真实TestMain两cmd离线build实际0。私有binary分别 `variable-app-race-01.test`、`variable-process-race-02.test`。均未运行top/监听器。
- 根已授权 `.agent-state/work-owner-http/root_chain_driver.py` 仅增加上述两精确selector→现app/process目录，旧三目标/全部资源链与预算不动。作者5旧新正+5非法负及实际input binding纯控通过，source其余逐字相同；首次控制脚本错误cwd的setup失败保留，纠正后独立执行通过，不涉及产品。固定SHA MinIO只读复制进本树ignored路径。该工具与两新根测试/card/current待下一安全保存和有限独审。
