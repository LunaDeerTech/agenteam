# D26 core-review02 窄复审

**STATIC PASS：review01 的 F1–F4 均已在此固定差量消除，未发现新增必要阻断；建议采纳该核心修复继续后续实现/验收。** 本轮未运行产品 JavaScript、npm、浏览器、Go、Docker 或网络。作者 29 个 pure tests 的通过是复用固定原件，不是我的动态验收，也不代表正式浏览器或完整 D26 已通过。

固定目录 `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/core-review-02`；manifest SHA-256 `0bd7611e421549ef4d13a148b50e04359b3b53845fabfb6a6b597fdddf74eece`，delta SHA `fc70afbc15d26d68ce4c42c6fb9ad1e1157356fc54b84d26f6a9fdb5d33e0c31`，仍为基线 `457b1979c9d6563740543b2011eedc06cce34c71`。旧独立报告 `/tmp/agenteam-d26-core-review01-v-7uhn1hok/review.md`（SHA `b423a36a006cbbb5741ff9e1b66613f44601077757cb8dffa85b77c9debbc8d8`）保持原字节；其余未变静审结论复用。

| 项 | 修复与原反例核对 |
| --- | --- |
| F1 | `dismissChallenge()` 新增 `owner || pending` 门禁；正在请求和 Unknown 后两条原反例均保持完整 password/pass/token/key 对比，原 2 FAIL 保留。只是收紧挑战展示动作，不阻止明确 leave/restart 放弃旧意图。 |
| F2 | 原值先 `typeof === 'string'` 再检查 commit_state 闭集。数组 `['unknown']` 的客户端原红及贯穿真实 `createAccountAPI + controller` 的原红都保留；最终覆盖数组、对象、null、数字拒绝，整链仍保留原 body/token/key，未用已合法 mock 代替 malformed JSON。 |
| F3 | Response 所有取得后的出口由外层 finally 实际 await body.cancel；readJSON 保存唯一 reader.cancel Promise，abort listener 只启动取消，finally await 相同 Promise 后才释放 reader lock。不会用 30s 页面等待结束代替 body 清理终局。原 content-type（cancel=0）与 oversize（busy 过早 false）两红保留；最终相同 deferred cancel 反例要求取消被调用、清理未返回前 Cookie owner/busy 保持、后继 restore/logout 不发请求、30000ms 用户等待已返回但 busy 仍 true、释放 cancel 后才清 busy。这里只证明抽象 body/transport 责任，不宣称 body 可在 headers 后再次写 Cookie。 |
| F4 | `runChallenge` 对 create/verify 统一 30000ms 逻辑等待；超时清当前题/代际并 abort，实际 API/body Promise 独立保存在私有 challengeTails，到真实 finally 才移除。结果、错误及 finally 均检查 generation/revision/原 intent/abort，旧尾部不改新题或 pass/busy。原 create/verify “30000ms returned=false” 两红保留；最终真实 API/ReadableStream 例保持原上界，并在旧 cancel 未返回时完成新题与新 verify，释放旧尾部后仍保持新状态。 |

`account.ts` 和两 package 文件与 review01 SHA 逐项相同；本轮没有锁、依赖、Node/scripts 或 DTO API 扩张。实际 diff 仅 client.ts、useSession.ts、两 unit；从固定前后文本独立重建的统一 diff 与作者 delta **字节相同**。F1/F2 原红的三生产源与 core01 逐字节相同；F3/F4 原红生产仅先加入 F1/F2 两处修复，未提前加入 body/timeout 修复。两组测试的原反例断言在最终输入中保留，格式化不改变其值。

作者证据链已逐 SHA 核对：

- F1/F2 首轮：exit1，4 failed / 3 passed / 18 skipped；局部修后两文件 25 PASS、exit0。F3/F4 首轮：exit1，4 failed / 10 skipped。这些是有意 selector 下的原红，不改写成整组失败/通过。
- `core02-unit-01` 仍保留实际 1 FAIL / 28 PASS。新 helper 把实际请求放入微任务，旧 late 测试立即开始第二题会在第一 API 进入前将其取消；此前两个 deferred 就不再各对应一个真实 API 调用。最终测试增加第一/第二 create API entry barrier 及 verify entry barrier，**原 busy、旧题不发布、旧 pass 不发布断言均未删除或降低**。该首失败没有在本轮动态重跑；归因基于固定代码、原日志和精确测试 delta，不冒称取得新的运行证据。
- 最终 `core02-unit-02`：两文件 29 tests PASS，667ms，exit0；type-check exit0；本轮实际为限定七路径 **Prettier --check** exit0。原日志与 argv/cwd/exit 由 manifest 定位，缺少的环境明细不补造。真实浏览器、Cookie jar、UI/路由组合仍未验。

实际只读检查命令：

```sh
python3 /tmp/agenteam-d26-core-review02-v-1pwnsdm7/verify_inputs.py
```

exit0：41 个固定 payload SHA 匹配，4 路径精确 delta，account/package 未变。`checks.json` 记录输入；`test-token-delta.json` 辅助对比（保留字符串，非 JS 执行）；`check-command.json` 保存实际命令结果。其余读取只用 Python、cat 和固定文本 diff。未读取活动 UI/修复源，未写仓库/业务/旧归档/作者原件，无 Git 写操作。

后续按原短计划审冻结完整候选，独立动态重点仍为迟到 Cookie/body 与 Unknown 输入、真实 Session/CSRF、同源正式构建和挑战消费；必要证据按风险去重。不能把本次 STATIC PASS 扩为正式认证页面、完整 D26、D28 部署或上游 Object/Artifact 缺陷关闭。报告冻结后 all-stop。
