# Knowledge Owner 只读 UI

- 分支 `ai/knowledge-owner-ui`，本地树 `/workspace/agenteam-knowledge-owner-ui`；基线 `bacbb28d` 完整默认根候选，规划保存 `b6102066`，治理同步 `3107ce4d`。root 独占 Git，content 独占本树下述源码/current；candidate 与旧正文树均不写。
- 已读 origin/main `4d1cf3d2` 三治理；完整任务尚未交付，不清理任何树或引用。root 已授权四 GET 的最小 Owner 树/metadata/正文 UI，Skills 对接口规划有限静审接受，未证明 UI 实现或真实调用。
- 正式范围见 `docs/development/work-items/d12-knowledge-documents.md` 末段。四新生产源：`web/src/api/knowledge-owner.ts`、`web/src/composables/useKnowledgeOwner.ts`、`web/src/views/projects/ProjectKnowledgeView.vue`、`web/src/components/knowledge/KnowledgeDocumentTree.vue`；七旧窄接缝 client/Session、router两文件、ProjectNav、UiTree/types。新三 spec 加相邻组件/路由测试及必要文档。后端/锁文件/来源/parser/STOP 不变。
- 当前写到 API+client+Session：完整 DTO/union/UTF8 offset、原 5MiB/7MiB 表示界、Human current/revision/cancel/failure 与唯一 Cookie owner；尚未完成页面/controller 与相应 tests，未验收 UI。节点依赖从 Work UI 的相同 package-lock 离线复制到本树私有 node_modules，锁未改。
- 下一步：定向类型检查，再完成 controller/树/页面与 exact 纯控；所有新 UI Go/browser/socket/网络未运行且无资源授权，真实 top 与共享 entry 后续由 root 调度。既有正文/root有限PASS复用，不升级到 UI/ProjectCreateHTTP/SPA 发布。

## 第一片段：四 GET / Session 接缝

- 新 `web/src/api/knowledge-owner.ts` + `web/src/tests/knowledge-owner-client.spec.ts`，旧 `web/src/api/client.ts` / `web/src/composables/useSession.ts`。35 个实际客户端纯控 PASS（Vitest 4.1.11，session 77376 actual0，1.56s），`npm --prefix web run type-check` 修后 actual0（90183）；源码格式检查及编译不证明 Session 组合或 UI 动态通过。
- 首 type-check 21638 actual2：unavailable literal 被 Object.freeze 推断为 string；仅补 `as const` 后上述修后检查通过。保留失败事实，不改正式表示。首检查启动与仅授权源码格式化尾短暂交叉，不计为稳定片段通过；实际格式化67942已0，修后90183在停写源上通过。
- 此四源码/测试可停写保存；controller/页面未创建，下一片段独立编写。依赖使用相同锁的私有复制，无新增包/lock变化，无网络或实际资源命令。
