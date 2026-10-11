# D10 Project Secret 创建与编辑 UI

基线 `73dccca1`。variables_review 实现，agent_display_settings 独审；Git 与资源窗口由 root 统筹。页面/API/Session、组合基础检查与 native02 默认 App/browser 原整轮实际通过；agent_display_settings 已最终有限接受，无 must-fix。首次浏览器定位失败完整保留。

## 范围与正式合同

沿 [Owner HTTP](d10-secret-variables-owner-http.md)、[Secret OpenAPI](../../../api/openapi/secret-variables.json) 和既有安全目录页面，当前普通 Project Owner 可创建 Secret、修改名称/描述、明确替换材料。无额外 SystemAdmin 门，不包含删除、运行注入、Agent 授权或生产 Project initializer；原 STOP 保留。

Create 发送必填 `variable_id/name/description/value`；Update 使用当前独立 GET 的版本，省略字段保留，空描述可清空，显式 value 必须非空且始终作为一次替换。材料为 1..65536 UTF8 字节、有效 Unicode scalar、禁止 NUL；空白、TAB、CR、LF 均按原字节保留，不 trim 或规范化。textarea 展示会规范化换行，因此原粘贴文本由私有输入所有者保存，后续编辑只替换发生变化的片段。

安全元数据恰为原八字段。回执只证明历史命令，确认后必须独立 GET/List 回读。Unknown 仅以原 Project/target/command/expected_version 和原 Idempotency-Key Lookup；Lookup 不含 request/value/digest，not_observed 不证明回滚，不自动重放或生成新键。

## 材料与生命周期

材料仅在表单私有内存和一次 HTTP 调用中持有，不写 Session、公开进度、URL、storage、日志、摘要或测试证据。提交捕获后立即清表单与原 DOM；checking、失权、换目标、取消、离页和卸载同样清除。失败/冲突可保留名称与描述，材料须重新输入。HTTP 实际 fetch/body/cancel 未返回前仍占唯一 Session owner，不提前释放。JavaScript 字符串与浏览器内部副本的物理擦除不作保证。

## 写域与验收

七产品域：新 `api/project-secret-commands.ts`、`composables/useProjectSecretEditor.ts`、`components/projects/ProjectSecretEditor.vue`；现有 `api/client.ts`、`composables/useSession.ts`、`composables/useProjectSecrets.ts`、`views/projects/ProjectSecretsView.vue`。专属控验证 presence/多行 UTF8、安全回执、当前权限/检查中退役、实际取消尾、Unknown 原身份和独立当前读。组合 `0661f376` 已合入 main `48fe9a37` 的 Knowledge 删除与 Runner directory；client/Session 双向增量应用所得完整文件均等于自动合并，原唯一 owner 保留。

真实默认 App 两条有限链计划覆盖普通 Owner 的 create→metadata edit→replace，以及已提交响应丢失→原身份 Lookup→当前读；正式 D10/D04/Audit/Outbox 后验由 Go 持有，浏览器证据不保存材料或其摘要。knowledge_delete_fixture 持独立 App Go/PW/profile/method-controls 文件；只有新 shared observer opt-in，旧 profile 保留。原方法 68 项与后增 paste→Session 精确值绑定 5 项分别通过，组合后 Secret/Knowledge 控制分别 66/65 通过；首次 Go 编译、唯一 selector list 与新资产唯一 Session 绑定实际通过。上述方法准备不代实际业务验收。

## 基础验证与已修问题

原件在 `output/ai/project-secrets-write-ui/checks/`。首 build01（含 type）actual0，修前物理资产保留于 `dist-build01/`。主审三个窄缺口已用 review-repro01 实际复现：4 case 中 3 FAIL/1 PASS，分别为同步 submitting 后 abandon 仍发写、重复显示换行删除保错原 CRLF、Create 名称冲突误入不可用的版本重读门。修补仅本片退役 revision、beforeinput 选区映射和 VERSION_CONFLICT 判别，原 FAIL 保留。

unit01 为 6 文件 42 PASS/1 FAIL；三个审查反例修后已过，新增 same-Session checking 恢复控发现数据 ready 但 visible 缓存 false。checking01–03 保留原实际失败；安全状态观测定位为原 reader 先发布 reactive scope、后设置非响应式 route，在新同步 consumer 下提前缓存旧路由。仅调整为 route→scope 发布。page02 受影响新页/既有状态/既有页共 3 文件 20 PASS，actual0；unit01 中其余 23 个 API/Session 控沿未变输入复用，合计 43 项为分段通过，非单次全绿。原取消非 EOF held-body 控仍证明实际 cancel 返回前 Cookie owner 不释放。

