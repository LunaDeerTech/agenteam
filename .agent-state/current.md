# Knowledge Owner 只读 UI

- 分支 `ai/knowledge-owner-ui`，本地树 `/workspace/agenteam-knowledge-owner-ui`；基线 `bacbb28d` 完整默认根候选，规划保存 `b6102066`，治理同步 `3107ce4d`。root 独占 Git，content 独占本树下述源码/current；candidate 与旧正文树均不写。
- 已读 origin/main `4d1cf3d2` 三治理；完整任务尚未交付，不清理任何树或引用。root 已授权四 GET 的最小 Owner 树/metadata/正文 UI，Skills 对接口规划有限静审接受，未证明 UI 实现或真实调用。
- 正式范围见 `docs/development/work-items/d12-knowledge-documents.md` 末段。四新生产源：`web/src/api/knowledge-owner.ts`、`web/src/composables/useKnowledgeOwner.ts`、`web/src/views/projects/ProjectKnowledgeView.vue`、`web/src/components/knowledge/KnowledgeDocumentTree.vue`；七旧窄接缝 client/Session、router两文件、ProjectNav、UiTree/types。新三 spec 加相邻组件/路由测试及必要文档。后端/锁文件/来源/parser/STOP 不变。
- 第一片段 API+client+Session 已保存 b0c89d6e，Skills 对四技术源实际静审有限接受。当前 controller/树/页面及路由/UiTree 窄接缝为未验收 WIP，暂停实施。节点依赖从 Work UI 的相同 package-lock 离线复制到本树私有 node_modules，锁未改。
- 当前优先级：按 root 指令冻结 UI，转 candidate 撤回违反 D08/D10 manifest participant 门禁的默认 Project initializer 绑定；UI 真实 fixture 方案也须按该边界重整后再继续。所有新 UI Go/browser/socket/网络未运行且无资源授权，真实 top 与共享 entry 后续由 root 调度。既有正文/root有限PASS复用，不升级到 UI/ProjectCreateHTTP/SPA 发布。

## 第一片段：四 GET / Session 接缝

- 新 `web/src/api/knowledge-owner.ts` + `web/src/tests/knowledge-owner-client.spec.ts`，旧 `web/src/api/client.ts` / `web/src/composables/useSession.ts`。35 个实际客户端纯控 PASS（Vitest 4.1.11，session 77376 actual0，1.56s），`npm --prefix web run type-check` 修后 actual0（90183）；源码格式检查及编译不证明 Session 组合或 UI 动态通过。
- 首 type-check 21638 actual2：unavailable literal 被 Object.freeze 推断为 string；仅补 `as const` 后上述修后检查通过。保留失败事实，不改正式表示。首检查启动与仅授权源码格式化尾短暂交叉，不计为稳定片段通过；实际格式化67942已0，修后90183在停写源上通过。
- 此四源码/测试已保存 b0c89d6e，保持停写；第二片段独立 WIP 见下。依赖使用相同锁的私有复制，无新增包/lock变化，无网络或实际资源命令。

## 第二片段冻结：未验收 WIP

- 新四路径：`web/src/composables/useKnowledgeOwner.ts`、`web/src/components/knowledge/KnowledgeDocumentTree.vue`、`web/src/views/projects/ProjectKnowledgeView.vue`、`web/src/tests/knowledge-owner-state.spec.ts`。旧五路径：`web/src/components/layout/ProjectNav.vue`、`web/src/components/ui/UiTree.vue`、`web/src/components/ui/types.ts`、`web/src/router/auth.ts`、`web/src/router/index.ts`。这九路径与本 current 均停写，交 root 保存；无在途进程或实际资源。
- controller 仅四 GET、当前 Human/Project generation、原 Cookie owner 实际退役后继续、显式目录分页及单正文段；祖先只作路径不伪造子目录。UiTree optional expandable 同步 disclosure/键盘/ARIA，默认语义保留。页面/窄路由接缝已写，尚无页面 spec、相邻兼容控、私有 build 或第二片段独立审。
- 第一轮全量 type-check 62736 actual2：局部 emptyAncestors.message/emptyContent.offset literal 推断过窄；只给新 controller 局部空状态函数显式返回类型后，95398 actual0。新片段 Prettier 实际0。
- Session/controller exact state 检查 80391 actual1：13 控中12通过，`keeps a local 403 out of System denied state` 失败保留。已静态定位测试 fetch fixture 只匹配 `/api/v1/system/users`，实际正式客户端请求带 `?limit=25`，落入不匹配 JSON；尚未修复/重跑，不能称整组通过。其余结果包含普通非 admin/current401/迟到401、原 reader+outer held 尾、分页、祖先路径、UTF8 offset/版本变化、tombstone/unavailable。
- 暂停后不继续 UI 实施。原默认 root 649e6ad3 业务执行 PASS 仍为历史事实，但此前对生产 initializer 接缝的接受已因 canonical D08/D10 门禁撤回；不得用该历史结果为 UI 新建 Project fixture 或生产创建背书。既有正文 HTTP 有限验收未因此改写。所有网络/socket/PG/browser 未运行。
