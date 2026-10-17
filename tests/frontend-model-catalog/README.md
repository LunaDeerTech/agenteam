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
- `catalog-ui.spec.cjs`：新 family 组合的页面旅程；同一供应商与凭据，手工模型编辑、发现后逐模型选择 chat/embedding 并导入、第一项真实提交后停止后续导入再继续、以“供应商/模型 ID”定位两个真实 adapter 测试、上游失败和 UI 显式恢复原测试请求。旧双协议组合不能执行或记作本场景通过。
- `selection-ui.spec.cjs`：正式 API 只创建独立模型前置，用户界面完成供应商→模型选择、禁用缺少 json_schema 的总结模型，在 embedding/summary/reranker/image 四项表单中点击唯一“保存”。断言只产生一个用途 PUT、单一资源版本加一，当前值和候选显示“供应商/模型 ID”，刷新保留；删除总结模型时先取消并验证零变化，再选兼容替代模型，核唯一总结引用替换持久化。全程不需要模型出网。
- 统一用途须包含迁移149和对应前后端组合：GET/PUT `/system/model-selection` 使用一个 id/version/receipt，Memory 与 Meeting 共用 `summary`；不再请求独立 meeting-summary 资源。真实 PG 的迁移重置、两消费者共用解析、Commit Unknown 与引用一致性由对应后端 selector 验证，浏览器场景不冒充这些内部门。原两资源界面的通过证据不算新合同已验；`--list` 只确认可选择场景，实际结果须在新组合运行后记录。
- 真实页面旅程在新 Vue 对应控件就绪后增量加入。发现和试调用必须经过真实受控模型 HTTP 服务；不能用浏览器全 API mock 声称实际出网成功。
- `GET /__fixture__/provider` 要求 `X-Fixture-Nonce` 为本轮 nonce，只返回请求/认证命中/发现/聊天/向量计数，不返回认证头、正文或 Secret。CRUD 和用途选择应为零新增模型请求。断言副作用计数的测试共享一个目标，按独占写窗口运行，避免把另一场景的请求计入本场景。

模型目标解析只允许 `models.fixture.test` 和本轮 SMTP sink；正式出站规则只授权模型 loopback 的单个随机端口，产品鉴权和策略判断仍执行。fixture 不是公网服务，也不提供通用代理。

## 收尾

创建本轮私有目录中的 `stop`，或只向实际登记的 launcher 发 SIGTERM。launcher 等原测试链实际退出；fixture 先 join App/TLS/provider，再由正式 helper 回收精确 nonce 容器、网络与目录。记录原 wait/exit 和实际资源退出，不能以终止信号替代完成。

同一组合的 `app.test` 可供另一个 owned fixture 只读复用，使用正式 `AGENTEAM_FIXTURE_TEST_BINARY` / `AGENTEAM_FIXTURE_TEST_CWD` 接口并登记 SHA。共享前显式以 `AGENTEAM_MODEL_CATALOG_KEEP_BINARY=1` 启动，确保 preview 成功或失败退出均保留这一原路径；全部消费者实际退出后由 owner 精确删除。本开关不保留运行中的 fixture，也不跳过清理；默认仍删除 binary。不同测试不共享数据库、策略或私有运行目录。

App 启动失败时，fixture 在原进程 join 后、owned DB 清理前记录 `startup-failure-safe.json`（0600），只包含固定事件/阶段、fault code、PG SQLSTATE、schema 及管理表存在布尔；不保存原日志或错误正文。原来没有该观察字段的失败不能事后补写为已知原因。

每条浏览器旅程最后在 owned identity 文档退出 Session 并确认401；编译、`--list`、skip 和 ready 均不算业务测试通过。既有通过证据只按未变化输入复用，产品或 fixture 首失败分别记录，再做限定修复。
