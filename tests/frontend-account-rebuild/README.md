# 新 Vue 账号真实浏览器测试

本 harness 只消费本轮自有 HTTPS 账号组合，不启动旧 `web-crape/`，不以模拟 API 声称真实联调。提供方由后端 owner 的长驻 Go 测试进程装配真实 Central App HTTP/API、独立 PostgreSQL/MinIO 与私有恢复/邮件渠道；这不等同独立 `cmd/central` 进程验证。`https://localhost:5250` 将页面代理到本轮新 Vue 5245。尚未就绪时测试明确失败，不跳过。

依赖沿用 `tests/account-captcha-web/package-lock.json` 的 Playwright 1.56.1。先做不要求运行夹具的入口加载检查：

```sh
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs --list
```

后端 owner 提供当前运行的 `private/runtime.json` 绝对路径后，从仓库根执行：

```sh
export FRONTEND_ACCOUNT_RUNTIME=/absolute/task-owned/private/runtime.json
# 指向该 manifest 的 certificate_file，供故障注入时 Node route.fetch 严格验证测试证书。
# Chromium 同时继续按 manifest 中的 SPKI 信任；不使用 ignoreHTTPSErrors。
export NODE_EXTRA_CA_CERTS=/absolute/task-owned/private/certificate.pem
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/browser-run-01"
# 仅在和其他使用者协调完私有 SMTP mailbox 独占后设置；完整旅程会发邀请与重置两封测试邮件。
export FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE=1
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs
```

runtime 文件必须 0600、父目录 0700，均归当前执行用户。字段为 `origin,nonce,certificate_spki,certificate_file,credentials:{email,password}`；其余受控渠道字段由 provider 提供，不输出到普通日志。历史账号旅程使用精确 `https://localhost:5250`；新增菜单头像片段也接受下述固定 r2 origin，通过本机单目标 CONNECT 进入同一 fixture。核对证书 SPKI 与有效期，浏览器只信任该测试证书；不启用 `ignoreHTTPSErrors`、不降低产品 Secure Cookie、不将材料发送到 HTTP 公网预览。每个页面先检查测试代理 `/__fixture__/identity` 的 nonce，才填写凭据。原 localhost 场景没有因新增 r2 支持而重新执行或获得 r2 通过结论。

默认关闭 trace、video、screenshot，并设置 Playwright 1.56 的 `PLAYWRIGHT_NO_COPY_PROMPT=1` 关闭独立于 trace 的失败 DOM 快照，避免采集密码、Cookie 与一次性 token。密码填入和私有链接导航使用固定安全错误包装，避免默认 Playwright 失败 call log 回显值。失败附件只记白名单 API 路径、method 和 HTTP status，不含 query、body 或 header。截图实看按独立审查者的脱敏流程执行，向用户展示前上传约定图床。

当前证据分轮保留在忽略目录 `output/ai/frontend-account-rebuild/`。`browser-run-01` 的真实 API 边界 1 项通过，登录首链 1 项因成功导航误触发离页保护失败；产品修复后仅重跑首链，`browser-run-02` 的 1 项通过，包括登录→受保护返回→刷新→退出、Secure/HttpOnly/SameSite=Lax Cookie 与退出后 401。`browser-run-03` 的完整旅程停在真实 SMTP 邀请已接收但公开邀请页初导航被匿名监听跳回登录，尚不算完整旅程通过。

`web/tests/account-transport.spec.ts` 的 10 项受控测试先通过，随后因实现调整新增已发 mutation 归属失效必须 Unknown 的分支，限定重跑 11/11 通过，覆盖请求串行至 body/cancel 尾部、命令和确认读取不可插队、迟到归属、严格成功与 Unknown 等。`web/tests/account-session.spec.ts` 的 7 项通过，随后因上下文代际实现变更定向复验仍 7/7：确认后的 GET 失败不重复登录/改密、原请求显式重试、两类 CSRF 失效及 401/403 身份边界。它们不能代证浏览器 Cookie 或真实 PG。

