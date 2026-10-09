# Work recovery Project 根 GET 完成分支独立审查

Skills 未参与本增量实现。输入为 `/workspace/agenteam-work-ui` 的 `03f9228e` 加作者冻结9路径：`tests/account-captcha-web/e2e/project-work-planning.{helpers,native,publication,spec}.ts`、`.agent-state/work-owner-http/root_chain_driver.py`、`.agent-state/work-owner-planning-ui/project-refresh-completion-controls.cjs`，以及该目录 `project-refresh-diagnosis.md`、Work current、`docs/development/work-items/d11-work-owner-planning-ui.md`。**有限离线接受，无本范围 must-fix。** 不称新实际浏览器/PG/SQL通过，不回填 recovery08 FAIL 或其环境中断后未取得的 outer/TCP/input 尾。

## 证据链与范围

- 仅 recovery 三次固定 Project 根 GET/no query/200 启用。原 PW Request 恰一 `requestfinished` 仍等待原 `response.finished()===null`；恰一实际 `net::ERR_ABORTED` 且无 finished 才可走候选。原 Request/唯一 XID、Project/path/method/status/document、真实 native EOF/identity CL等长、reader cancel/release/outer cancel实际尾及无早abort全部继续强制。未知失败、重复事件、关闭/过期、额外 Query/错方法均不成为成功。
- 原 `auth.projects.get` 走 `projectRead`→`runAuthorized(undefined,'project-read')`→Project API/transport；正常结果只能在实际 body/cancel 和 `actual.finally` 释放 Session owner 后 fulfill。该链没有 password 的特殊提前成功分支；timer、abandon、identity 失效只可能拒绝。原 Session facade Promise 保留。
- Workspace 还须本次 `readCurrent` 的原 Promise 实际返回。它会吞异常，所以不能仅凭 Promise fulfill；观察器另核同身份/Project/generation、本次 readGeneration+1、`accept` 后 current detail 的 owner/version/archived、blocked=false 和 `canonicalize` 完成后的地址。唯一对象来自实际 Vue 根 provide 的 `project-workspace` Symbol，既有 private dist AST 校验唯一 marker；没有另造 Workspace 或替换产品 controller。原 Workspace Promise 也保持。
- 最后联合证明只在真正 finish 后核：每份 document 的首 explicit 退休、当时及当前 pending0、end 已见、恢复 hook 无错、sampler joined；sidecar 只取同 XID 唯一原响应，校 SHA/source/path/JSON content type，经正式 Project schema 和实际 typed API解码。native bytes、typed owner/version 与该原体对齐，恰三个不同 Project 才进入原 complete/Work verify/Go 后验。helper 的合成 Response 只用于重解码已保存原体，不增加 HTTP，不代浏览器消费证明。
- 旧 Work/Blocker 判据继续走其原闭集，其他页面的 Project refresh 仍走旧 finished/json 方法。root adapter 只增加 Project API 与 schema 两个只读输入；产品、Go、binary14、私有 dist、cap4 和原 PW45/expect5/Go120/Go6m/root540+60+3/TCP75 未改，不解除其他停止项。

## 本人控制与复用

`96791/4806f2` actual exit0，**6 个独立增量控制，0 unhandled**。通过 locked Playwright1.56.1实际 transform 后的 installer，复用作者已冻结 fixture 里的真实 Vue mount、Session、Project API/transport、Workspace，执行自己的6例，不重跑原116/41或作者55全组：

1. 真实改名刷新完成 canonicalize，当前 URL 与 normalized_name 为新值，完整 Project 判据接受；旧 Work 默认判据仍拒 Project 行。
2. 显式 `readCurrent(true)` 确能改变产品当前值，但不属于本无参刷新绑定，不能生成完成证明。
3. 直接 `auth.projects.get` 保留原 Promise，实际 Session 尾完成并返回 archived，Workspace 仍是旧 active；没有对应 Workspace 调用，必须拒绝该证明。
4. 两次实际读取产生同一受控 XID，真实捕获两个 native/call；保留两个实际记录时唯一性判据拒绝。
5. Workspace hook 被另一个 wrapper 接管时，退休保留新 wrapper、标 observer_failed，拒绝完成。
6. Project facade hook 相同冲突也保持上述拒绝。控制清理自己新设的 wrapper 不被称为观察器恢复成功。

首 `96940/48d000` 第4例是本控制设置失败：作者的一次读取 report 助手只投影首 native，而本人新增第二次实际读取；漏掉第二条后当然不能检测重复。修正为保留两条实际捕获记录后六例通过，不是产品反例，也不把原失败改写成功。

`087856` 实核原 `originalBody`、`decodeOriginal`、`schemaProgram`、`workIncompleteLedger` 四 AST 单元逐字相同，adapter逆去两新增输入等于03f原字节，`web/internal/cmd/db/api` 无差异。首 `057bf0` 误把 schemaProgram 常量当函数导致自身静态 probe失败，改按变量声明提取后通过；该命令随后diffcheck为0不代表前段 Node 已通过。

复用作者冻结未变证据：新55控 `46030/fa6c07`（实际生产桥、held reader/outer/canonicalize、失效/错误、Node原Request事件、原体schema/typed及三份最终证明），旧消费者116 `13067/389233`、native41 `99922/ab92ab`，严格TS `82068/0b4c55`。作者控制里的 PW/page 事件源也是明确替身，不与真实网络混称。

```sh
# cwd: /workspace/agenteam-skills；原锁定 Node/依赖，不下载
node .agent-state/work-project-read-review/controls.cjs
```

脚本只读取作者冻结 fixture 前段，在内存接入自己的6控制；Vite `write:false`、jsdom/Node原 Response/ReadableStream，不启动server/browser/socket。作者树不写产品或测试，原55/116/41未重跑。所有本人命令已取得实际终态，无资源在途；下一真实轮仍需root fresh grant及全部原资源尾。
