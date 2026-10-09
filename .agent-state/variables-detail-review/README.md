# Variables authority 首次详情 GET 增量独立审查

对 `/workspace/agenteam-project-variables-ui` 的 `0c5a9e6d..2a603a3d219de46105a266f805b67ad1227ac2a3`，14 个冻结路径有限离线接受，无 must-fix。范围仅 authority 首次 main Project/Variable 的 GET/no-query/200 完成分支；不翻转 Authority03 whole FAIL，不替代新 Go candidate09 或实际 browser/PG/SQL 验收。

## 受审闭包与结论

- 新 `tests/account-captcha-web/e2e/project-variables.detail.ts` 通过实际 dist03 的既有 Session AST binding 包装原 `auth.projectVariables.get`；接收者、两参数、原 Promise/throw 不变。初始无 editor、原 GET 返回值仅留在 document 内，与唯一原 Request/XID/private 完整响应的正式 API 解码值逐字段匹配，输出为闭集计数/布尔。
- `useSession` 的原 GET/transport 消费/owner finally 与 `useProjectVariables.readSelected` 的原 captured context/readRevision/live/adopt 是实际产品路径。仅 facade fulfillment 不够：原身份/authenticated/owner idle、相同 document/route、fresh editor、目标 ID/version、三字段值、非 disabled、无 review/原 operation progress 等共同证明实际采用。真实 SFC 的 disabled 派生包含 blocked/current mutation/context 状态，不能拿陈旧页面值补证。
- helper 仅在 `AGENTEAM_PROJECT_VARIABLE_WEB_CASE=authority` 的首次指定 endpoint 预装。最终判定还要求唯一原 Request、无 body/query、状态200、failed 且无 finished、无 declaration，全部失败记录只能是该请求的一次 unexpected-failed。重复/交错 finished 与 failed、其他 GET/method/endpoint/错误仍拒绝。正常 requestfinished 仍等待原 `response.finished() === null`。
- 原 native 模块未改；最终同原 Request/document 的唯一 PW/native XID、EOF 在中断前、identity Content-Length/实际 bytes、单 reader、read/两 cancel/release 实际结算且无 abort/拒绝，导航前 `endDocument`、后续采样实际 join、hooks 真还原全部必须成立。detail 的 DOM证据在进入归档修改前首次 finish 捕获，后续 authority stage 的 document retirement 与最后 native.stop 是另两项事实；不能将任一 Promise.race 超时当 join。250ms 内只赋值一次结果，迟到 work 不升级 Node facts。
- 原 `originalResponse` 的 method/path/query/status/content type/body/key/CSRF/唯一 XID 私有记录校验保持；严格客户端与正式 Schema 在最终 `validateOriginalBodies` 继续运行。唯一新增资格只跳过目标已独立证明消费的 PW `response.body()` 再读取，其他响应逐字比较原 body 不动。资格本身不能由可构造的公开值或 native EOF 单项给出。
- Go fixture 只增加 strict typed safe detail 投影；未知字段/错误类型/计数越界通过原严格 decoder 拒绝，不输出原 body/ID/文本。原 Go 主验收和 SQL 后验未改。driver 只将 detail.ts 纳六个 UI 输入闭包，default/预算/资源门不变。

## 独立控制与源保留

命令（cwd `/workspace/agenteam-skills`）：

```sh
node .agent-state/variables-detail-review/controls.cjs
```

`85150/5ca8ba` actual exit0，5 个差异控制：

1. 首次 finish 收到错 XID 后，即使再给正确 XID 也不能升级原事实。
2. 原 Promise 已 fulfillment、pending0、body 匹配，但 DOM 尚未发布；退休后的迟到 render 不升级。
3. 已有 hook 被变为不可写，真实还原失败时 `hooks_retired=false` 阻止接受；退休后的 cached wrapper 保原 receiver/args/Promise，事实不变。
4. 原 Request 的 failed 后再 finished，仍因混合事件错误拒绝，不能被 detail proof 豁免。
5. 三参数调用原样传给原 facade/保 Promise，但不能进入限定两参数候选。

控制复用作者文件的 **setup 部分**，使用实际 locked Playwright transform/evaluationScript 后的 installer、实际 helper；DOM/Session/native 数据仍是明确替身，不冒真实产品或网络。未运行作者全部116控制，也未重新执行生产 consumer/SFC10 控。临时 Playwright transform cache 只在本树 `output/ai/skills/variables-detail-review/pw-cache`；未写作者树。

独验原失败保留：`c8dc37` 是探针把 setup 的 hashbang 传给 `new Function`，修为去除首行；`70378/3d174b` 是探针误要求非 strict 的序列化函数在不可写赋值时必须抛错/置 `observer_failed`，实际该赋值静默失败但 `hooks_retired=false` 已正确拒绝。修正独立断言后通过，没有作者技术返修或产品反例。

静核打印 PASS：driver 去唯一 detail 输入后逐字0c5；native.ts、authority.ts 与 Go 主 test 逐字0c5；web/internal/cmd/db/api 零 diff；原 command/schemaProgram/decoderCSRF/originalResponse 逐字，`validateOriginalBodies` 逆去新增资格参数和目标 body 条件后逐字旧实现。

作者未变范围复用：生产 Session/Workspace/Variables/真实 SFC10（63f8ce）、锁定PW序列化及helper116（b54ddd）、typed Go51（dfded5）、旧events14/authority41/decoder+Schema及strictTS/vue-tsc。它们各自边界保持；本次无 PG/browser/socket/network/大编译或未回收测试进程。新实跑仍须 root fresh grant，原失败不可回填。
