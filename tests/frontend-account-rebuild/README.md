# 新 Vue 账号真实浏览器测试

本 harness 只消费本轮自有 HTTPS 账号组合，不启动旧 `web-crape/`，不以模拟 API 声称真实联调。提供方由后端 owner 的测试夹具启动真实 Central、独立 PostgreSQL/MinIO 与私有恢复/邮件渠道；`https://localhost:5250` 将页面代理到本轮新 Vue 5245。尚未就绪时测试明确失败，不跳过。

依赖沿用 `tests/account-captcha-web/package-lock.json` 的 Playwright 1.56.1。先做不要求运行夹具的入口加载检查：

```sh
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs --list
```

后端 owner 提供当前运行的 `private/runtime.json` 绝对路径后，从仓库根执行：

```sh
export FRONTEND_ACCOUNT_RUNTIME=/absolute/task-owned/private/runtime.json
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/browser-run-01"
# 仅在和其他使用者协调完私有 SMTP mailbox 独占后设置；完整旅程会发邀请与重置两封测试邮件。
export FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE=1
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs
```

runtime 文件必须 0600、父目录 0700，均归当前执行用户。字段为 `origin,nonce,certificate_spki,certificate_file,credentials:{email,password}`；其余受控渠道字段由 provider 提供，不输出到普通日志。测试仅接受精确 loopback HTTPS origin，核对证书 SPKI 与有效期，浏览器只信任该测试证书；不启用 `ignoreHTTPSErrors`、不降低产品 Secure Cookie、不将材料发送到 HTTP 公网预览。每个页面先检查测试代理 `/__fixture__/identity` 的 nonce，才填写凭据。

默认关闭 trace、video、screenshot，并设置 Playwright 1.56 的 `PLAYWRIGHT_NO_COPY_PROMPT=1` 关闭独立于 trace 的失败 DOM 快照，避免采集密码、Cookie 与一次性 token。密码填入和私有链接导航使用固定安全错误包装，避免默认 Playwright 失败 call log 回显值。失败附件只记白名单 API 路径、method 和 HTTP status，不含 query、body 或 header。截图实看按独立审查者的脱敏流程执行，向用户展示前上传约定图床。

当前证据分轮保留在忽略目录 `output/ai/frontend-account-rebuild/`。`browser-run-01` 的真实 API 边界 1 项通过，登录首链 1 项因成功导航误触发离页保护失败；产品修复后仅重跑首链，`browser-run-02` 的 1 项通过，包括登录→受保护返回→刷新→退出、Secure/HttpOnly/SameSite=Lax Cookie 与退出后 401。`browser-run-03` 的完整旅程停在真实 SMTP 邀请已接收但公开邀请页初导航被匿名监听跳回登录，尚不算完整旅程通过。

`web/tests/account-transport.spec.ts` 的 10 项受控测试已通过，覆盖请求串行至 body/cancel 尾部、命令和确认读取不可插队、迟到归属、严格成功与 Unknown 等。`web/tests/account-session.spec.ts` 的 7 项通过，随后因上下文代际实现变更定向复验仍 7/7：确认后的 GET 失败不重复登录/改密、原请求显式重试、两类 CSRF 失效及 401/403 身份边界。它们不能代证浏览器 Cookie 或真实 PG。

`mail-reader.cjs` 消费任务所有的 SMTP sink 私有 nonce 控制通道，限制地址与消息大小，仅返回匹配收件人、正式 origin 和用途的邮件链接。该 sink 只保留最后一封成功邮件；触发邮件到取回正文期间必须和其他审查者协调独占 mailbox，取得私有材料后再释放。SMTP 配置后不回退受限日志，两个渠道的证据分别报告。

后续按账号工作项增量覆盖个人设置、邀请与重置、受控失败和请求所有权，模拟和真实提供方结果分别报告。
