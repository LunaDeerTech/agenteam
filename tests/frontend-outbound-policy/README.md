# 系统出站策略与代理的浏览器验证

范围以[系统出站合同](../../docs/development/work-items/system-outbound-policy-proxy.md)为准。这里只验证新 Vue 的管理入口与本轮隔离真实 API；协议拨号、热生效和代理认证材料边界由后端及安全独审的专属 fixture 分别给出证据。已有账号、系统四页、旧出站 UI 的通过结果不代证本模块。

测试仅消费后端 owner 提供的任务所有实例，不自行启动或停止服务。禁止使用用户的 18443 实例、公开代理、真实邮箱或管理员配置。页面输入、Cookie、CSRF、代理密码、原请求 key/body 只在内存中比较，报告仅含安全状态与相等性；无 trace、截图、视频、默认失败 DOM 或原始响应附件。运行数据留在忽略的 `output/ai/frontend-outbound-policy/` 或 owner 的私有目录，不提交 Git。

依赖复用 `tests/account-captcha-web` 的锁定 Playwright 1.56.1 和系统 Chromium，无新 npm 安装。当前实际核对为 Node 24.19.0、Playwright 1.56.1、Chromium 151.0.7922.173。`playwright.config.cjs` 限定三个入口、worker 1、retries 0；运行前先 `--list`，加载成功不算行为通过。

计划限定场景：

- `rules.spec.cjs`：真实规则整份配置读取、增改删与严格回执、刷新持久；409 保留草稿到显式采用新版本。
- `proxy.spec.cjs`：真实代理保存、密码空值保持、替换/移除及安全投影；真实已接受写入后受控丢响应，显式恢复同 key/body，严格回执后的读取失败只重读。
- `authority.spec.cjs`：新端点普通身份 403 后仍为同一有效 Session；缺失 CSRF 的写入拒绝且版本不变。

本轮接口、私有 runtime 合同及 fixture 尚在准备，当前仅搭建 loader 配置；spec 未落齐，也未执行 `--list` 或真实浏览器。后续准确命令、输入、通过/失败与资源退出会在实际完成后补充。不可把此计划记为已验证。