初导航修复后，`browser-run-04` 完整旅程 1/1 通过（31.4 秒）：真实 SMTP 邀请兑换、普通账号登录、资料保存和 409 冲突保留草稿、外观预览/取消/保存/刷新、改密后当前 Session 有效且另一旧 Session 401、真实 SMTP 找回重置、目标账号当前 Session 失效、新密码登录后仍保持已保存主题。邮件均取自成功 DATA 250 的私有 sink 正文，不拼接受限日志；这不证明公网或外部 SMTP 提供方可用。

`avatar-recovery.spec.cjs` 为后续定向片段，通过真实 API 和私有 SMTP 新建独立普通账号，仅复用登录辅助步骤，避免每次重复完整七页旅程。覆盖头像拒绝/预览/上传/读取/删除，及确认删除后读取失败只重读；另在真实资料 PATCH 已提交后受控丢弃响应，验证 Unknown 状态的显式同 key/body 重放。受控丢失用 Playwright 路由拦截，实际写入仍由真实 Central/PG 执行，与自然网络故障分开说明。

`browser-run-05` 已走通头像 SVG 拒绝不上传、PNG 本地预览/取消、实际上传并存为 JPEG、刷新后读取。后半故障注入因测试 Node 缺少 fixture CA 信任失败，属于 harness 配置问题。默认 `route.fetch` 错误曾将该独立临时账号 Cookie/CSRF 写入失败结果；已真实 logout 204、再读 Session 401 撤销会话，本地结果敏感行清理并改为 0600，保留 TLS 失败事实，无 trace/截图/上传或 Git 跟踪材料。后续 `safeRoute` 捕获所有拦截器错误，仅报告固定错误码，Node 进程显式加载本轮 CA；头像前后片段已拆开，定向 `--grep confirmed-delete` 仅复验恢复与 Unknown。

`browser-run-06` 使用本轮专属 CA 定向复验后半 1/1 通过（16.4 秒）：实际 DELETE 204 后受控丢弃 GET，只重读恢复，DELETE 共 1 次；实际 PATCH 已提交后丢弃响应，UI 显示 Unknown、冻结草稿，显式重试的 key/body 与原命令一致，最终服务端版本等于初次提交版本。头像前半沿用 run-05 已执行完的步骤事实，没有将其外层失败测试记成通过，也未为新拆分入口虚构独立运行。完整旅程、API 边界和首链均未无依据重复。

最后产品源码冻结于 `85c4ac49` 后，测试 owner 唯一执行 `npm run test:unit --prefix web`，6 文件 35/35 通过（13.26 秒），含合法 `not_started` 保持可纠正及缺失/非法状态仍 Unknown 的三项回归。工程 format/type/build 由 integration owner 执行，独立安全与视觉审查由其他 owner 提供。原型、旧共享 UI 测试、受控故障、真实私有 SMTP 与公网/真实邮件提供方的证据各自独立；此处不声称公网账号、真实外部邮件或所有历史安全矩阵已经验收。

`challenge.spec.cjs` 将本轮 frontend 作者的真实挑战脚本迁入同一恢复入口：真实错误密码拒绝、公开图片旋转、错误 proof 拒绝、键盘滑块正确验证与原 login key 完成登录。仅复用 `tests/account-captcha-web/e2e/public-solver.ts` 的公开像素算法；没有读取后端答案、替换验证结果或降低阈值。新账号准备与其他片段共享 `journey-support.cjs`，仍须独占私有 SMTP mailbox。正式入口 `--list` 共加载 6 项；加载成功不等同实际通过。

迁移后仅执行正式 `challenge.spec.cjs`，`browser-run-07` 1/1 通过（约 1 分钟）：真实错误密码 401 的 `commit_state=not_started`，输入仍可纠正且未出现 Unknown 重试按钮；错误 proof 400；公开图片计算后使用真实键盘调节 range，verify 200、login 200 且保持原 login key；390px 无页面横溢。这是正式恢复源的实际结果，与此前 frontend 作者临时脚本的通过事实分别记录。

