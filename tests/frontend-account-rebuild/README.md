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
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs
```

runtime 文件必须 0600、父目录 0700，均归当前执行用户。字段为 `origin,nonce,certificate_spki,certificate_file,credentials:{email,password}`；其余受控渠道字段由 provider 提供，不输出到普通日志。测试仅接受精确 loopback HTTPS origin，核对证书 SPKI 与有效期，浏览器只信任该测试证书；不启用 `ignoreHTTPSErrors`、不降低产品 Secure Cookie、不将材料发送到 HTTP 公网预览。每个页面先检查测试代理 `/__fixture__/identity` 的 nonce，才填写凭据。

默认关闭 trace、video、screenshot，避免采集密码、Cookie 与一次性 token。结果只记录必要状态与安全事实。截图实看按独立审查者的脱敏流程执行，向用户展示前上传约定图床。

当前源码仅建立登录→受保护返回→刷新→退出及真实 Session 状态、Cookie 属性的首链；尚未运行。后续按账号工作项增量覆盖个人设置、邀请与重置、受控失败和请求所有权，模拟和真实提供方结果分别报告。SMTP 配置后不回退受限日志；私有 sink 只有摘要时不能据此宣称取得了邮件中的恢复链接。
