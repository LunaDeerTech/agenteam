# D26 core-review-01 独立静审

结论：**BLOCKED，F1–F4 为当前固定核心的必要修复项。** 本轮只做静态检查，没有执行产品 JavaScript、npm、浏览器、Go、Docker 或网络。没有读取活动 UI/路由，也不认定下面的 controller 可达路径已经由某个最终页面按钮暴露。没有发现或声称服务端授权绕过。

固定输入是 `457b1979c9d6563740543b2011eedc06cce34c71` 加作者 `core-review-01` 的 3 个生产、2 个单测和 2 个 package 文件。目录 `/workspace/agenteam-d26-auth-author-jrfhr6h1/evidence/core-review-01`，`manifest.json` SHA-256 `d58f18be1558b9e1493a40ec848070a32af92ed05037fb1c734aeff76b5aafbc`。规格采用 `64e47fbc180a1fb2da385e7d4fabcc880699b0d5` 中 rev1.2，卡 SHA `76745915d15c24dcd86ba061b0ff7bfada440d0a251f23f026166b971838edcd`。作者在本轮期间获准修复；本文始终只读旧固定副本，不把新活动改动混入结论。

## 必要修复

| 项 | 固定位置与可达输入 | 结果及最小修复/原红验证 |
| --- | --- | --- |
| F1：挑战关闭改写 Unknown 原输入 | `web/src/composables/useSession.ts:524–526` 的 `dismissChallenge()` 无 `owner/pending` 门禁，调用 `clearChallenge():98–105` 删除 `intent.pass`。公开动作序列为 `CHALLENGE_REQUIRED → createChallenge → verifyChallenge` 得到 pass，再以相同 key 登录并得到 `COMMIT_UNKNOWN`，接着 `dismissChallenge → retryOriginal`。 | 第二次 `performLogin():323–330` 使用同 key，却没有第一次的 `challenge_pass`。原 login 未返回时调用关闭，也会改变保存的重试意图。违反卡 §4 的完整原输入冻结。应防止关闭动作在实际请求/Unknown 期间改写该快照，或把不可变重试 body 与挑战展示状态分离；不替作者实现。原红应比较第一次与重试的完整 email/password/pass/token/key，而不只比较 key。当前测试只分别覆盖“带 pass 成功”和“无 pass Unknown”，未覆盖交集。 |
| F2：非法 commit_state 被强转接受，Unknown 分类失真 | `web/src/api/client.ts:83` 使用 `includes(String(p.commit_state))`，`:94` 却保留原值。合法 JSON `commit_state:["unknown"]` 会通过此条件；其余字段可以是正常的 `503 / DEPENDENCY_UNAVAILABLE` Problem，并匹配 X-Request-ID。 | `useSession.ts:65–69` 以严格字符串比较，首次错误不会被视为 Unknown，最后走 `:204–206` 的 `forgetIntent()/anonymous`，而畸形 mutation 响应本应 `invalid-response → uncertain` 并保留原 key/body。先验证原值确为字符串，再检查闭集；数组、对象、null、数字都必须拒绝。至少一个原红须贯穿真实 `createAccountAPI + controller`，不能仅 mock 已合法的 `AccountFailure`。这不是服务端会正常产生数组的声明，而是本卡明确要求的畸形响应防线。 |
| F3：错误/取消响应 body 尾部未纳入真实请求责任 | `client.ts:165–172` 拿到 Response 后在已 abort、重定向或错误 Content-Type 分支直接抛出，没有取消其 body；`readJSON():134–136` 调用 `void reader.cancel()` 后立即释放锁和返回。 | 已取得的 body 或其异步底层 cancel 尚未结束，`run():244–250` 已可清 owner/busy，允许下一 Cookie 请求。违反卡 §3 “观察其 transport 结束”及已冻结独立计划的实际 body/transport 尾部要求。这里**不声称** body 能在 headers 后再次写 Cookie：真实浏览器通常已在 headers 阶段处理 Set-Cookie；确定问题是丢失已有 body 清理责任。最小纯原红使用真实 `createAccountAPI + controller`，Response 为错误 Content-Type 的 ReadableStream，底层 `cancel()` 返回 deferred；应实际请求 cancel，未返回前 owner/busy 不能清、后继 restore/bootstrap/logout 零进入。另以单块 >600000 bytes 命中已读 body 的失败分支，证明不是只补早退而仍 fire-and-forget。30s 可结束页面等待，但不能当作底层清理已完成。204 沿 HTTP 无 body 规则，不要求解析 JSON。 |
| F4：challenge/verify 只有 abort，没有统一 30s 等待上界 | `useSession.ts:424/469` 的定时器只执行 abort，`:426/471` 的 await 仍依赖请求配合取消；这两动作没有 Cookie `run()` 的逻辑等待 race。 | 若 transport/body 忽略 abort，动作 Promise 和 `challengeBusy` 可无限挂起，页面无法在卡 §3 的统一 30s 上界收到未确认反馈。非 Cookie 请求允许逻辑取消，但不能因此发布 pass/旧题或误清新一代 busy。最小纯原红：真实 API 的 create/verify body 读取挂住且不响应 signal，推进 fake clock 到 30000；观察动作有界结束、未确认反馈及逻辑题目作废，再启动新题，释放旧 body，确认旧成功/错误/finally 均不污染。仍保留实际未结束请求所需的私有引用至其真实尾部；不增加自动重试或恢复旧题。 |