作者账号浏览器最终证据为五条独立通过测试（API 边界、首链、完整旅程、恢复片段、正式挑战），加 run-05 头像前半已完成步骤。没有把 `--list` 中新拆分的头像入口虚构成独立通过，也未为凑全量数字重跑未变场景。所有作者 Playwright 进程实际退出；本 harness 没有持有服务端口，5250/5245 生命周期由后端与 integration owner 管理。最终禁用账号 dist 的三个基础兼容场景结果与 5242 子进程退出事实见 [基础 harness](../frontend-rebuild/README.md)。

独立审查者的 [会话 Cookie 竞态 probe](session-cookie-race-probe.cjs) 是单独的有界恢复入口，不加入上述 Playwright 页面矩阵。当前实际运行命令如下；恢复时必须先由后端 owner 启动本任务的新实例并提供有效私有 manifest，不能把历史路径当作服务仍在运行：

```sh
mkdir -p output/ai/account-review
FRONTEND_ACCOUNT_RUNTIME=/workspace/scratch/account-preview-public-r2/runtime.json \
  node tests/frontend-account-rebuild/session-cookie-race-probe.cjs \
  > output/ai/account-review/session-cookie-race-final.jsonl
```

该 manifest 仍须为当前用户所有、文件 0600/父目录 0700，除凭据和 nonce 外提供 `origin,expires_at,certificate_file,certificate_ca_file,certificate_spki`。此 probe 固定浏览器地址为 `https://154.12.86.233:18443`，通过临时 loopback CONNECT 转发器仅允许该一个目标，并固定拨号 `127.0.0.1:5254`；转发器不终止 TLS。Node 请求使用专属 CA 和 IP SAN 严格验证，浏览器使用 SPKI 并检查实际叶证书及两标签页 nonce，不使用全局忽略 TLS。它验证的是**本机固定路由**，不经过公网 frp，不能据此宣称公网 18443 可达。

正式 probe 已由原审查者实际执行，GET `/session` 和 PATCH `/me` 两个场景均通过、进程 exit 0：取得真实后端 401 响应后受控延迟交付给旧标签页，在另一标签页建立新登录，迟到 401 不含 Set-Cookie、不改变新 Session Cookie，后续 Session 200；显式 logout 204 后 Cookie 清除、Session 401。结果中的实际浏览器叶证书 SPKI 与 nonce 两项均为 true。该范围证明真实浏览器 Cookie 与服务端错误响应边界，不替代 Vue 页面交互或自然网络延迟的全链验证。原 owner 已确认浏览器与临时 CONNECT 监听退出；输出仅含状态、计数、相等性布尔值等安全事实，无 Cookie/CSRF 值、请求头、正文、trace 或截图。

`mail-reader.cjs` 消费任务所有的 SMTP sink 私有 nonce 控制通道，限制地址与消息大小，仅返回匹配收件人、正式 origin 和用途的邮件链接。该 sink 只保留最后一封成功邮件；触发邮件到取回正文期间必须和其他审查者协调独占 mailbox，取得私有材料后再释放。SMTP 配置后不回退受限日志，两个渠道的证据分别报告。

本轮共享头像的产品输入为 `1e0dbf0a`。`web/tests/account-avatar.spec.ts` 的 4 项受控行为测试通过：共享读取与 HTTP body 尾部所有权、相同 metadata 复用；替换/确认删除释放 Blob、失败重读和旧版本不能恢复图像；身份失效清理、迟到旧身份 Blob 释放且不覆盖新身份；读取失败与重载。随后唯一执行 `npm run test:unit --prefix web`，7 文件 39/39 通过（11.85 秒）。之后仅修正测试 mock 的 TypeScript 函数签名，没有改变运行行为；类型检查由工程 owner 补验，未重复全量 unit。

