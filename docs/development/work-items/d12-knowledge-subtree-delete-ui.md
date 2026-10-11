# D12 Knowledge 子树删除 UI

状态：第三轮真实浏览器原 whole PASS，唯一主审已有限接受；基础检查分段通过，前两轮 whole FAIL 全部保留。原基线 a15d8bbd，已在19df892c组合 main 7c884cb5。实现者 skills_milestone_review；root 管理 Git 与运行窗口，独立审查由未参与实现者承担。

依据 [Owner 树命令 HTTP](d12-knowledge-owner-tree-http.md) §§2–4。普通 Owner 从当前文档取得完整子树预览，明确确认后使用该预览的原令牌删除；无 expected_version，不把预览或历史回执当当前授权。预览 POST `{}` 带当前 Session/CSRF，不带幂等键；删除与完整意图 Lookup 保持原 key、root 与 confirmation_token，Unknown 不自动重发或刷新令牌。当前归档只读，原 Lookup 仍可查询。

前端以完整响应校验后发布预览，验证安全 Document、同 Project、完整无环根连通拓扑及规范 scope digest；令牌不进入 DOM、持久存储或公开诊断。收到确认后独立重读：root metadata 为 HTTP 200 安全 deleted tombstone，content/ancestors 的删除状态为 410，当前 children 移除子树且保留兄弟文档。cleanup_pending 仅表示可靠清理仍待处理，不表示 Object 已物理回收。

首链限定两场景：正常预览→明确删除→当前墓碑/目录；另一次真实提交后物理截断回执→原完整意图同 key Lookup→当前墓碑/目录。复用同包真实默认 App/TLS、正式 Account Owner 与正文创建种子；生产 initializer、Source/Jina、runtime/Execution 和 Object STOP 均不解锁。此前 Move 未正式交付，原 whole FAIL 不变。

实现域为独立 codec、Session 原意图、删除确认 controller/dialog，以及既有 Knowledge 页面/读取 owner；共用唯一 transport/runAuthorized。App/router 如无必要不改。Go 夹具独占 `internal/central/app/knowledge_delete_web_test.go`，因复用的真实 App 测试入口均为同包私有；不新建替代服务。

当前完成限定 Web 基础检查，Go 夹具编译/list实际通过，前两轮真实浏览器 FAIL、第三轮原 whole PASS 均已记录。测试与实际资源/原始失败按下列原件分段记录。

## 基础验证进展

源码与页面首完整片已固定 c0c39a56。`output/ai/knowledge-subtree-delete-ui/checks/` 保留真实原件：publication-repro02 两个同步 progress watcher 退出反例 FAIL，随后仅增加两个发布后的 live 复查；unit01 共24控中22通过，另两拒绝控制因测试 Problem 缺原 X-Request-ID 未触达拒绝分支。补真实RID头后 denied02 两断言通过但 Vue 重复清理递归报错，增加当前拒绝清理幂等门后 denied03 两控实际0。build01 只因 receipt true 分支 TypeScript 返回类型失败，精确重载声明后 build02 实际0（含type/Vite，24.351s）；没有将分段结果说成单轮全绿。

受影响的原 rename/text/标题选择31控中27通过，4个旧页面控制在 a15 原产品的限定源覆盖下再次同样失败（baseline-legacy01，原38623/595267实际1）。原因是旧测试定位命中先出现的标题查找输入；仅改原表单作用域，不改业务断言。stale-repro01 证明合法 CONFIRMATION_STALE/not_committed 被归为 Unknown，现只在Delete已知拒绝闭集加入该code，仍保此前Unknown/Lookup失败的原意图。targeted-final01 原75694/595824实际0，8.196s，修复后stale1与旧页面4项共5PASS，旧27PASS复用；不称全矩阵单轮通过。组合后build-final01因旧text选择器收窄失去元素泛型推导而实际2，补HTMLLabelElement类型标注后的build-final02原34022/598637实际0、23.800s（含type/Vite）；fresh dist-01物理资产与web/dist逐项一致。首次真实浏览器结果见下。

preview-binding-repro01 为探索性客户端解码期待FAIL。唯一主审依据正式边界裁定 token 继续不透明：服务端 Validate 负责 signed claims 与当前范围绑定，UI保持原token、完整预览与Session expectedIDs，异常回执不能confirmed。该自加解码期待已撤出正式suite，原FAIL与原探索case保留，不列产品缺陷或联调门。

方法组合控制首轮为64PASS/1FAIL，证明新Delete profile未知字段尚未拒绝；仅该分支增加允许key闭集后，controls-only原598000实际0、63PASS。原normal/lost两次精确Playwright发现列表均实际0（仅发现，不计行为通过），未重跑。原件见`output/ai/knowledge-delete-ui/method-controls-*`；旧System/Secret及全部harness门保留。

## 首次真实链（FAIL 保留）

