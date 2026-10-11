# Project Scheduler 配置 UI

状态：固定技术候选 `bc4787c5` 的当前 Owner 配置页面、默认 App/browser 正常保存与丢响应恢复两场景已有限独立接受，等待 root 整体保存到 main。作者 `skills_milestone_review`，Go fixture 作者 `knowledge_text_fixture`，独审 `skill_package_review`；root 独占 Git。

契约采用 [Owner HTTP 卡](d08-project-scheduler-owner-http.md)及 [OpenAPI](../../../api/openapi/project-scheduler.json)，界面沿[项目设置设计](../../frontend-design/layouts/project-settings.md#7-scheduler-与项目审计)。`/:username/:project_name/settings/scheduler` 支持启停配置、有限正整数上限和显式不限；版本与上限全程保留 canonical int64 字符串。暂停不结束已有执行，启用不自动启动 planned Sprint，也不证明 Runtime 正在运行。

表单草稿绑定原 expected_version。PATCH 完整替换三个必需字段，仅返回历史两字段确认；原 Lookup 只发送原 key 与 `{command:"update"}`，完整历史 Project DTO 不含 Scheduler 值，也不证明传入意图 body。确认后另 GET 当前四字段，读取失败保确认、不重复保存。Unknown 保原 key/body/version，用户明确查证或按原请求重放；同 Session checking 保留草稿，身份变化或当前拒绝清除私有材料。版本冲突保输入，明确读当前值并采纳版本后才提交新意图。归档时按钮与方法双门禁止重放，但保原 Lookup/未决意图；恢复 active 后原重放仍使用原请求。

42项专项、129项共享回归、类型和全新 `dist-01` 构建实际通过；原件位于本树 `output/ai/project-scheduler-ui/checks-01/` 至 `checks-04/`。独审三个原反例 FAIL 与早期测试材料失败保留，修复权限拒绝后的 busy、独立资料编辑误清意图及归档重放。上述受控检查与真实浏览器分段记录，不合称一次全量。

`TestProjectSchedulerWeb` 两子例经真实默认 App/run/listener、正式 Owner HTTP、原 Project Service/PG 验证。默认项目仍从暂停开始，测试通过原 HTTP seed 启用至 v2；正常浏览器 v2→v3 暂停并设置 `9007199254740993`→v4 启用/null，每次 ack 后独立 GET；lost 子例只发一次 v2→v3 PATCH，业务与 Audit/Outbox/命令事实通过后原 proxy 物理截断响应，页面保持未确认，用户仅查询同 key 的旧 Lookup，再独立 GET 当前配置，无重发 PATCH。所有响应均绑定原 request ID、完整或真实失败的 reader、公有 consumer、服务端 EOF/Close/handlerReturn 与适用持久事实。

`native01` 因测试无条件收起默认展开的栏目而 wholeFAIL，两子例均未到 Scheduler 请求；原工具15945 actual1及全部资源尾保留。仅修专属 spec 导航和安全诊断后，`native02` Go 1 top/2 sub PASS44.33s（22.23s/22.11s）；normal 9请求8 finished，原 Project GET `ERR_ABORTED` 由完整联合消费证据接受；lost 8请求7 finished，原 PATCH `ERR_CONTENT_LENGTH_MISMATCH` 保留，不能称全部 finished。三 PNG 由独审本人查看，浅色1440与深色390、品牌解码、键盘导航及无横向溢出在本场景范围接受。

原配方与输入闭包见 `output/ai/project-scheduler-ui/native02/invocation.json` / `input-hashes.json`（2439项），原日志 `/tmp/psw02/ui-f307086951b24456.log`，业务 JSON/三图在 `native02/evidence/TestProjectSchedulerWeb/`。原 Go/两 Node/driver/八 Chromium Wait0、七资源双absence、private/runtime/descendants/HOST_TCP双尾及inputs unchanged齐，原外层工具1728本人wait actual0/145.225s；result/terminal记录同目录。独审已亲核17条联证、三图及原尾后有限接受，不需重跑未变输入。

默认 App 动态接通不等于生产初始化；Project seed 与静态托管为显式测试设施，退出使用进程内 signal channel，不冒外部 OS signal。真实撤权竞争、完整权限/故障矩阵、在运行 Execution 的暂停行为、Scheduler Runtime、完整 D08 与生产 `ready=false`/503、既有 STOP 均未由本片解除。