`avatar-menu.spec.cjs` 是新增的一条真实菜单头像片段，使用 fresh SMTP 邀请普通账号，管理员只用于发出该邀请，不修改其资料、头像、密码或主题。运行前仍须协调独占 mailbox，并核实 r2 manifest 尚有效；不重启已有服务或 frp。实际命令为：

```sh
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
export FRONTEND_ACCOUNT_RUNTIME=/workspace/scratch/account-preview-public-r2/runtime.json
export FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE=1
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/avatar-menu-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs avatar-menu.spec.cjs --list
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs avatar-menu.spec.cjs
```

本轮 `--list` 加载 1 条，随后实际 1/1 通过（14.2 秒、exit 0）：候选预览不改变顶栏；确认上传后顶栏和资料页共用 Blob URL；离开资料页仍保留头像，经账号菜单可返回个人设置；退出后 Session 401 且旧 URL 已释放；新文档直接登录项目页也能读取已保存头像；确认删除后两处同步清空并释放 URL，最终退出再读 401。失败收尾也会尝试撤销本测试上下文的 Session，不保存凭据。浏览器与临时 CONNECT 经 fixture `finally` 关闭，CLI 已退出，未新增保留监听；原服务和 frp 未动。没有重复密码、挑战或七页完整旅程。

该片段通过 [fixed-runtime.cjs](fixed-runtime.cjs) 的最小适配器使用 r2：只允许 CONNECT `154.12.86.233:18443` 并固定拨号 `127.0.0.1:5254`，不终止 TLS。manifest 的 `certificate_file` 是 leaf、`certificate_ca_file` 是专用 CA；核验 CA 签发、IP SAN、期限及 SPKI，浏览器实际 Security 事件中的 leaf SPKI 必须匹配，nonce 通过后才进入凭据步骤。此片段没有 Node `route.fetch`；若以后增加，必须使用专用 CA 与安全错误包装，不能把 leaf 当 CA 或全局忽略 TLS。此结果仅证明**本机固定路由下的真实 Vue/API 链路，不是公网 frp 验真**。既有会话竞态 probe 保持原样，其通过证据与本轮分别记录。

系统用户与账号四页沿用本 harness，以[系统账号合同](../../docs/development/work-items/frontend-system-account-settings.md)为准。其全局安全/SMTP写入必须使用后端 owner 另建的独立 `https://localhost:5250` 实例，禁止指向供用户试用的 r2。新增 [system-owned.cjs](system-owned.cjs) 除核验原 runtime，还要求同目录、当前用户所有的 0600 `system-owned.json`，其中 `test_scope` 必须为 `frontend-system-account-settings`、`runtime_file` 必须等于传入 manifest 绝对路径、`nonce` 必须匹配；实例期限也必须有效。fixture 收尾注销当前测试上下文并确认 Session 401，服务生命周期仍由后端 owner 唯一管理。

系统首链的恢复命令如下。`system-users.spec.cjs` 先读取真实管理员目录；若不足分页，仅在该隔离实例通过正常邀请/私有 SMTP/兑换 API 补到 11 个注册用户，再验证每页 10 条的游标前后页与只读展示。不使用搜索、角色筛选或全量补抓，也不写共享 PG fixture。

```sh
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
export FRONTEND_ACCOUNT_RUNTIME=/absolute/isolated-system/private/runtime.json
export FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE=1
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/system-users-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-users.spec.cjs --list
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-users.spec.cjs
```

本轮 `web/tests/system-account.spec.ts` 首轮 2 项通过、1 项失败：系统 403 清除显示/旧管理命令但保留登录，以及同身份 checking 保留未确认原请求、真实角色丢失使其失效已通过；请求 10 条却接受 11 条的响应上界失败交给产品 owner 修复。修复后仅定向复验该项与新增 SMTP 四项，5 项通过；随后 generic `useCommand` 的 committed 原意图/严格回执后读取失败行为新增 1 项并定向通过。最后由 integration owner 唯一执行 `npm run check --prefix web`，实际全量 47/47 unit 与 format/type/build 通过。后续用户要求的标题动作及说明文案改动由该 owner 补 format/type/build，未重复无关账号浏览器矩阵。

