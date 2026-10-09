# D27 Model UI：main 集成准备

只读核对基线：正式 main `3cea6076bb01693ead2755826826d189626aa3aa`；Model 单点 cast 已保存于 `38fdae31`，候选为 `account-reference-cast.test`，SHA `54e2a068faeb06cb1fbe9759dbefc00d695475858eaa582dc09c9ba6e1bca1f4`。这是待执行清单：authority18 完整 FAIL，authority19 未执行，仍新 5/6、旧 14/14、独立 A/B 已接受；不宣布整卡或新 main 组合通过。root 负责创建正式 main 基线独立交付树、Git 与最终推广。

## 精确差异与保留范围

卡 §8 的 31 路径已逐个对照 main blob（f8727f）。25 个 web 路径全部逐字相同：

- API：`web/src/api/client.ts`、`project-models.ts`、`project-model-credentials.ts`。
- 状态／装配：`web/src/composables/useSession.ts`、`useProjectModelSettings.ts`、`web/src/App.vue`。
- 路由／菜单：`web/src/router/auth.ts`、`index.ts`、`web/src/components/layout/ProjectNav.vue`、`web/src/views/projects/ProjectSettingsView.vue`。
- Model 页面：同 `web/src/views/projects/` 下 `ProjectModelProvidersView.vue`、`ProjectModelsPanel.vue`、`ProjectAvailableModelsView.vue`、`ProjectProviderEditor.vue`、`ProjectModelEditor.vue`、`ProjectModelCredentialEditor.vue`、`ProjectModelDeleteDialog.vue`。
- 单元：同 `web/src/tests/` 下 `project-models-client.spec.ts`、`project-model-credentials-client.spec.ts`、`project-model-settings-state.spec.ts`、`project-model-settings.spec.ts`、`authentication.spec.ts`、`project-workspace.spec.ts`、`session.spec.ts`、`project-audit.spec.ts`。

这些路径保留 main，不需要复制或再提交。`UiDialog.vue`、`UiPopover.vue`（`web/src/components/ui/`）、`web/src/composables/useLayer.ts`、`web/src/tests/dialog-outside-focus.spec.ts` 也逐字相同，共享修复不重复搬运。

实际待带入的测试差异恰为以下五路径：

1. 新 `tests/account/project_owner_models_web_fixture_test.go`：包括本轮唯一 `$2::agenteam_model.safe_id` cast。
2. 新 `tests/account/project_owner_models_web_test.go`。
3. 新 `tests/account-captcha-web/project-owner-models.config.js`。
4. 新 `tests/account-captcha-web/e2e/project-owner-models.spec.ts`。
5. `tests/account-captcha-web/e2e/project-owner-audit.spec.ts`：只在原菜单期待中追加“模型与 Provider”，其余逐字保留 main。

浏览器 spec 实际依赖 `.agent-state/model-ui-recovery/` 的 configuration-and-credential、read-and-pagination／read-pagination-contract、authority-and-identity、navigation-and-layouts、resolve-publication-observer、session-controller-binding、native-client-probe／build-native-client-probe、validate-same-body。Go 的诊断 tops 还消费 session-consumption-probe；执行／资源终态依赖 run-owned-top、fixture-go、owned_resources。不能只复制四个公开 harness 而遗漏这些文件。已接受方法的负控与卡内链接的必要失败输入原位保留；本次核对的已跟踪集合为 recovery 53、independent 10、regression 3 文件，另加本 brief。集成时按这三个限定目录的 tracked 文件保留其实际源码／必要记录，不带 `output/`、cache、binary、私有资产、原始日志或源镜像。

必要文档：更新 `docs/development/work-items/d27-project-owner-model-settings-ui.md`；完成后在 `docs/development/frontend/README.md` 新增本卡能力、真实命令、限制及分版本验收摘要（该 README 当前与 main 相同，没有整卡完成结论）；root 最后只更新任务台账对应项。端点附件 `d27-project-owner-model-settings-ui-endpoints.json` 已与 main 相同，保留原件；不覆盖主 README、后端 README、开发计划或整份活动树台账。