四项均是静态确定的代码路径，**本轮没有动态复现原红**。作者原红与修复应另固定并交差量复审。F3/F4 的 fixture 可放在已授权的两个 unit 文件，不需要新路径、浏览器或后端旁路，也不改变原预算。

## 其余已核边界

- 六个 typed API 使用精确相对路径、方法/成功状态、`credentials: same-origin`、`cache: no-store`、`redirect: error`；匿名/Session CSRF 显式传参，204 不解析 JSON。完整 required/extra-key shape、UUIDv7、int64 字符串、不做数值 version 转换、CSRF43/pass80原样传递、题图 PNG data URI 与合计 262144 上界均有直接实现。此结论不宣称所有边界输入已动态测试，F2 明确保留。
- 四个 Cookie 入口实际同由 `owner` 单飞；`run` 的 30s race 不会仅因逻辑过期清 owner；`leave` 不主动 abort/释放该 owner。结果检查 generation/expired，最后清理检查 owner 身份。原 `AccountAPI` Promise 不返回时的逻辑串行设计成立；F3 是其下层 body 责任缺口，不能用现有 mock 证明闭合。
- login200 不直接发布认证状态，而是保留期望 user/session，再实际 GET Session，比较两项 ID 并取得 Session CSRF。失配分支清身份并显示变化；后续再查仍保留期望身份。当前两个单测没有直接覆盖失配分支，最终验收应补或复用有效作者新增证据。
- 普通 Unknown/transport/invalid-response 路径保留 intent，401 查询不会偷偷 bootstrap 后用旧 key 再写；logout 未知保留原 Session/CSRF/key，原上下文失效后不允许重试。F1/F2 是这项意图保存的确定例外。新 key 依赖 `crypto.randomUUID()`，无低熵降级。
- public readonly state 不含 password/pass/CSRF/key/retry body。输入没有 trim/normalize，实际 login body 由私有 intent 生成；离开/明确放弃会清意图与认证引用，旧实际调用保留其必要局部快照。没有在本轮核心中发现 storage、URL/history、日志或底层 exception 携带敏感请求的写入。页面密码字段释放和真实导航仍待冻结 UI；JS 字符串物理擦除不在此声明中。

## 锁差量及作者证据

独立按 JSON 逐项比较固定 `457b197`：唯一新包为 `go-captcha-vue@2.0.7`，与旧 `tests/account-captcha-web/package-lock.json` 中该完整包条目相同；170 个既有非 root lock 条目逐值不变。仅 package root dependency 增加此包；Node engines、scripts、其余声明、lock 顶层和旧包都没有升级/删除。`lock-delta.json` 保留前后实际值。没有执行安装、cache/integrity 网络恢复或浏览器兼容验证。

manifest 所列 7 候选与 8 作者日志/metadata 均独立 SHA 匹配。原日志证明作者两轮 `npm run type-check` exit0；`core-format-01` 是限定路径 **Prettier --write** exit0，不改称完整 `format:check`；`core-unit-01` 为两个文件 **18 tests PASS**、exit0、Vitest 4.1.11、837ms。这18例是作者纯测试，不是我的动态执行，更不是真实 Browser/HTTP/PG/Cookie 验收。它们覆盖 typed wire 11 例和 coordinator 7 例；late Cookie/timeout 场景使用 AccountAPI deferred，未贯穿下层 fetch/Response body。原 metadata 中没有的 env 明细不补造。

## 实际检查、限制与交接

本轮命令为 `cat/nl/sed/rg` 定位冻结卡/源码/技能，`git show/git grep` 只读取上述固定提交，及 Python 标准库进行 SHA、JSON/锁差量检查。最终可复跑检查为：

```sh
python3 /tmp/agenteam-d26-core-review01-v-7uhn1hok/verify_inputs.py
```

实际 exit0：7 candidate files、8 author logs、11 git materials、170 existing lock entries unchanged。`checks.json` 保存每个输入 SHA/字节数及精确 Git argv/cwd/exit；`check-command.json` 保存该脚本实际执行结果。未读作者活动 UI/路由/修复源；未写 repository、业务、既有归档或作者输入；无 Git 写操作。源文件七项末检仍同原 manifest。

本次只冻结此私有报告和轻量检查，不接受核心或完整 D26。后续只审冻结修复 delta/原红与修后证据，再按既定短计划补必要独立纯例和正式同源 dist 浏览器组；独立运行权限另授。完整 D26、D28 托管、真实服务端 COMMIT Unknown、Provider 调用及已知 Object/Artifact 阻断不在本结论内。