组合后 build02 保留真实 type FAIL：新页面测试的 button helper 收到 DOMWrapper，却声明只接 VueWrapper。`e1e09454` 仅把该辅助函数参数收窄为实际使用的 `findAll` 接口，不改产品或断言。修后 build03（含 type）actual0；combined01 的 Secret/Knowledge 四个 API/Session 文件实际展开为 32 PASS，actual0，没有重跑旧 43 项矩阵。新资产 `dist-02/` 共 84 个普通文件，物理复制并逐项核同构建输出，原 `dist-build01/` 保留。原件仍在 checks，实际终态已收。

早期四产品文件的精确恢复差额与原 WIP 未留完整字节的限制见[恢复说明](../../../.agent-state/project-secrets-write-ui/README.md)；原日志和失败不回填。所有材料测试使用受控合成输入；日志仅断言或安全身份状态，不保存真实材料。非完整 D10/撤权矩阵/生产初始化验收。

## 首次真实调用与定位返修

`native01/` 原 outer actual1，supervisor 实际返回 136.33s；原进程 Wait、七资源、私有目录/runtime/descendants、HOST_TCP 双尾与输入不变门全部收束。两个子例均未发生 Create/PATCH/Lookup/cut。normal 停在 spec19 空目录列表的可见性断言；lost 已读出 seed 详情并通过标题，再停在 spec45 的替换材料 `getByLabel(..., exact)` 焦点定位。原件没有失败截图、DOM 快照或 raw locator count，不补造，也不把 lost 误记为详情发布失败。

真实 View/Session 的 `native-dom-repro01` 两项实际 FAIL：两个输入都先满足 activeElement，再因 label 原文分别为 `Secret 名称 *`、`Secret 值 *` 而不等于旧 exact 文本。必填星号在 `aria-hidden` 子节点中；PW 已窄改为 textbox 可访问名称定位，normal 空态检查改为目录存在/零项与可见空文案，焦点、Enter、写入和独立回读门不变。产品和 dist 无修改。

后续两条组件 DOM 流程检查覆盖全部既有表单/按钮/标题/版本及材料清除步骤：native-dom02 为 1 PASS/1 FAIL，创建链最后错误比较旧夹具名称；只修该断言后 native-dom03 的创建链 1 PASS。替换/Unknown 链沿 native-dom02 的 PASS 复用，不写单次全绿。这是受控组件检查，不代下述真实浏览器复验。

## 默认 App/browser 实际结果

native02 的原工具 16188 由方法作者亲收 actual0，supervisor 实际返回 127.185s；两业务子例、全部原 Wait、七资源、私有/runtime/descendants 与 HOST_TCP 双尾、输入不变门齐。normal 为 1 Create、2 Update（metadata-only 后明确 replace）、4 List/3 Get，验证三次当前读；lost 为 1 已提交后实际截断的 PATCH、1 原身份/原 key Lookup、2 List/2 Get，无再次 PATCH。正常链的材料原粘贴字节、Session 实参、原 HTTP 输入和 D04 持久值只在私有内存联证，metadata-only 未换材料；安全回执及 D10/D04/Audit/Outbox 后验按原方法核对。

normal 13 请求为 12 finished + 1 Create 的实际 ERR_ABORTED，13 份原联证齐；lost 9 请求为 8 finished + 1 PATCH 的实际 CONTENT_LENGTH_MISMATCH，9 份原联证齐，不能概称全部请求 finished。两子例各有宽屏浅色/窄屏深色图，截图前原材料 DOM 已清空。build04 含 type actual0，84 资产与原 dist-02 逐字同，复用既有 binary/Session binding；没有为定位返修重编产品或 Go。原件位于 `output/ai/project-secrets-write-ui/native02/`，主审已核 22 份联证、4 张实际截图、26 份持久事实 JSON 与原全部退出门，最终有限接受。

本次实际覆盖 runtime-off 默认 App 配置链，不外推 Secret 删除、运行注入、完整 D10、生产 Project initializer 或既有 STOP。native01 原 spec 的精确小恢复 patch 见本片恢复说明；全部历史失败仍保留。
