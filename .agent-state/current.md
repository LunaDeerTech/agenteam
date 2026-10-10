# Knowledge Owner 只读 UI

- 分支 `ai/knowledge-owner-ui`，树 `/workspace/agenteam-knowledge-owner-ui`；`0658c54f` 已合正式后端 `fb84a892`，UI 已保存 `da77c639`，首链七源已保存 `15e730da`。root 独占 Git，content 独占本树 UI 下述路径及新首链测试；shared 入口由 coordination 单写。不写 candidate 或旧正文树。
- API/client/Session 第一片段 `b0c89d6e`、后续页面和目录失败清层修复均已通过对应离线验证及 Skills 有限 actual diff 审。首真实 Go/browser fixture 已实现并通过方法静审、race 编译与精确 list，仍未运行真实链、未验收 UI。后端、锁、parser、来源及 STOP 不变。
- 默认 Project initializer 已按 canonical 门禁撤回。修后 root-02 的默认 Create Unbound/零事实、显式测试 fixture 真 ports/Stop/Drain 和默认读取链已正式进入 main；旧 root-01 业务 PASS 仍仅历史事实，不为生产初始化背书。UI 后继 fixture 必须沿修后边界，不重新绑定默认 initializer。

## 唯一写域与当前结果

11 个生产路径：`web/src/api/knowledge-owner.ts`、`api/client.ts`、`composables/useSession.ts`、`composables/useKnowledgeOwner.ts`、`components/knowledge/KnowledgeDocumentTree.vue`、`views/projects/ProjectKnowledgeView.vue`、`components/layout/ProjectNav.vue`、`components/ui/UiTree.vue`、`components/ui/types.ts`、`router/auth.ts`、`router/index.ts`（未写全前缀者均在 `web/src/`）。四个测试为 `web/src/tests/knowledge-owner-{client,state}.spec.ts`、`knowledge-owner.spec.ts`、`components.spec.ts`。文档仅本 current、D12 主卡、frontend README/components。既有 Session/认证/Project 四 spec 只运行不修改。

- 四 GET 严格客户端、当前 Human Session dispatch 保持第一片段已审源。35 项客户端控（77376 actual0）及修后 type-check（90183 actual0）按未变输入复用。
- controller 只在当前 identity/Project 读取上下文内工作；单 Cookie owner 实际 finally 前不放行下一请求。目录按层分页，祖先仅路径，正文只保留一个 UTF-8 段及偏移导航。页面树默认未选择，文本安全展示，不获取来源或解析 PDF/DOCX。
- 本轮修复正式 System users query fixture；用响应式任务槽解除实际完成后的页面 busy；祖先 `403/410` 清除详情并停止正文链；Drawer 选择关闭保持原入口焦点恢复；UiTree `expandable=false` 同时约束旧展开状态、键盘、disclosure 和 ARIA，undefined 保留原行为。
- 最终稳定源检查 88317 actual0：7 个 spec、166 项 PASS，11.27s，包含新页面 10 项、state 16 项、组件 14 项及未变 Session/认证/Project 工作区兼容。受控 Fetch/jsdom 使用实际客户端/Session/工作区，不冒真实浏览器。
- 最终完整类型检查与私有正式构建 51976 actual0：`npm --prefix web run build -- --outDir /workspace/agenteam-knowledge-owner-ui/output/ai/knowledge-owner-ui/dist`；包含 `vue-tsc --noEmit`，Vite 8.3.1 / 294 模块。未写默认 dist 或锁文件。
- 独审发现目录继续页失败/取消后未标旧却仍保留行和 cursor；按 root 授权只返修 controller/state：失败 catch 与 loading/waiting 退役都清该层，其他 ready 层保留。新增根403、子503与 held 后页 Stop 控；修后 state/page 2 spec、29项 PASS（34409 actual0，9.45s），完整 `vue-tsc --noEmit`（22652 actual0）、两源 Prettier 与 diff 检查通过。原166项为返修前有效结果，未变兼容源复用；51976私有 dist 早于此返修，真实联调前必须由当前冻结源重建。
- 11 个生产源与4个测试的 Prettier 检查 49660 actual0；三份正式文档 UTF-8/LF 与74个本地链接/标题核对通过，`git diff --check` 通过。无在途进程；以下冻结范围可由 root 保存。
- 定向测试命令：`node web/node_modules/vitest/vitest.mjs run --root web src/tests/knowledge-owner.spec.ts src/tests/knowledge-owner-state.spec.ts src/tests/components.spec.ts src/tests/session.spec.ts src/tests/authentication.spec.ts src/tests/project-workspace.spec.ts src/tests/project-workspace-state.spec.ts`。Node 使用 `/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node`；私有 node_modules 来自相同锁的离线复制。

