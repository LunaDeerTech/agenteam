# Knowledge Owner 已有文档改名

- 分支/树：`ai/knowledge-owner-rename`，`/workspace/agenteam-knowledge-owner-rename`，正式 main `04455194`。本树唯一实现/current 写者 content；全部 Git 由 root 负责。
- 目标：已有文档 rename→原 Lookup/显式重放→当前 metadata/body 与 root+已知父层有界重读。正式规格为 [D12 主卡附段](../docs/development/work-items/d12-knowledge-documents.md#owner-已有文档改名-ui实施规格尚未验收)。
- 已完成：SPEC 已 checkpoint 81066224；七生产源与三核心测试已落盘初稿，尚未类型/测试。原同锁 web/node_modules 已离线复制到本树私有目录，未复制 Vite cache。正在准备限定客户端/状态/页面测试与类型检查，不冒用旧 read04。
- 七生产源：新 `web/src/api/knowledge-commands.ts`、`web/src/composables/useKnowledgeRename.ts`、`web/src/components/knowledge/KnowledgeRenameDialog.vue`；旧 `web/src/api/client.ts`、`web/src/composables/useSession.ts`、`web/src/composables/useKnowledgeOwner.ts`、`web/src/views/projects/ProjectKnowledgeView.vue`。三新测试：`web/src/tests/knowledge-command-client.spec.ts`、`knowledge-command-state.spec.ts`、`knowledge-rename.spec.ts`。首纯测后唯一新增 `web/src/router/auth.ts` 导航注册点写权：原 restore 前确认，保已转入 Skills canonical route hunk；不改 Workspace/router index/nav/UiTree/Go/backend/锁/D13/其他命令。
- 关系门：Move 不涨版本，receipt.parent 不强等旧 parent；current 同 doc 版本不得低于 receipt。confirmed 不因重读失败倒退或重发；root+提交前/receipt/current 三 parent 去重，非根仅已加载，最多四层首屏，旧 cursor 失效。发布绑定 identity/Project/selected doc/编辑代次；checking 保未定材料，真实 identity/CSRF 变化才清，actual finally 未回不放 Cookie lane。
- 下一步：初稿十技术源+本卡/current冻结供 root 保存；执行三新测试与受影响旧读取/会话检查、类型，失败窄修后交 skills actual diff 审。真实 Go/PW fixture 与 shared entry 另分派，当前没有 browser/socket 授权。
- 恢复：本 current 替换继承的其他任务状态；历史读取结果留原 ai/knowledge-owner-ui 与正式 D12 卡，不回填。可重建产物放 `output/ai/knowledge-owner-rename/`；必要源码/失败结论跟踪保存，不重推已删除 topic。

## 首基础检查与窄返修

- 初稿12路径已root保存 `0a857335`，owned format 80038 actual0；首类型41795 actual0/22.123s。
- 首pure86317 actual1/10.266s，29项24通过5失败，原log/result在 `output/ai/knowledge-owner-rename/pure-01/`。3个client阳性fixture遗漏正式Instant六位微秒；其余为新controller错误清除same-session恢复后的unknown意图、同组件route guard晚于全局auth.restore导致确认前页面卸载。先前“可能Project先改上下文”只是初猜，实际源码确认是restore顺序，未按初猜修改Project。
- 窄修：fixture对齐Instant；同会话检查退休旧UI但保Session原未知材料；新动作failure仅current能清身份；根授权唯一第8生产源auth.ts增加pre-restore确认，已导入Skills `1b5fa6b5` canonical suffix原hunk保留。新增一个已约并发Move父层集合控制，未改成功门。修后检查待运行；未跑browser/socket。