四页使用后端 owner 的独立 `system-settings-runtime-r1`，真实 App/PG/MinIO/私有 SMTP，后端生产输入 `7aa9b503`；不是另起正式 `cmd` 二进制的验收，也不是公网 r2。恢复时必须让 owner 提供新的有效 runtime 和 sidecar，不能沿用已过期证书。安全配置和 SMTP 的受控丢响应使用 Node `route.fetch`，必须指向 manifest 的 **`certificate_ca_file` 专用 CA**，不能误用 leaf。所有转发异常包装为固定错误码；私有 body/key 只在内存中比较，密码断言只输出相等性布尔值。

以下每条均先 `--list` 加载成功再单独实际运行；`--list` 本身不算业务通过。在上面的隔离 runtime/mailbox 环境变量之外设置专用 CA，并为每次运行提供独立输出目录：

```sh
export NODE_EXTRA_CA_CERTS=/absolute/isolated-system/private/certificate-ca.pem
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/system-security-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-settings.spec.cjs
# 其余限定入口，分别设置输出目录后单独运行：
# system-invitations.spec.cjs
# system-permissions.spec.cjs
# system-smtp.spec.cjs
```

本轮分段实际结果如下，后续小改只复验受影响部分：

- `system-users-run-01`：1/1 通过，38.6 秒。通过正常邀请/私有 SMTP/兑换补到 11 个注册用户（管理员加 10 个普通账号），真实前后游标页每页上界 10、顺序和只读目录通过。
- `system-security-run-01`：1/1 通过，11.5 秒。真实安全配置保存与独立版本推进产生 409，草稿保留到明确采用；真实 PUT 已接受后受控丢弃响应，检查当前状态不当作确认，同 key/body 重放取得回执；之后 GET 失败仅重读，服务端版本没有再次增加。受控响应丢失不等同自然网络故障或真实 PG 提交不明。
- `system-invitations-run-01`：创建 POST 201 与列表 GET 200 已发生，但确认状态 DOM 断言失败，不能记为通过。两处邀请源 mtime 确实落在该运行窗口内，保留此重叠事实，不断言 HMR 是唯一原因。相关源冻结后 `system-invitations-run-02` 1/1 通过，83.06 秒：创建、安全投影、私有 SMTP 接收、人工重试、真实 60 秒间隔后的重发、撤销反例、原链接兑换及用户目录可见；重发/重试不延长链接期限，不臆造邀请版本递增。
- `system-permissions-run-01` 在准备现有普通账号时错用 Session CSRF 调用公开重置入口，得到 403，属于 harness 准备错误。修为匿名 bootstrap CSRF 后 `system-permissions-run-02` 1/1 通过，18.1 秒：仅将本轮该隔离实例已有的普通种子账号通过真实私有邮件重置一次，不新建账号；普通身份真实 GET 系统用户接口 403，随后 Session 200 且身份仍为同一普通用户。此项停留在 fixture 文档执行 HTTP，不代证 Vue 的权限错误布局；页面清显示由 unit 和独立受控视觉证据覆盖。
- `system-smtp-run-01`：最终产品输入 `243a4560`，1/1 通过，18.9 秒。单密码框空值保持凭据且请求省略 password；新值替换、派发清空；真实保存接受后受控丢响应，仍用原 key/body/版本显式恢复，GET 安全投影不回显密码。保存私有主机的未允许端口成功不触发发送；独立测试 202 后受控丢 GET，只重读且测试 POST 共一次，真实任务最终 `policy_rejected`。恢复原端口后人工 retry 产生新任务，真实 SMTP sink 接收并确认 `sent`，与“请求已受理”分开；最后显式移除认证与停用配置均确认。成功后仅将空密码断言改为布尔输出，防未来失败报告回显值，没有改变步骤或重跑真实写入。

