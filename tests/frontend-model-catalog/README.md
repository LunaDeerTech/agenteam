# 模型接入与平台模型用途的真实联调

本目录只访问本轮 owned fixture，不读取或修改用户的 5245/5256/5257/18443 实例、外部模型凭据或 SMTP 配置。API 使用真实 App、PostgreSQL、Secret 和正式权限/CSRF；受控目标仅替代外部模型协议服务器。原始请求、凭据、Cookie 和响应正文不进入报告。

## 启动

前端 owner 在新 Vue 工作树启动 5281，显式设置 `VITE_ACCOUNT_ENABLED=true`。测试树提供独立 App 5282、严格 TLS 同源入口 `https://localhost:5283`、随机端口模型 TLS 服务和独立 PG/MinIO/SMTP sink。端口冲突时停止，不能占用其他会话的服务。

从测试工作树根运行：

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/workspace/toolchains/minio-account-20261016/bin/minio \
GOMAXPROCS=2 \
sh tests/frontend-model-catalog/run-owned.sh
```

脚本打印本轮 0700 私有目录。fixture 先验证实际 App 登录、Session、CSRF、TLS 和退出，然后发布 0600 `runtime.json` 与 `model-owned.json`。浏览器必须等到 ready；文件路径不是服务存活证明。普通测试账号通过真实邀请、受控 SMTP 收取和兑换建立，邮件仅到该测试 sink。

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
NODE_EXTRA_CA_CERTS=/absolute/private/certificate-ca.pem \
MODEL_CATALOG_RUNTIME=/absolute/private/runtime.json \
MODEL_CATALOG_OUTPUT=/absolute/private/browser-output \
node /workspace/agenteam/tests/account-captcha-web/node_modules/@playwright/test/cli.js \
test --config tests/frontend-model-catalog/playwright.config.cjs --list
```

确认实际 selector 后去掉 `--list` 执行。复用已锁定 Playwright 1.56.1 与系统 Chromium，worker1、retries0，无 trace、截图、视频或默认失败 DOM。Node 先用专用 CA 和 hostname 严格验证实时 leaf/nonce；Chromium 的专属 SPKI 与实时 CDP 证书必须匹配相同 owned leaf 后才使用合成凭据。

`NODE_EXTRA_CA_CERTS` 必须在启动 Node 前指向该 runtime 的 CA 文件，供 `route.fetch` 的真实响应转发使用；Chromium 的 SPKI 验证不为 Node 请求增加信任。管理与完整目录场景会校验此输入，禁止全局忽略 TLS 验证或用伪造响应绕过转发。

## 分阶段范围

- `api-smoke.spec.cjs`：在浏览器的同源文档中调用真实 API，核 CRUD、凭据安全投影、原 key 回执、版本冲突、分页、单一 summary 用途的版本与回执重放、旧 memory 字段/独立 meeting-summary 接口拒绝及删除替换影响，以及普通身份403/缺CSRF403。它证明正式 API 接线，不代替页面控件旅程。
- `management.spec.cjs`：须包含模型管理接线及迁移147的组合；发现和试调用到真实受控 TLS 目标，验证终态重放不重复请求、上游拒绝，以及受理完成后响应丢失再用原 key/body 恢复。响应丢失由浏览器只丢弃已经完整取得的正式响应，不替换 API 结果。
- `catalog-ui.spec.cjs`：新 family 组合的页面旅程；在创建 modal 中填写 API 端点与凭据，成功后自动进入 `/system/models/:providerId?tab=config` 的内联配置，读取正式 API 确认供应商与凭据已保存，再切到“模型”Tab。随后手工模型编辑、发现后逐模型选择 chat/embedding 并导入、第一项真实提交后停止后续导入再继续、以“供应商/模型 ID”定位两个真实 adapter 测试、上游失败和 UI 显式恢复原测试请求。旧双协议组合不能执行或记作本场景通过。
- `picker-api.spec.cjs`：跨两个供应商的真实列表分页与规范化 q 查询，核安全 provider 五字段投影、凭据不泄漏、改变 q/provider/list-kind 的游标拒绝、匿名401和普通用户403；无模型出网。
- `selection-ui.spec.cjs`：正式 API 创建两个供应商、13个独立模型，超过真实 picker 的10项页长。用户界面通过锚定浮层的服务端搜索及下一页找到首页之外的总结模型，跨供应商选择只改草稿，再选择向量模型并点击唯一“保存”；断言一个用途 PUT、版本加一和刷新保留。缺少 json_schema 的总结候选禁用；删除已引用总结模型时先取消，再用同一全局 picker 选择另一供应商的替代模型，核引用替换持久化。全程不需要模型出网。
- 统一用途须包含迁移149和对应前后端组合：GET/PUT `/system/model-selection` 使用一个 id/version/receipt，Memory 与 Meeting 共用 `summary`；不再请求独立 meeting-summary 资源。真实 PG 的迁移重置、两消费者共用解析、Commit Unknown 与引用一致性由对应后端 selector 验证，浏览器场景不冒充这些内部门。原两资源界面的通过证据不算新合同已验；`--list` 只确认可选择场景，实际结果须在新组合运行后记录。
- 真实页面旅程在新 Vue 对应控件就绪后增量加入。发现和试调用必须经过真实受控模型 HTTP 服务；不能用浏览器全 API mock 声称实际出网成功。
- `GET /__fixture__/provider` 要求 `X-Fixture-Nonce` 为本轮 nonce，只返回请求/认证命中/发现/聊天/向量计数，不返回认证头、正文或 Secret。CRUD 和用途选择应为零新增模型请求。断言副作用计数的测试共享一个目标，按独占写窗口运行，避免把另一场景的请求计入本场景。

