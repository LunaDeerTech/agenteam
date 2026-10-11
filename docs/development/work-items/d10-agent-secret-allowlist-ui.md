# D10 Agent Secret Variable 白名单 UI

本片以 `9bc9e13a` 为基线，产品候选 `a52610da` 已完成普通 Project Owner 在现有 Agent 创建、编辑页选择 Secret Variable 安全元数据的配置流程。variables_review 负责 Web，secret_allowlist_fixture 负责默认 App/浏览器方法；skills_milestone_review 已独立接受 native01 的业务、五张实际截图和完整退出尾。本次合入 main，Git 与资源排期由 root 负责。

正式协议沿 [Agent 管理](../../architecture/agent-management.md)、[Agent configuration OpenAPI](../../../api/openapi/agent-configuration.json) 及 [Secret Variables OpenAPI](../../../api/openapi/secret-variables.json)。目录只有原八字段安全 metadata，选择使用稳定 Secret ID。创建提交完整集合；编辑未触及字段时省略保留，显式 `[]` 清空，`null` 拒绝；每集合最多 128，完整 Tool/Mount/Secret 引用总数最多 256。没有额外 SystemAdmin 门。

目录采用表单内显式读取，完整成功页才原子发布；未在已读页出现的当前 ID 保留，读取失败不清空选择。当前身份、项目、路由退役与真实请求取消尾沿唯一 Session owner；checking 恢复保留草稿，已读取 ID 的响应式集合保证成功重读会重新计算校验。Unknown 保留原完整 intent/key，只显式查询原操作；提交确认后独立 GET 当前 configuration，历史 receipt 不作为当前数据。

真实默认 App、runtime-off 链已通过：normal 为浏览器 Create 1 次、Update 2 次，依次选择 S1、换 S2、显式清空，每版独立回读，合计 configuration GET 4 次、Secret 目录 GET 4 次；lost 为浏览器 Update 1 次实际提交后响应截断、同原 key/完整 intent Lookup 1 次，再独立 GET，未重放 PATCH。Go 正式服务创建两个 Secret，值只留 Go；每次写后核 Agent canonical allowlist 与 Secret provider references、版本及命令/Audit/Outbox 后验，非空引用的事务一致性和清空后的双域缺席均成立。

native01 的 normal 为 30 请求：28 finished、2 个 Mount GET 实际 ERR_ABORTED；lost 为 19 请求：16 finished、2 个 GET ERR_ABORTED、1 个截断 PATCH 的 CONTENT_LENGTH_MISMATCH。49 条原请求联证完整，不能改写成全部 finished。原 Go/Node/driver/Chromium Wait、七资源两轮 absence、private/runtime/descendant 与 HOST_TCP 双尾、输入不变门均通过；原 outer actual0，未重试。此验收不包含 Secret 值 UI、运行注入、Runner 执行、生产 Project initializer 或完整 D10，原 STOP 保持。

前端控制分段通过：3 项 client、最终 9 项真实 Session/页面控，以及原 Runner 24 项受影响控；不是一次 36 项全绿。build02 含 type/Vite、Session binding02 均 actual0，fresh `dist-02` 为 82 个物理资产。Go 复用 compile-web01 的实际 race-c/list 通过 binary，方法、PW 与 harness 没有因恢复窄修重编。原件位于 `output/ai/agent-secret-allowlist-ui/` 的 `checks/`、`methods/`、`compile-web01/`、`native01-run/` 与 `native01-evidence/`；外部原监督日志为 `/tmp/asaw01/ui-206e564776804426.log`。

原 FAIL 全部保留：unit01 整轮 34 PASS/1 FAIL 是新页标签定位错误；controls01 整组 FAIL 是额外 SystemRunner 控制使用了错误的 token DTO；checking-repro01 是实际恢复后校验消息缓存缺陷，修后 page03 9 PASS。旧 dist-01、原命令和输出不覆盖。可恢复控制源为 [.agent-state/agent-secret-allowlist-ui/method-controls.mjs](../../../.agent-state/agent-secret-allowlist-ui/method-controls.mjs)，从仓库根执行 `node .agent-state/agent-secret-allowlist-ui/method-controls.mjs`，使用 harness 同 lock 的物理私有依赖。controls01 与该修后源码仅末项 `const sensitive` 输入不同：旧形状 `enrollment: { token: 'never-retain', expires_at: 'safe' }`，修后为正式 `token_available/enrollment_token/expires_at`。这是 fixture 修正，敏感投影断言未削弱。

共享 `task-review.helpers.ts` 在主线已交付 SystemRunner UI（`f86f4215`）的 helper 上叠加本片 Secret 差额，结果保持本片原验收 helper 字节；两 harness 保留 main 的 SystemRunner、DirectText 与所有既有 profile，再加入本片 selector。全部正式方法与控制源码入 Git；原日志/FAIL、资产、binary 和私有依赖留 ignored 输出，必要历史输入差额在退役前按正式恢复规则保全。