上述五个限定入口各有一轮实际通过，原失败仍保留在忽略输出目录。全部作者 Playwright/CLI 已退出，每次测试上下文收尾真实 logout 204、再读 Session 401；无 trace、默认失败 DOM、截图、邮件正文或凭据附件。该 harness 没有持有监听，也未改公网 r2 用户管理员、配置或 frp。最终 owned 5250 的 SMTP 因显式停用验收而未配置，mailbox/全局写窗口已释放；服务退出由后端 owner 统一执行，本 README 不把测试进程退出当成后端服务退出。

用户随后明确每页可选 5/10/20/50、默认 10。新增 `web/tests/system-account-pagination.spec.ts` 仅定向 2/2 通过（7.22 秒）：响应数组上界绑定实际请求大小；旧 5 条页面的 body 尚未读完时切到 20，不提前释放共享 HTTP 队列，完成收尾后新请求无旧 cursor，迟到旧行不发布。该测试源与下面真实入口保存于 `0a824d41`，运行消费 frontend owner 后续冻结的分页产品候选；此前四页证据不当作新分页证据。

新增真实入口仅使用现有管理员读取已种好的独立用户目录，不恢复 SMTP、不创建账号或发邮件。先 `--list` 加载 1 条，再实际 `system-pagination-run-01` 1/1 通过（10.2 秒）：默认 10，选 5 后进入第二页携带实际 cursor，再选 20/50 各产生新的对应 limit 请求、清 cursor 回第一页，显示条目与真实 API 响应一致。收尾仍 logout 204 → Session 401，浏览器和 CLI 退出，owned 服务生命周期留给后端 owner。三个列表的布局、键盘选择和 SMTP 空列表 query 由独立受控审查另行覆盖，不与该真实用户目录范围混记。

```sh
npm run test:unit --prefix web -- tests/system-account-pagination.spec.ts
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/system-pagination-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-pagination.spec.cjs
```

后续 SMTP 详情弹窗改动使用 [system-smtp-modal.spec.cjs](system-smtp-modal.spec.cjs) 做有限的**实际 Vue 浏览器、受控 HTTP 响应**验证。先由页面 owner 确认当前源码的账号启用 dev 位于精确 `http://127.0.0.1:5245`；此入口不启动服务、不读取 runtime、不登录真实账号。所有 `/api/v1/` 都使用合成 Session/配置/任务响应，未知请求与外部 origin 被拒绝，不接触已清理的 5250、公网 r2 或真实 SMTP。沿用锁定 Playwright 1.56.1、系统 Chromium、原 config 的无 trace/video/screenshot/失败 DOM 设置，无新依赖。

```sh
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/smtp-modal-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-smtp-modal.spec.cjs --list
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-smtp-modal.spec.cjs
# 只复验普通详情关闭的焦点/滚动问题时，另设独立输出目录并追加：
# --grep 'detail closes back'
```

本轮 `--list` 加载 3 条。首产品输入 `91f16e7e` 的 `smtp-modal-run-01` 实际 2 通过、1 失败（36.3 秒）：Esc 关闭后原列表按钮未回焦。作者与独审定位为 busy 使所有 Tab 禁用，导致当前面板卸载。首修后补充原列表和原触发器 `ElementHandle.isConnected` 断言，`smtp-modal-run-02` 仍 2 通过、1 失败（31.9 秒）：原 DOM 已持续存活，关闭回焦仍失败。第二次局部修复后仅重跑原失败项，`smtp-modal-run-03` 1/1 通过（9.2 秒）；原失败材料保留，未放宽断言或重跑后端矩阵。

三个场景分轮通过的范围是：20 条/第 2 页长列表、未发送收件人输入、原 DOM 与滚动位置保留，Esc 回原触发器且普通详情开关不增加列表/配置 GET；390px 深色与 reduced-motion 输入下，一次测试 POST 取得严格受理回执后打开详情，首 GET 失败只重读，不重发 POST，关闭后输入和分页保持；重试 Unknown 时关闭必须确认，选择继续追踪保留原 method/path/key/body/version，不能发新重试覆盖原命令，显式恢复后打开新任务详情。确认写入后最多一次局部列表重读必须仍使用原 20 条与第 2 页 cursor，关闭弹窗不触发 Tab 初始读取。

