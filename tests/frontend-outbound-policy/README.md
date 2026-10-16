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

在两个 owner 确认服务 ready、产品输入冻结后，替换下列私有路径。首次可只选择 `rules.spec.cjs` 接通真实规则链；剩余场景按失败影响定向运行，不重复旧账号矩阵。`NODE_EXTRA_CA_CERTS` 必须是该 runtime 的 **CA 文件**，供受控丢响应中的 Node 转发建立信任；不能使用全局 TLS 忽略。所有转发异常改为固定错误码，不打印原始 Playwright headers。

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
OUTBOUND_POLICY_RUNTIME=/absolute/private/runtime.json \
NODE_EXTRA_CA_CERTS=/absolute/private/ca.pem \
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test \
  --config tests/frontend-outbound-policy/playwright.config.cjs rules.spec.cjs
```

当前实际结果：两个 loader/config 文件 `node --check`、五个 CJS 限定格式化均 exit 0；`--list` 实际 exit 0，列出 3 文件、3 场景。真实 fixture 尚未 ready，前端还在完成删除确认和密码派发清空，**尚未运行真实浏览器，未形成业务通过结论**。后续结果会区分真实后端行为和受控丢响应边界，不以列表加载或历史通过代证。
