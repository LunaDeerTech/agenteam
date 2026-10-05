# D26 UI04 拖动焦点窄复审

**STATIC PASS：当前题真实拖动起点的焦点归属已补齐，未发现新增必要阻断，可按原 RotateChallenge 顶层进行真实复验。** 本轮只读固定差量和原件，没有运行 npm、浏览器、Go、Docker 或网络。desktop 原红报告及 UI03 的历史结论保持原字节；本次通过不代替实际鼠标拖动及失败焦点浏览器验证。

固定 UI04 manifest SHA `f64458bc7fba6ffb95ff9a6ab3b5e4bd750c6aba268e04d587de878e105e9bb3`，目录 `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/ui-review-04`；delta SHA `99e37c1359eafbddaf532c76fa7abe49c0ead10c28a52d33faecb35b014558a6`。完整 input04 manifest SHA `918ea0883bb77fab0beedcc48cfbbeaa4d38a32230525bdf2fb92090f5af0bdd`，仍为基线 `457b197`。

唯一生产变化为 LoginView 的 document pointerdown capture。事件目标必须位于当前真实挑战 panel，且当前存在题、无 Cookie/challenge busy、primary/button0、目标属于实际 `.gc-drag-block`，才记录当前题 ID；它先于浏览器默认焦点动作执行，不强制 focus，也不 preventDefault 或改 SDK。外部 pointer 立即取消原归属/待交接，现有外部 focusin 取消、新题/卸载清理保留。此前 pre-flush 到 busy 时即使焦点已是 body，也不会抹掉该题 pointer 建立的归属；清题时仍需匹配原题 ID，再经过 body-only、nextTick 同一交接对象、无新题/无 busy 的原检查。卸载正确移除同一个 capture 监听，不引入核心/API/身份权限变化。

失败链按原件区分：

- 原真实 `auth-lifecycle-rotate-02` 仍是 lifecycle/keyboard 通过、desktop verify200/pass80 后登录按钮 inactive；未记录该次 desktop 的 activeElement 时序。此前基于官方非 focusable 拖柄的静态归因不被倒写成原日志直接观测。
- `ui-drag-focus-red-01` exit1，5 FAIL / 14 skipped，全部是 VTU 对只读 MouseEvent.button 赋值的构造前置异常，未到产品焦点断言。
- 仅改事件构造后，`ui-drag-focus-red-02` 仍使用 UI03 生产，实际 3 FAIL / 2 PASS / 14 skipped：成功、拒绝、30000ms 超时的目标焦点红，外部 focus/pointer 两个控制绿。测试确实在已挂载官方拖柄上 dispatch primary 事件，再显式模拟 body 落点、经官方 confirm callback 和真实 controller；这仍是 pure，不是新的浏览器鼠标观测。
- 修后 `ui04-unit-01` 两文件22 PASS、1.54s，实际命令 exit0。**red02 的 unit 源码与最终 unit 字节完全相同**（SHA `90c9ef3fe2df6029fcc52ba8e72e78b8f686ea23b14896d0db31cc8ea3916e0d`），所以没有通过减少断言消除三红。原30s、晚到成功不得发布、实际 busy/disabled、外部交互后不夺焦均保留。

作者 `npm run format:check` 与 `npm run build` 实际 exit0；build 原 raw 明确执行 `vue-tsc --noEmit` 后 Vite production build。只复用原 argv/cwd/env/exit（metadata env={} 不等于记录过进程完整环境），没有另跑检查。

本轮实际 Python SHA/文本核对：两源精确 delta 与独立生成的 unified diff 字节相同；input04 的21项中恰 LoginView 与原 authentication unit 两项变更，其余19项含 core3、官方 wrapper、e2e/Go/config 原断言/预算未改。五项非 dist 依赖声明相同；新 dist 三项由固定 manifest 与 build 原件定位，本轮未读取活动 dist。两组原红 manifest/源码、12条 UI04 logs/metadata、保留的真实 red 均核 SHA，详见 checks.json 与 red01-to-red02.patch。

建议执行者保持原顶层2分钟、PW45秒、workers1/retries0、原 driver race/count1/6m，先完整 RotateChallenge（desktop+keyboard），绿且实际清零后再此前未跑的撤销/期限/布局组。不得把本 STATIC PASS 写成首轮全绿或完整 D26 已验。报告和私有证据冻结后 all-stop，无资源占用。