这些是受控 API 下的交互证据，不证明本轮新建真实邮件、PG 幂等或外部投递。原 `system-smtp.spec.cjs` 已按弹窗作用域、关闭详情和显式清除保留输入更新恢复选择器，限定格式/语法检查通过；**没有重新运行该真实流程**，之前 18.9 秒的真实通过仍仅属于当时输入。所有本轮 Playwright/CLI 已实际退出，无新增监听或保留浏览器；既有 dev、公网服务与配置未改。完整工程检查和独立视觉结果由相应 owner 另报。

三列表统一到公共分页与列表容器后的验证以产品 `9e557cd6`、首批测试 `409b49b7` 为输入。`web/tests/system-account-pagination.spec.ts` 定向 4/4 通过（5.27 秒、exit 0）：保留原有两项请求上界/共享 HTTP 尾部测试，新增当前成功页刷新时保行、失败后同 cursor 重读，以及停用列表时退休迟到响应、保留页面、重新启用后显式读取。最后全量工程检查仍由 integration owner 唯一执行，未在这里重复整套账号 unit 或真实后台六条旅程。

[controlled-system.cjs](controlled-system.cjs) 从原 SMTP modal spec 抽出有限合成响应，供该 spec 和 [system-list-layout.spec.cjs](system-list-layout.spec.cjs) 共用。只允许精确 `http://127.0.0.1:5245`，三个列表与现有 SMTP 场景用固定合成 DTO；所有 API 都被拦截，未知 API/外部 origin 拒绝。它没有读取 runtime、建立真实会话或写入真实 SMTP。布局入口分别在 1440px 浅色和 390px 深色下检查三页有数据/空态，比较实际 Settings 主滚动区扣除边框、padding 和滚动条后的内容边界，允许 1.5px 亚像素差异；检查状态左侧、页长右侧、列表及页脚外缘、0 条状态与无横向溢出，不把字段天然留白当作容器宽度。

```sh
export NODE_PATH="$PWD/tests/account-captcha-web/node_modules"
export FRONTEND_ACCOUNT_OUTPUT="$PWD/output/ai/frontend-account-rebuild/system-list-run-01"
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-smtp-modal.spec.cjs system-list-layout.spec.cjs --list
node tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-account-rebuild/playwright.config.cjs system-smtp-modal.spec.cjs system-list-layout.spec.cjs
# 后续仅复验受影响项，另设独立输出目录：
# --grep 'three lists|closing an uncertain'
# 或仅 system-list-layout.spec.cjs --grep '390px'
```

本轮每次均先 `--list`，实际分段结果为：

- `system-list-run-01`：2 通过、3 失败（exit 1）。SMTP 普通详情同 DOM/滚动/页 2/20 条/输入/关闭回焦，以及窄屏单次发送与详情读取恢复通过。两个布局失败是测试误用邀请空态的短句 exact 匹配；改为产品原有完整文案，几何及状态断言保持原样。Unknown 场景在初配置页 5 秒内未找到“端口”，尚未进入恢复业务。
- `system-list-run-02`：2 通过、1 失败（exit 1）。1440px 三页有数据/空态全部通过；Unknown 重试的关闭确认、继续追踪、原 method/path/key/body/version 恢复及同页局部刷新通过。390px 在最后 SMTP 初配置页再次未找到“端口”，先前两页已执行，不能据此算窄屏场景通过。
- `system-list-run-03`：仅 390px 原场景 1/1 通过（20.8 秒、exit 0），消费已冻结的 SMTP 页长选择焦点局部修正；没有重复已过的四个浏览器场景。由此五个目标场景已分轮通过，页长选择焦点修正的独立复验由原 reviewer 另报。