## 保留的失败及收敛

- 第一片段 type-check 21638 actual2：unavailable literal 推断过宽；显式 `as const` 后 90183 通过。第二片段 type-check 62736 actual2：空状态 literal 推断过窄；局部返回类型修后 95398 通过。
- 原 state 80391 actual1（12/13）：fixture 匹配 `/api/v1/system/users`，正式请求为 `?limit=25`；仅精确修 fixture，21650 actual0（13/13）。原拒绝判据未放宽。
- 新页面 62362 actual1（4/10）：按钮查找误把 aria-hidden 文字算入名称；改用既有页面测试同一查找方法。29463 actual1（6/10）暴露 controller 非响应式任务槽导致已完成仍 busy；修后 59259 actual0（页面/state 共23项）。
- 组件定向 33241 actual1（2/3，11 skip）：`expandable=false` 仍显露旧展开子节点；同步所有展开判定。后续 75280 actual1（37/40）暴露祖先拒绝后仍继续读取、Drawer 关闭焦点被 heading 抢走；精确修复后最终 88317 全部通过。历史 FAIL 不改写为 PASS。

## 下一步与资源边界

下一步由 coordination 完成 D12 独立精确入口与输入初尾闭包，root 保存后分配真实窗口；只运行一个 `TestKnowledgeOwnerReadWeb` 与一个正常 Playwright case。显式测试 Project fixture 实际 Stop/Drain，默认 Knowledge 创建父子文本，真实页面四 GET 与 UTF-8 下一段、窄屏键盘/焦点、原消费发布和取消尾及原七资源完整退出门。尚未运行 browser/socket/PG/network；当前实际资源窗口由 root 分配给其他任务。UI 未验收，不宣称编辑、完整 D12、生产 SPA 或 Project Create HTTP 可用。

## 首真实链 fixture 实施准备

root 已授权四新测试源与自有 native controls；生产11源保持 `da77c639` 冻结。新增 `internal/central/app/knowledge_owner_web_test.go`、`tests/account-captcha-web/knowledge-owner-read.config.js`、`tests/account-captcha-web/e2e/knowledge-owner-read.{spec,native}.ts`、`.agent-state/knowledge-owner-ui/native-controls.cjs` 和 README 已随 `15e730da` 保存。精确输入与失败见[恢复说明](knowledge-owner-ui/README.md)。最终同步双 seal 后 strict TS 80628 actual0，native 控制44484 actual0（67项/0 unhandled），当前 dist-read-01 正式构建44971 actual0；Skills actual diff 有限接受。固定锁 PW 私有依赖/MinIO已离线复制，无lock变动/网络。

首 race-c 36818 actual1/list 未执行：旧 `project_update_test.go` 三调用的末 nil 未随生产 initializer 撤回同步；仅此三处机械修复后该文件逐字正式 main。88190 修后 race-c + exact list actual0，候选 `output/ai/knowledge-owner-ui/knowledge-owner-web-race-01.test` 为60,147,683 bytes、恰1 top。使用私有 mode=off/去三旁路，同 process 空闲9,549,406,208 bytes。首 PW --list 80873 actual1/0tests，原因是完整 title 含文件前缀；root 授权只改 config 精确 grep，11664 actual0，恰1 case。最终 config 属运行时输入，Go/native/spec/dist 未变。原方法环境826b、类型20326、观测49762、编译36818、PW列表80873 FAIL全部保留。当前无在途作者命令；此批新增待保存四路径为上述 `project_update_test.go`、PW config、本 current 和恢复 README。真实 Go/browser 尚未运行，shared 入口由 coordination 独占。
