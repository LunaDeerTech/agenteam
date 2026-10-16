# 系统出站策略与代理的浏览器验证

范围以[系统出站合同](../../docs/development/work-items/system-outbound-policy-proxy.md)为准。这里只验证新 Vue 的管理入口与本轮隔离真实 API；协议拨号、热生效和代理认证材料边界由后端及安全独审的专属 fixture 分别给出证据。已有账号、系统四页、旧出站 UI 的通过结果不代证本模块。

测试仅消费后端 owner 提供的任务所有实例，不自行启动或停止服务。禁止使用用户的 18443 实例、公开代理、真实邮箱或管理员配置。页面输入、Cookie、CSRF、代理密码、原请求 key/body 只在内存中比较，报告仅含安全状态与相等性；无 trace、截图、视频、默认失败 DOM 或原始响应附件。运行数据留在忽略的 `output/ai/frontend-outbound-policy/` 或 owner 的私有目录，不提交 Git。

依赖复用 `tests/account-captcha-web` 的锁定 Playwright 1.56.1 和系统 Chromium，无新 npm 安装。当前实际核对为 Node 24.19.0、Playwright 1.56.1、Chromium 151.0.7922.173。`playwright.config.cjs` 限定三个入口、worker 1、retries 0；运行前先 `--list`，加载成功不算行为通过。

限定场景：

- `rules.spec.cjs`：真实规则整份配置读取、增改删与严格回执、刷新持久；409 保留草稿到显式采用新版本。
- `proxy.spec.cjs`：真实代理保存、密码空值保持、替换/移除及安全投影；真实已接受写入后受控丢响应，显式恢复同 key/body，严格回执后的读取失败只重读。
- `authority.spec.cjs`：新端点普通身份 403 后仍为同一有效 Session；缺失 CSRF 的写入拒绝且版本不变。

后端 owner 唯一启动并清理 `https://localhost:5260`，TLS 反代本轮新 Vue 5246。loader 要求绝对路径的 `OUTBOUND_POLICY_RUNTIME`：runtime 文件 0600、直接父目录 0700、当前用户拥有且无符号链接；包含 `origin`、`nonce`、实际 `expires_at`、`certificate_ca_file`、`certificate_file`、`certificate_spki`、管理员 `credentials` 与普通用户 `regular_credentials`（两者均为 `{email,password}`）。证书文件在同一私有目录，校验 CA 签发、有效期、localhost 名称和 SPKI。浏览器只对指定 SPKI 使用测试证书例外，且在凭据使用前核实际 CDP leaf SPKI 与 `/__fixture__/identity` nonce。

同目录 `outbound-owned.json` 必须恰有 `{test_scope:"system-outbound-policy-proxy",runtime_file:绝对路径,nonce}`，与 runtime 一致。代理场景另需后端 owner 提供本轮受控 `proxy_gateway:{protocol,host,port}`；保存此网关不代表已验证对应隧道/投递。测试创建的代理密码仅在内存中随机生成。每个场景 finally 撤销本场景 Session 并确认 401，浏览器由 worker finally 关闭；服务保留或退出由后端 owner 报告。

先只列举，不读取私有 runtime、不启动浏览器：

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test \
  --config tests/frontend-outbound-policy/playwright.config.cjs --list
```

在两个 owner 确认服务 ready、产品输入冻结后，替换下列私有路径。首次可只选择 `rules.spec.cjs` 接通真实规则链；剩余场景按失败影响定向运行，不重复旧账号矩阵。`NODE_EXTRA_CA_CERTS` 必须是该 runtime 的 **CA 文件**，供代理场景的 Node 转发建立信任；不能使用全局 TLS 忽略。所有转发异常改为固定错误码，不打印原始 Playwright headers。

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
OUTBOUND_POLICY_RUNTIME=/absolute/private/runtime.json \
NODE_EXTRA_CA_CERTS=/absolute/private/ca.pem \
OUTBOUND_POLICY_OUTPUT=output/ai/frontend-outbound-policy/current-rules \
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test \
  --config tests/frontend-outbound-policy/playwright.config.cjs rules.spec.cjs
```

