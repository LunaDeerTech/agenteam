# Passkey 前端验证

这些脚本仅使用任务私有服务，不访问用户的 5245/18443 实例。Playwright 复用仓库已锁定的 `tests/account-captcha-web/node_modules`，Chromium 使用 `/usr/bin/chromium`；不安装第二套依赖。所有浏览器旅程串行运行。

## 前端检查

在当前 Vue 工作树的 `web` 目录运行：

```sh
node node_modules/prettier/bin/prettier.cjs --check .
node node_modules/vue-tsc/bin/vue-tsc.js --noEmit
node node_modules/vitest/vitest.mjs run --maxWorkers=1
node node_modules/vite/bin/vite.js build
```

私有开发入口由 owner 确认 5274 空闲后启动，用户服务端口保持原状：

```sh
VITE_ACCOUNT_ENABLED=true node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 5274 --strictPort
```

依赖链接须按相同锁文件逐包只读复用，构建、`.vite`、`.vite-temp` 等缓存只写当前工作树。

## 受控页面旅程

从当前工作树根目录运行；`NODE_PATH` 指向已有的锁定 Playwright 安装：

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
PASSKEY_CONTROLLED_ORIGIN=http://localhost:5274 \
node tests/frontend-passkeys/controlled.cjs
```

`controlled.cjs` 严格拦截同源 API，使用合成账号和邮件响应；真实调用 `navigator.credentials` 与 Chromium 虚拟认证器，并校验原生 assertion 签名。涵盖首次登记、重命名丢响应后原 key/body 恢复、改邮箱错码与确认、免邮箱登录、原生取消和最后一把凭据删除后的回焦。这不能证明真实 App、数据库或 SMTP 链通过。

## 隔离的真实 App 旅程

仅在后端 fixture owner 明确发布 ready、交出唯一 SMTP 消费和浏览器窗口后运行。后端使用 `https://localhost:5270`，同源反代上述 5274；scope 必须是 `account-passkeys-email-change`。运行前由 owner 提供两个绝对私有路径：

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
PASSKEY_OWNED_RUNTIME=/absolute/private/runtime.json \
NODE_EXTRA_CA_CERTS=/absolute/private/certificate-ca.pem \
node tests/frontend-passkeys/owned.cjs
```

`owned.cjs` 校验 manifest 与 `passkey-owned.json`、文件权限、有效期、CA/证书/SPKI、实时 TLS 和 nonce。凭据、断言、一次性码及 MIME 只在内存使用，不写报告。验证码仅从该 fixture 的私有 SMTP sink 读取，绝不使用外部邮件服务。

真实旅程使用独立普通账号完成密码登录、原邮箱码首次登记、原生 Passkey 免邮箱登录、fresh Passkey 改邮箱及验证码确认；主动丢一次真实确认响应，显式重放原 key/body 获取 receipt。随后确认当前强 Session 和 Cookie 保留、另一登录 Session 被撤销。`main_chain_pass` 单列该链结果。

后续 `signature_budget` 单独使用两份真实 reauth assertion 验证错误签名限次：同 key 重放不重复扣次；四次不同 key 失败后原正确 assertion 仍可完成；五次后被拒。该证据只涵盖已登录 reauth，不外推匿名登录的重放语义。脚本最后退出本轮会话、移除虚拟认证器并关闭浏览器；fixture 的数据库与 SMTP 资源由后端 owner 统一清理。

独立交互/视觉审查仍由其专用 harness 负责。每次实际结果与首失败边界记录在任务 current；此文件仅维护运行方式，不以脚本存在替代通过结果。真实域名和实体设备留部署环境另验。