模型目标解析只允许 `models.fixture.test` 和本轮 SMTP sink；正式出站规则只授权模型 loopback 的单个随机端口，产品鉴权和策略判断仍执行。fixture 不是公网服务，也不提供通用代理。

## 统一总结模型的限定运行

先按上文导出 `NODE_PATH`、`NODE_EXTRA_CA_CERTS`、`MODEL_CATALOG_RUNTIME` 和本轮私有 `MODEL_CATALOG_OUTPUT`，在已就绪的新组合中依次执行：

```sh
node /workspace/agenteam/tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-model-catalog/playwright.config.cjs api-smoke.spec.cjs --grep 'owned production catalog'
node /workspace/agenteam/tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-model-catalog/playwright.config.cjs selection-ui.spec.cjs
node /workspace/agenteam/tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-model-catalog/playwright.config.cjs catalog-ui.spec.cjs
```

API/UI 全部结束后，[独立并发检查](summary-security.cjs) 使用已经有效配置的同一 owned 实例，独占 API 写入窗口：

```sh
NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules \
MODEL_CATALOG_RUNTIME=/absolute/private/runtime.json \
SUMMARY_SECURITY_RUN=1 node tests/frontend-model-catalog/summary-security.cjs
```

该检查验证同版本并发只有一个提交、原 key 异 body 拒绝与注销后重放拒绝。收尾使用合法的新版本恢复原配置内容，删除自己的模型副本并退出两个 Session；不把配置内容恢复称为版本回滚。实际执行证据由独立 owner 记录，不与页面测试混称。

2026-10-17 的实际组合为 App/fixture `5edd81b1` 与 Vue `0852da63`：三条原 CLI 分别 exit0，1 case 通过，耗时 36.2s、11.4s、20.3s。每条都经过严格 CA/leaf/nonce、真实登录及退出204→401、浏览器关闭。这里是新库 schema149 的 API/UI 证据；旧配置重置、Memory/Meeting 两消费者、持久 Commit Unknown 与独立并发门由对应 PG/App/安全检查分别报告，不由这三条替代。未部署公网，未使用用户数据或真实外部模型服务。

## 供应商配置页签的 harness 随动

2026-10-17 按 Vue 候选 `4eafeeee` 同步创建入口：表单“服务地址”现为“API 端点”，创建成功即关闭 modal 并进入配置 Tab，不再等待“完成”后点击列表名称。名称入口仍为 `/system/models/:providerId`，默认模型 Tab；列表“配置”进入 `?tab=config`，已有供应商属性和凭据由同页内联编辑。正式目录旅程保留原凭据持久化及后续业务断言，只调整导航/定位。

