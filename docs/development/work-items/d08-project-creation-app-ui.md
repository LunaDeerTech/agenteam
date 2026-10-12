# 默认 App 项目创建与原身份恢复

2026-10-12，默认 App 创建与原意图恢复已完成限定验收并交付 main `25ee6e3df39782bd7957bfa4e12f84edeedd426d`；真实组合受测版本为 `600681fc`。原开发基线 `f7d62f186ace2bdd6b40bd7d88fc8c3e67715b96`、树 `agenteam-project-creation-app-ui`。当前用户可从项目列表创建项目，完成真实 Skills/Object 初始化并进入项目；Archive/Delete 未开放，完整 D08/D10 未完成。

本片将 [D08 §7/§8](d08-project-owner-design.md) 与 [D10 §1/§14](d10-skills-initialization-design.md) 的默认 initializer/full manifest 联动改为分能力开放。创建仍须真实四口初始化、完整锁 union/sameTx 确认、原 key 恢复和所有原调用返回；Archive/Delete 仍须完整 Registry 后才可接受。旧 `649e6ad3` 接线及 `0747b02c` 规范撤回历史保留。没有新 DDL、假 ready、空 Registry、full participant 或删除能力。

后端复用实际 SkillService、Store、Project Authority、Audit/Event、ProcessGuard，保原启动顺序。Account 正常 Drain 已在 Skills 前等待 Project；Force 同样在实际 Project work 未 Joined 时保留其下游，含 late install/原 force context。根资源层原 Store.ForceClose 政策不变，不以 deadline 代实际返回或承诺永久保 DB。

产品域为 `app/project_update.go`、`app/account.go`、新 `project/http/creation.go`/`creation_wire.go`、`api/openapi/project-creation.json`；前端独立作者负责创建 API/状态/表单与 ProjectListView，复用原 Session owner。既有默认根测试改用默认 Project 创建，保留其 Skill/Object/Knowledge/退出断言；HTTP 基础控制与实际 App/浏览器验收按影响补充。

HTTP：`POST /api/v1/projects` 接 16 KiB strict `{project_id,name,description?}` 与 Idempotency-Key，复用 Cookie/Origin/CSRF 和原请求流预算。201 为 `{state:"ready",project}`，202 为 `{state:"accepted",operation,status_url}` 与 Location；operation.version 是十进制字符串，state 区分 accepted/initializing/failed。`GET/HEAD /api/v1/project-creations/{id}` 200 返回同 union，failed 如实展示。原 domain 确定 Fault/CommitState 保持。新建意图固定 projectID/key/payload；Unknown 未获 creationID 时用户显式原 POST 恢复，已有 ID 可读原 status 并显式继续；不新增任意 key 枚举，不换目标或自动新建。

必要验收：

1. 真实 app.Run/Session/TLS/PG/MinIO 创建、原初始化发布及同 Tx 确认，浏览器从项目列表创建并进入当前项目；同 key 重放和新 Project 的既有 Agent/普通 Variables 真实消费。
2. 未授权/跨 Owner、strict input、原完整响应/Body 关闭；已完成重放可返回当前重命名或 archived Project，不强制历史 name/active。
3. 普通事务锁/本地返回屏障下的 pending、失败/取消和原 key 恢复；必要 Unknown 只装饰已明确底层结果的 Store 返回，不称网络 COMMIT 故障。原身份/唯一发布事实保持。
4. Project/Skills/Object 原调用未返回时，正常 Drain 和 Force 不越过未 Joined 的 Project；释放后亲收实际退出与 guard，含原预算及根 Store.ForceClose 例外。浏览器核 pending/failed/Unknown/恢复状态、键盘焦点及窄宽/明暗显示。

[Object safe 修复](d05-object-runtime-join-safe.md) 的 native01/02/03 有限通过可复用，旧 Runtime join 明确缺陷不再笼统阻塞本片。旧被禁网络探针、事务代理/截断/异常 backend 保留或改派重建不恢复；不重跑未改旧整矩阵。本片不宣称全模块 ready、Cleanup、foreign join、完整 Delete 或 E01。

## 实际结果与验收边界

- 前端创建 API/Session/表单 8 项控制与类型检查通过；后端 HTTP/App 定向 race（含默认 vet）11 top / 37 sub 全部通过，涵盖 201/202 union、当前 Owner、原请求返回、Force 下游门及 late install。原 [前端记录](../../../output/ai/project-creation-author-checks/frontend01/result.json)与[后端记录](../../../output/ai/project-creation-author-checks/backend01/result.json)保留；组合[类型检查/生产构建](../../../output/ai/project-creation-delivery/frontend01/result.json)通过。
- 原 `5267 → edf33b` 实际退出 0，两个真实 top / 三个 sub 全部通过：`TestProjectCreationApp/default-create-and-configure` 24.52s、`create-through-browser` 44.87s、`TestProjectCreationRecoveryApp/cancelled-create-restarts-original-intent` 23.60s。原 Go、Node、App 及收养子进程实际返回，六个自有容器/网络和私有目录均退出，未留 runtime/child；见[原日志](../../../output/ai/project-creation-delivery/browser01/browser.log)、[原结果](../../../output/ai/project-creation-delivery/browser01/result.json)与[外层 Wait](../../../output/ai/project-creation-delivery/browser01-outer.json)。这是普通真实 App/PG/MinIO 链，不使用被禁网络故障探针。
- 正常链通过公开 Session/CSRF/TLS 创建，核原 Add Skills revision、Object 发布与 MinIO 包字节；同 ID/key/body 再 POST 及 GET/HEAD status 返回原结果，跨 Owner 与同 key 异义拒绝。新 Project 通过正式 Provider/Model 配置后使用 Agent、普通变量与 Secret 入口。浏览器从项目列表完成一次 POST、重读目录并进入新项目，原响应与当前消费闭合，保留桌面浅色及窄屏深色截图。
- 恢复链以真实 owned PostgreSQL SHARE 表锁阻住首次 Skill 初始化 INSERT；确认原等待后仅取消原 HTTP client，亲收 client/bridge 返回、原 SQL 消失及产品 failed/terminal checkpoint。释放原锁、旧 App/guard 实际退出后，重启默认 App，以同 ProjectID/key/body、同 creation/initialization key 完成唯一真实发布。此例证明已知 failed 的原意图恢复，不冒真实网络丢响应或 COMMIT Unknown；浏览器 pending/failed/Unknown 的状态处理由受控前端验证承担，不称这些状态均已跑真实浏览器。

旧 `649e6ad3` 业务 PASS 与 `0747b02c` 规范撤回、其他历史失败均保持原结论。本片不开放 Archive/Delete，不代证完整 participant、引用退休、foreign 终局、生产部署 ready 或 E01。