固定6e0f90b1的native01沿原真实默认App/TLS与完整退出方法执行。原tool41483实际1；supervisor605047实际wait1/136.361s，Go两sub35.98s均FAIL。normal11原请求全部finished；lost12中11finished、1原物理截断删除回执，随后原完整意图同key Lookup1。两场景均到独立当前墓碑/目录验证，安全证据current_verified/all_observed为真；尚未走完关闭后兄弟选择，不能按整场业务通过记录。

停止点是spec:10:24的关闭按钮toBeEnabled。原spec未区分顶部X与footer同名“关闭”，源码及实际预览图支持定位歧义；安全原件无raw错误/count，不补造。最小返修仅限定专属spec的dialog-footer并断言唯一，原focus/Enter及全部业务断言保持，产品/Go/binary/dist不变。

原2Node/Go/driver失败终态保留，8Chrome后代实际0；七资源两次absence、private/runtime/desc双空、原HOST_TCP自动baseline差值连续双空及输入不变均齐。原wholeFAIL不以资源已清理改写。证据`output/ai/knowledge-delete-ui/native01/`及`native01-evidence/TestKnowledgeDeleteWeb/`，原PG日志`/tmp/kdw01/ui-0af01d9d6bed4b2e.log`。正常与丢回执场景尚需受影响复验；不增加Object物理回收、initializer或运行能力结论。

## 第二轮真实链（FAIL 保留）

native02 使用 d35143a3 的footer定位修复及同原Go binary/84资产/Session绑定。原tool42120实际1、supervisor613412 Wait1/140.067s，Go615141两sub40.77s均FAIL，Node615431/615881及driver613434均1；8Chrome原Wait0、七资源14absence、private/runtime/desc双空、原HOST_TCP delta双空和输入不变全部收齐，wholeFAIL不变且窗口已释放。原件 `output/ai/knowledge-delete-ui/native02/`、`native02-evidence/` 和 `/tmp/kdw02/ui-f6fc52654d934332.log`。

normal11原请求全finished、all_observed/current_verified真；关闭与焦点已过，停止于spec76的exact文本定位。实际UiState将aria-hidden“!”与标题放在同一h3，原查询不符合可访问heading语义；限定修复仅使用文档区内的标题角色与名称，业务断言不弱化。lost10请求中8finished，真实删除回执cut1有完整已提交联证；随后唯一原Lookup返回502/ERR_ABORTED且无RID/Go proof，尚未确认当前墓碑，all_observed/current_verified假。不能将它归于normal定位失败或冒称本轮Lookup成功。

原统一Go observer失败出口未保存Lookup502的具体内部stage，现仅加首次失败固定stage码即时日志（不输出原error、token/key/body或SQL），不改变原拒绝与预算；这是补观测，尚非已证根因修复。单个既有Session/App用例追加剩余DOM交互：remaining-dom01因新控制省略浏览器原focus动作而实际1，补该动作后remaining-dom02原83254/621483实际0/4.300s，提示、footer关闭焦点、drawer Escape返回、兄弟键盘选择和正文全部到达；只运行该1项，不扩业务矩阵。原失败与该次源码保留。产品/资产未变，第三次真实运行仍待必要准备及root窗口，不自行重试。

## 第三轮真实链（有限 whole PASS，独立接受）

固定2dcd2ef9纳入已交Directory与安全stage诊断；compile-app02原77521实际0，Go624421 Wait0/94.925s、1716输入前后相同，唯一list6259050/1.053s。新binary2f98e5d2…5fb592a6沿原2551运行输入、同84资产与Session绑定进入native03，没有重build或改变原门。

原tool87921 actual0，supervisor626692 Wait0/132.049s；Go628361两sub38.83s（normal17.16s、lost21.67s）通过，Node628541/628904、driver626693及8个Chrome后代原Wait均0。七资源14absence、private/runtime/desc双空、HOST_TCP自动baseline差值双空、输入不变/terminal0全部收齐，窗口释放。normal14请求12finished，children GET与preview POST各自ERR_ABORTED仍按原终态保留，完整native/public consumer/同RID Go证据成立；lost15请求14finished，唯一delete回执原CONTENT_LENGTH_MISMATCH物理cut，随后同key/full-intent Lookup1及独立当前读取完成，无第二次删除。29份联证与7张浅/深色桌面/窄屏图覆盖预览、Unknown、删除后当前树、旁支正文；关闭、Escape回焦及旁支键盘选择均实际到达。

本轮未再现native02 Lookup502，未触发新增安全stage；02原失败缺具体内部阶段的限制仍在，不宣称已找到或修复其根因。01/02 wholeFAIL及早期受控失败原样保留。这里只接受普通Owner这两场有限删除/恢复与当前读取链，不扩大到全权限/撤权竞争/令牌到期浏览器矩阵、Object物理回收、生产initializer或Runtime；Move原wholeFAIL仍未交付。原件 `output/ai/knowledge-delete-ui/native03/`、`native03-evidence/TestKnowledgeDeleteWeb/` 与 `/tmp/kdw03/ui-1eac6d76eacf4fc9.log`。唯一主审已有限接受。