本次仅执行语法、格式与锁定 CLI `--list` 检查，未启动 App/PG 或重跑真实目录旅程；新增界面的实际浏览器结果由作者和独立视觉检查分别报告。上方旧真实链结果仍保持其原输入，不能记作新页签组合已通过。

## 全局模型 picker 的限定运行

本轮仅运行新增列表差量和受影响用途旅程，不重跑既有目录发现、Commit Unknown、Memory/Meeting 或 ColdRecovery 矩阵。先以同一已就绪 owned 实例和上文私有环境变量运行：

```sh
node /workspace/agenteam/tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-model-catalog/playwright.config.cjs picker-api.spec.cjs
node /workspace/agenteam/tests/account-captcha-web/node_modules/@playwright/test/cli.js test --config tests/frontend-model-catalog/playwright.config.cjs selection-ui.spec.cjs
```

后端真实 PG 查询测试位于独立 `tests/model` Go 包，不能把 App 测试二进制当作该包的执行产物。独立视觉检查由 `picker-visual.cjs` owner 使用受控 UI 数据完成，与上述真实 API 证据分开，浏览器窗口串行。

2026-10-17 实际 App 输入为 `d1c45c42`（后端 `d95dffb2`），最终 Vue 为 `1b8bd205`。API 原 CLI exit0，1 case 11.396s；最终用途 UI 原 CLI exit0，1 case 23.854s，完成13模型跨供应商服务端搜索/分页、只改草稿、唯一 PUT/version+1、刷新与引用替换；模型目标请求增量为0。两条均经严格 TLS/leaf/nonce、真实登录及退出204→401、浏览器关闭。作者真实 PG 查询和独立视觉检查按各自实际结果单独记录，不重复旧目录/Unknown/Cold 矩阵。

UI 的前三次运行分别在 CDP 响应体读取或 Playwright `finished()` 观测处失败，均未保存配置；一次只读诊断实际看到 UI 已解析正确结果、页面内原生查询完整返回，而工具 body/finished 状态失配。独立源码审查确认 Playwright 1.56.1 的失败事件与客户端 finished promise 存在完成语义差异；没有证明具体网络失败原因或 CDP detach 为根因。最终场景用精确 GET/q/cursor/状态、真实候选与分页状态、相同 cursor 的同源只读 GET 和后端持久事实验收，不伪造响应、不补发写命令；随后发现并纠正了一处旧“图像模型”定位器，现与产品“图像生成模型”一致。全部原失败保留，不能称首次全绿。

本轮 fixture 原 launcher 实际 exit0，test 与正式 chain 的原 wait 均为0；4个专属容器、4个网络和4个目录全部不存在，5282/5283无监听，测试二进制在原 SHA 与零消费者核对后删除。Vue5281由前端 owner 另行精确退出。保留必要私有记录，未操作公网或用户数据。


## 收尾

创建本轮私有目录中的 `stop`，或只向实际登记的 launcher 发 SIGTERM。launcher 等原测试链实际退出；fixture 先 join App/TLS/provider，再由正式 helper 回收精确 nonce 容器、网络与目录。记录原 wait/exit 和实际资源退出，不能以终止信号替代完成。

同一组合的 `app.test` 可供另一个 owned fixture 只读复用，使用正式 `AGENTEAM_FIXTURE_TEST_BINARY` / `AGENTEAM_FIXTURE_TEST_CWD` 接口并登记 SHA。共享前显式以 `AGENTEAM_MODEL_CATALOG_KEEP_BINARY=1` 启动，确保 preview 成功或失败退出均保留这一原路径；全部消费者实际退出后由 owner 精确删除。本开关不保留运行中的 fixture，也不跳过清理；默认仍删除 binary。不同测试不共享数据库、策略或私有运行目录。

App 启动失败时，fixture 在原进程 join 后、owned DB 清理前记录 `startup-failure-safe.json`（0600），只包含固定事件/阶段、fault code、PG SQLSTATE、schema 及管理表存在布尔；不保存原日志或错误正文。原来没有该观察字段的失败不能事后补写为已知原因。

每条浏览器旅程最后在 owned identity 文档退出 Session 并确认401；编译、`--list`、skip 和 ready 均不算业务测试通过。既有通过证据只按未变化输入复用，产品或 fixture 首失败分别记录，再做限定修复。
