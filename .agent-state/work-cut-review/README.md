# Work 四条预声明截断方法独立复核

2026-10-09，Skills。只接受 Work 固定 `152eb964` 相对 `ae101b00` 的四条预声明截断观察方法；未修改 Work 产品或 helper。普通 native 诊断当时仍 WIP，不在本结论内。原 recovery05 `63642` 整体 FAIL 不变，不是新真实 UI 或整包验收。

依据 D11 Owner planning UI 卡 §8.2 与先前独立方法核定：固定 Playwright 1.56.1 的 `_onRequestFailed` 不结束 `Response._finishedPromise`，四条故意截断流本来不可能取得完整 EOF。因此仅 `unforwarded-milestone-update`、`lost-milestone-update`、`lost-task-update`、`lost-blocker-add` 不创建无必达的 finished 操作，以同一原 Request 在 page-close 前的真实失败事件结束本地观察。该事件必须联合唯一产品 owner 实际尾后可用的公开 Lookup、Go 原 Write/Flush/Hijack/Close、零转发或真实 completed SQL、精确原历史/唯一事实与资源收尾，不能独自宣告业务通过。普通五条 aborted 的原证据不足不变；held-read 原方法不扩大。

静核固定 helper diff：原预声明、精确材料与 Request identity、cap4、503 固定 headers 保留；新增成功事件反证，failed 后成功也累计错误；page-close 结束本地事件等待但拒绝验收，晚 failed 不能补为成功。普通响应仍唯一原 finished，held-read 仍原 finished/取消路径及晚拒绝门槛。originalBody/schema/decoder、Go 注入/SQL/Lookup/实际 Wait/资源预算未改。

离线 `53260` **actual exit 0，63 控制，0 unhandled**。`controls.cjs` 从固定 Git 版本读取实际 helper 和作者已有 57 控制，追加本实例六个控制：四种截断各在失败尾已完成后注入成功事件，仍必须拒绝、finished 调用为 0；未声明普通失败即使 finished 返回 null 仍拒绝且只调用 1 次；held-read 实际授权取消后，原 finished 晚拒绝仍失败且只调用 1 次。实际 PW 类方法控制同时验证固定 1.56.1 的 failure/finished 语义。所有声明、transport 事件、内存 evidence 和 schema 外部调用是控制输入；不证明真实网络/客户端发布/SQL，不启动 PG、浏览器或 socket。

在 `/workspace/agenteam-skills` 执行：

```sh
node .agent-state/work-cut-review/controls.cjs
```

探针对 Work 树只读，固定源码仅在内存加载，结果写入 Skills 忽略目录 `output/ai/skills/work-cut-review/independent-controls.json`。原作者 `69562` 57 控制、`29474` strict TS 的范围仍保留；此结论不使普通 native WIP 获得接受。待作者冻结 native 诊断接线后，另核其仅观察、不改变普通 finished/消费/发布门槛及实际观察尾。

## 后续 native 整包独审

2026-10-09，对固定 Work `d3322e3e` 相对 `152eb964` 的五个技术路径有限接受，无 mustfix：`tests/account-captcha-web/e2e/project-work-planning.{helpers,spec,native,publication}.ts` 与 `.agent-state/work-owner-planning-ui/native-diagnostic-controls.cjs`。依据作者冻结的 `recovery-native-diagnostic-proposal.md`、D11 卡 §8.2 和上述四截断方法；未修改 Work 源码。此处是诊断实现独审，不是实际浏览器消费／发布／Go／SQL 或整包 PASS，原 recovery05 整体 FAIL 不变。

静核原 fetch、Response、body、reader 与公开 facade 原 Promise；只观察原操作，不额外 read/cancel/clone/tee 或业务 HTTP。native 与原 PW Request 必须由固定 method/path/status 和全局唯一 XID 绑定，public call 还需同 document、当前 identity、单一候选及 Node 侧二次唯一校验。缺失／冲突保持 unbound。初始 auto-read 可能先于 public installer，因此不补造公开调用证据。DOM 仅记录入口状态及 fulfillment 后观察时点，不是发布因果证明。闭集投影、数量／字节上界、私密原值不持久化和单 flight 保持。

`dd8eb0` actual exit 0 的 TypeScript AST 检查确认：`originalBody`、`decodeOriginal`、`schemaProgram` 与方法基线相同；spec 内全部 198 个 `expect`、`ipc`、`complete`、`seen.verify` 调用及顺序相同。普通 finished／同原 body／schema／client 门槛未改，没有 fallback。导航原 `go()` 使用 `page.goto` 后 `ready`，新增 flush 是实际 document 边界观察。

必要探针 `native-controls.cjs` 在读取实际冻结文件前以本地 `git diff --exit-code d3322e3e` 验证 native/publication/作者 controls 三个输入；两个浏览器 installer 仍经作者实际锁定 Playwright 1.56.1 transform 后函数序列化再进 VM，Node sampler 从实际 TS 提取。完整命令：

```sh
# cwd: /workspace/agenteam-skills
node .agent-state/work-cut-review/native-controls.cjs
```

最终 `16351`／`280b89` **actual exit 0，41 控制，0 unhandled**（作者 33 ＋本人 8）。独立增量验证：

- 原 reader.cancel 与 stream.cancel 各保持同一个原 Promise、一次调用、同一 rejection error；取消不变 EOF，原私密错误不持久化。
- 同步 read throw 保留原异常、无虚构 settle/EOF；退休后捕获的 cancel wrapper 仍调用原操作，不能升级已结束快照。
- end evaluate 拒绝可以表示该观察 Promise 已返回，但不成为 end snapshot。固定 PW 实际 `Page.prototype.evaluate` 是 async，内层同步 throw 变成同一错误的 Promise rejection；没有为不存在的同步 PW API 改造入口。
- finish 已置 stopped、实际 end evaluate 仍 held 且原 listeners 尚在时，同原 Request 的 late response/failed/finished 以及新 Request 均不能改 XID、事件或请求集合；最终保持 unbound、CL-EOF false，移除 listeners 后晚事件也不改保存文件。
- 使用真实 Node `ReadableStreamDefaultReader`（read 为继承方法），`Object.preventExtensions` 后经实际 installer 的原 getReader 仍只调用一次、返回同 reader，原 read 方法和两次数据／EOF 返回正常。defineProperty 被 safe 边界拦住，诊断明确 observer_failed、read_calls0、read_donefalse，没有将产品成功变成诊断异常。

`71581` 曾完成前五个增量的 38 控制，`86592` 完成加入 PW／晚事件后的 40 控制，均 actual0；`96237` actual1 是本人新增控制引用注入环境外 `root` 变量导致 ReferenceError，改为固定路径后再验，不是产品反例，原失败保留。作者 66614／33 控制、96324 strict TS、67870 exact list 的已有范围不扩大。

退休证据必须分开解释：`retired` 仅说明观察停更，wrapper 所有权冲突仍由 `observer_failed` 明确报告，不能只读 retired 推出所有 hooks 恢复成功；`pending_observations` 不等于原生产分支已 join。Node `sample_joined` 仅说明该 evaluate 观察分支已返回，`end_snapshot_observed` 与各 document `source:end` 单独记录；缺 end 或 late callback 不升级。flush/finish 先对已有 pending 最多等 250ms，若已返回再对 end evaluate 最多等 250ms，是两个串行上界，不声称整个 finish ≤250ms。原 PW45／expect5／Go120／Go6m／root540+60+3+75 未扩。全过程只离线 JS／实际库方法／静态源，没有 PG、浏览器、socket 或网络；新的真实执行仍需 root fresh grant。