将末尾 spec 分别换为 `proxy.spec.cjs`、`authority.spec.cjs` 即运行其限定场景，同时换独立输出目录以保留先前失败。三个入口的 `--list` 实际 exit 0；loader/config 与修订 spec 的 `node --check`、五个 CJS 限定格式检查均 exit 0。

2026-10-16 实际组合为新 Vue 冻结 `e6bc7d61`、后端生产输入 `6ea558f1`、fixture 输入 `5a1871f1`；消费后端 owner 的私有 `/workspace/scratch/outbound-proxy-runtime-r3/runtime.json`，不是旧 r1/r2 或用户持久实例。三个场景分轮通过，未宣称一次全组运行：

| 输出子目录 | 实际结果 | 范围与限制 |
| --- | --- | --- |
| `run-04-authority` | 1/1，exit 0；CLI 18.8s | 管理员两端点 GET 200；规则及代理 PUT 缺 CSRF 均 403、两版本不变；普通身份两端点 GET 403 后仍为同一有效 Session。 |
| `run-05-rules` | 1/1，exit 0；CLI 17.1s | 真实整份规则增改删、刷新持久、独立更新造成 409 后保草稿并显式采用；保留 fixture 既有规则。 |
| `run-07-proxy` | 1/1，exit 0；CLI 58.0s | 真实保存、刷新后的安全投影、空密码保持、替换、明确移除；真实已接受 PUT 丢响应后显式同 key/body 重放，严格回执后的 GET 丢失只重读、写入次数不增加。 |

代理场景的普通 PUT 和故障 PUT 都通过 `route.fetch` 使用专用 CA、原 method/body/headers 请求真实服务；真实 receipt 在内存核 shape/version 后原样 `fulfill`。故障只丢第一份真实响应与恢复后的一个 GET，不制造成功回执。此处证明 UI/API 保存和恢复，**不证明 HTTP/HTTPS/SOCKS5 隧道、代理认证传输、热生效或 SMTP 投递**，那些证据由后端与安全独审分别提供。

此前实际失败保留，未改断言或增超时来消除：

- `run-01-rules` 在凭据输入前无登录页。一次匿名诊断实际观察到 502、固定“owned preview upstream unavailable”；作者确认 5246 Vite 进程已不存在，恢复相同源码的独立进程后继续。
- `run-02-authority` 在 bootstrap/login 准备包装内失败，尚未进入权限断言。当时没有 bootstrap 分阶段日志，无法补证准确根因；后续匿名浏览器观察 secure context、UUID 与 bootstrap 200 正常，不能倒推为 CSP 或产品故障。helper 已补固定阶段/整数状态与脱敏请求时序。
- `run-03-rules` 的真实 login/session 均 200，但 5 秒内 URL 尚未跳转。原轮缺少模块完成时序，根因未确定；`run-05` 同一 5 秒断言实际通过，记录点击到 URL 改变 1.807 秒。后续通过不抹去原失败，也不把共享 CPU 压力或冷模块推断写成已证原因。
- `run-06-proxy` 首 PUT 200 后测试的浏览器 `response.json()` 回读失败，页面已发后续 GET；具体底层异常已被安全包装，无法追溯。改为上文真实转发捕获回执后仅复验代理场景，保持原 receipt、持久与恢复断言。

所有实际 runner 已收集退出码；成功场景 finally 已确认本测试 Session logout 204 后 GET 401，浏览器关闭，匿名诊断进程也已退出。测试已释放 5260 配置写窗口；最终本轮代理为直连、保留网关且认证已移除。未访问 18443、未配置用户 SMTP、未发送真实邮件。root 已要求后端唯一 owner 清退 r3，服务及资源实际退出由该 owner 报告；此处不将测试退出写成后端已清退。过期 manifest 不能复用。
