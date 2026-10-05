# D26 首轮真实浏览器 selector 前置失败定位

**确定存在测试 selector 不匹配，最小修订只需 e2e spec；没有依据改共享 UiField、生产认证或增加等待预算。** 本轮仅静读固定输入、原 raw 和既有锁定 Playwright 算法，未再启动 npm、浏览器、Go、Docker 或网络。

输入为 fixture02 manifest `75e43c7ddf26b9005c0ea0cde48a37e085221bd15a78f461315cd931f733743b`（基线 `457b1979c9d6563740543b2011eedc06cce34c71`）及原日志 `/workspace/agenteam-d26-auth-author-jrfhr6h1/logs/auth-lifecycle-rotate-01.log`，SHA `99fa0fa02f82441744c369f1205eaa9f7aef44260b278f1879a042a9b7a845e7`。原三 browser case 都通过登录标题 focus 后，在 `openLogin:29` 的 `getByLabel('邮箱', {exact:true}).toBeEditable()` 等待 5000ms，最终 element(s) not found。SessionLifecycle 和 RotateChallenge/desktop、/keyboard 均失败，account package 32.384s；尚未填表、登录或 verify，不计其后的场景覆盖。其他包 no-tests 不计通过行为。

固定 `UiField.vue` 的 label 为 `{{ label }}<span v-if="required" aria-hidden="true"> *</span>`。LoginView 的邮箱、密码均 required，因此关联标签的 DOM 文本分别是“邮箱 *”“密码 *”。`UiInput.vue` 将 slot 给定的 id 经 `$attrs` 透传原生 input，未发现 for/id 关联缺失。

锁定 Playwright 1.56.1 的实际实现路径为：

1. `getByLabelSelector` 构造 `internal:label`。
2. `_createInternalLabelEngine` 调 `getElementLabels`，后者读取原生 `element.labels` 并对 label 调 `elementText`。
3. `elementText` 递归拼接文本；`shouldSkipForTextMatching` 只跳 SCRIPT/NOSCRIPT/STYLE/head，不排除 aria-hidden span。
4. exact 的内部 matcher 比较 `elementText.normalized === selector`，所以“邮箱 *”不等于“邮箱”，密码同理。

真正 accessible-name 算法另走 `getTextAlternativeInternal`，会过滤 aria-hidden 子树。不能把 `getByLabel` 的全文标签匹配等同于无障碍名称匹配。固定源的逐字节小摘录、完整来源 SHA 与字符偏移见 `pw-algorithm-excerpts.json`，未执行该 SDK 来制造结果。

因此当前 selector 即使表单正常出现也不能命中，是确定的测试前置缺陷，与原失败相符。原运行没有留下本轮可读的 DOM 状态快照，所以不据此排除修 selector 后可能出现的其他产品/前置问题，也不把本次静读冒称真实页面复现。

建议邮箱使用 `getByRole('textbox', {name:'邮箱', exact:true})`；密码没有可假定的 textbox 隐式角色，可用现固定 `#login-password` 并额外 `toHaveAccessibleName('密码')`。全部邮箱/密码入口、值为空、控件不存在的断言须一致修订，保留 editable/focus/原材料/真实响应断言；特别是旧错误 selector 的 `toHaveCount(0)` 可能因始终空匹配而产生假阳性。不使用含糊 substring、延长超时或绕过正式页面来消除原红。

本轮已核 21 固定候选和 16 条固定 pure 日志/metadata 的 SHA；另读固定 Git 的两个共享控件、challenge 合同实现及五个依赖声明。fixture snapshot 不包含声明的全部 dependency 文件；一次静态定位尝试读其 go.mod 报不存在后，改从原固定 Git 对照五条声明。dist 三项仅保留 manifest 指纹，本轮未读活动构建树或重做编译。该定位失误记在 input-collection-note.json，不是产品测试失败。

主线程已采纳以上范围并授权作者仅 e2e locator 窄修。等待新固定 delta 后只读复审，再由原真实组复验；独立增量链计划另留，不在本轮执行。报告及原件冻结后不改写。
