# Knowledge Owner 已有文档改名

- 分支/树：`ai/knowledge-owner-rename`，`/workspace/agenteam-knowledge-owner-rename`，正式 main `04455194`。本树唯一实现/current 写者 content；全部 Git 由 root 负责。
- 目标：已有文档 rename→原 Lookup/显式重放→当前 metadata/body 与 root+已知父层有界重读。正式规格为 [D12 主卡附段](../docs/development/work-items/d12-knowledge-documents.md#owner-已有文档改名-ui实施规格尚未验收)。
- 已完成：SPEC 已 checkpoint 81066224；七生产源与三核心测试已落盘初稿，尚未类型/测试。原同锁 web/node_modules 已离线复制到本树私有目录，未复制 Vite cache。正在准备限定客户端/状态/页面测试与类型检查，不冒用旧 read04。
- 七生产源：新 `web/src/api/knowledge-commands.ts`、`web/src/composables/useKnowledgeRename.ts`、`web/src/components/knowledge/KnowledgeRenameDialog.vue`；旧 `web/src/api/client.ts`、`web/src/composables/useSession.ts`、`web/src/composables/useKnowledgeOwner.ts`、`web/src/views/projects/ProjectKnowledgeView.vue`。三新测试：`web/src/tests/knowledge-command-client.spec.ts`、`knowledge-command-state.spec.ts`、`knowledge-rename.spec.ts`。不改 Workspace/router/nav/UiTree/Go/backend/锁/D13/其他命令。
- 关系门：Move 不涨版本，receipt.parent 不强等旧 parent；current 同 doc 版本不得低于 receipt。confirmed 不因重读失败倒退或重发；root+提交前/receipt/current 三 parent 去重，非根仅已加载，最多四层首屏，旧 cursor 失效。发布绑定 identity/Project/selected doc/编辑代次；checking 保未定材料，真实 identity/CSRF 变化才清，actual finally 未回不放 Cookie lane。
- 下一步：初稿十技术源+本卡/current冻结供 root 保存；执行三新测试与受影响旧读取/会话检查、类型，失败窄修后交 skills actual diff 审。真实 Go/PW fixture 与 shared entry 另分派，当前没有 browser/socket 授权。
- 恢复：本 current 替换继承的其他任务状态；历史读取结果留原 ai/knowledge-owner-ui 与正式 D12 卡，不回填。可重建产物放 `output/ai/knowledge-owner-rename/`；必要源码/失败结论跟踪保存，不重推已删除 topic。