## Variables 与共享 HTTP 的实际差异

main 的普通 Variables 后端已交付，当前活动树尚未包含以下增量，禁止从活动树覆盖或删除它们：

| main 保留路径 | 活动树相对 main 的缺项 |
| --- | --- |
| `api/openapi/project-variables.json` | 整个已交付契约缺失 |
| `api/openapi/project-audit.json` | 三个 variable action、project_variable resource、对应安全 record schemas |
| `web/src/api/project-audit-metadata.ts` | 三个解析器、resource、身份／版本关联校验与 label |
| `web/src/tests/project-audit-client.spec.ts` | 正式 Variables Audit 的正负 client 控制 |
| `web/src/tests/project-audit-metadata.spec.ts` | 34 action／15 resource 及对应向量；活动树仍 31／14 |

这不是 Model 25 web 文件的文本合并冲突，而是整树移植会造成的既有能力回退。Variables UI 活动分支不在 main3cea，本交付不夹带它。`api/openapi/common.json`、`project-models.json`、`project-model-credentials.json`、端点附件、web 与 Playwright 两套 package/lock 当前都与 main 逐字相同；17 Model operation／9 IPC、原 Session facade 和路由接口无需适配。

## 新组合验证条件与执行顺序

1. 先等待 authority19 在原冻结 delivery11c／迁移≤22／原资产组合获得完整实际结果。候选编译、独审和推广都不能替代它，失败继续保留原门槛。
2. root 创建 main3cea 的独立交付树后，只装上述五测试路径、三个限定恢复目录和必要文档。保留 main 的 migrations23／24、Variables／Work root 装配与 Audit 安全投影；不得复制旧 delivery 的 Go／schema／资产来模拟新 main。
3. 调整新树执行路径后再预飞。当前四文件写死旧 `/workspace/agenteam-delivery`：recovery 的 `run-owned-top.py`、`fixture-go.py`，regression 的 `run-owned-regression.py`，independent 的 `run-independent.py`。新交付只能明确绑定 root 创建的新 Go 树；旧11c驱动／binary／证据继续原位保留。路径调整需原 exact selector／预算／七资源／实际 Wait 与输入同一控制，不能无意继续运行旧二进制。
4. 新树重新 race 编译 account fixture、精确发现六新 top；如运行独立 A/B，按其原 overlay 再构建本版本。原54e候选只属于11c组合，不作为main二进制。native bundle 可从实际源重建；资产必须从新 main web 生成到新树自有输出，不能覆盖当前 Model 私有 dist 或其它任务 web/dist。
5. 前端受影响检查只针对 main 的新组合：Project Audit metadata/client 单元、菜单兼容单元与浏览器 spec 的静态检查、类型检查／构建。25 Model web 与共享层同一时，复用已接受单元和14旧场景证据；不机械重跑它们。若实际整合又改 App/useSession/client/router、共享浮层或 Model schema，才按具体消费范围重开相应恢复／导航／权限检查。
6. 必须区分源码相同与根装配相同：旧 binary11c 到 main3cea 新增 Work／Variables HTTP、Outbox producer、Project Audit facts 和 Account 退役顺序，并有 migrations23／24。Model 生产目录未变，但旧真实通过不能冒成新 main root 已通过。建议新 main 最小实际补验为原 authority 全 top（覆盖启动、身份、归档、引用／失败路径及完整 join）与受 Variables Audit 投影／菜单变化影响的 Project Audit 原 top；具体所需精确子集在新树实际 diff／编译后由 root 定案，均另需 fresh 独占窗口。若新资产或装配改变了其它已验行为，按受影响场景追加，不放宽预算／EOF／finished／身份门槛。

本 brief 只做 Git blob／源码接口与依赖检查，没有编译新 main、重跑旧检查、启动 PG／browser／socket或生成全仓哈希。正式 main 当前包含前端源码，不等于 D27 整卡已经验收。