两次初配置加载失败的根因仍未确认，分轮通过不代表该根因已修复。只读核对既有 run01/run02 材料后，确认仅有结果 JSON 与 `.last-run.json`，失败无附件、页面正文、URL、API 事件或 stdout/stderr；原配置关闭 trace/截图/默认失败 DOM，因此无法事后区分页面未挂载、登录/权限状态、配置读取错误或仍在加载。一次有限的相同初加载探针成功且未见 pageerror/失败请求/HTTP 错误，亦不能代证原失败原因。新增诊断仅在**完全合成、任何命令前**的初配置失败时保存截断的渲染正文、页面错误，以及路径/状态/耗时/计数；不记录请求头、API 正文、命令 body/key 或私有 runtime。run03 未再次触发，未放宽超时或继续盲重跑。

本轮所有作者浏览器/CLI 与有限诊断进程均已退出，无新增监听；未恢复已清理的 5250，也未改公网账号、SMTP 配置或 frp。以上仍是实际 Vue 加受控 HTTP 的证据，既有真实后端结果保留其原输入边界。

正式 Central 可恢复实例的最小独立检查入口是 [runtime-recovery-probe.cjs](runtime-recovery-probe.cjs)。它与旧 fixture 浏览器套件分开：通过精确 `https://154.12.86.233:18443` 身份、固定本机 `127.0.0.1:5256` TLS 拨号访问正式 Central，**不代表公网 frp 验证**。脚本不启动/停止服务、不读环境中的五组密钥、不改资料或 SMTP，也不做 AUTH 或测试发送。

```sh
node --check tests/frontend-account-rebuild/runtime-recovery-probe.cjs
node tests/frontend-account-rebuild/runtime-recovery-probe.cjs --help
FRONTEND_ACCOUNT_RUNTIME=/absolute/persistent-instance/private/runtime.json \
  node tests/frontend-account-rebuild/runtime-recovery-probe.cjs
```

runtime JSON 必须为当前用户所有、0600，父目录 0700 且无符号链接；字段包括 `origin,local_tls_address,central_address,source_revision,nonce,certificate_ca_file,certificate_file,certificate_spki,credentials_file,central_binary,central_binary_sha256`。目标固定为上述 origin 与 TLS 地址、Central `127.0.0.1:5257`，完整源码 revision 和 32 位十六进制 nonce 分别与实例身份端点核对。独立 credentials JSON 也须位于同一私有目录、0600，仅在内存中读取 email/password；CA 与 leaf 必须位于该实例目录，校验证书期限、签发关系、IP SAN 和实际连接的 leaf SPKI。该正式实例没有测试 fixture 的人工期限；若 manifest 提供 `expires_at` 则仍严格拒绝过期输入。

脚本先验证 TLS 与 `/__instance__/identity`、匿名系统 SMTP GET 401，再真实登录、读取当前管理员 Session 和 SMTP 安全投影。输出 `ready_for_restart` 后保留 Cookie/CSRF 仅在内存中等待；由**后端唯一 owner** 使用其管理入口只停启 Central，保留 PostgreSQL、MinIO、配置与密钥。确认重新健康后，在原 probe 的 stdin 仅输入无秘密的 `resume`。屏障最多等待 10 分钟，不轮询或自动重启。随后核验 `central-process.json` 与实际 `/proc` 的 PID/start_ticks/exe，确认进程确已更换；同 Cookie、user/session 身份与 SMTP 投影必须保留，最后 logout 204、Session 401。输出只含固定阶段、状态码、布尔结论及安全错误码；不保存 Cookie、CSRF、密码、请求正文或 trace。

目前该新入口仅 `node --check`、`--help` 与限定 Prettier 检查 exit 0；新实例真实链及一次重启结果待 owner 提供候选后执行，不能把语法/入口通过当成持久性通过。实际公网匿名实例匹配另在切换后协调一次检查，不用本机通过或 frpc 登录代替公网响应证据。
