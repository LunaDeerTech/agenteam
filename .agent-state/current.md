# 当前状态：新 Vue 账号与个人设置已实现，公网验真受阻

2026-10-16。本轮七页、新会话/客户端和个人设置已实现，指定本地真实旅程、工程检查与独立视觉/安全范围已通过。公开24小时实例与frp继续保留，但当前环境管理代理拒绝上游私有CA，尚未取得公网身份标记；不能把本机验证称为公网通过。行为和验收唯一来源为[账号合同](../docs/development/work-items/frontend-account-rebuild.md)。

## 范围、组合与所有权

- 主树 `/workspace/agenteam`、分支 `ai/frontend-vue-rebuild-20261016`；账号工作起点 `c13c2db5`。最终前端产品检查输入 `85c4ac49`，后端最终候选 `9dd3044c`，含S1认证错误不清Cookie与公开24小时模式；root已将后端增量集成主树。恢复以任务分支最新提交及实际差异为准，不回填本状态自身提交号，不宣称已合main。
- 后端树 `/workspace/agenteam-account-backend`、分支 `ai/account-entry-backend-20261016`。root负责全部Git/worktree/checkpoint及集成；内容按唯一路径owner维护，执行者不做Git写。
- 新 `web/` 的client/session/权限和页面独立编写，不导入、复制或包裹 `web-crape/`。后端成熟密码学、CSRF、事务和领域规则保留；七页为login/invite/forgot/reset与profile/appearance/password。不增加开放注册、MFA、设备管理、邮箱修改或邀请管理；其他未讨论业务正文仍为骨架。
- 登录按用户最新要求使用居中轻边框/克制阴影面板，含官方Logo、标题与表单；三项设置继续宽页头、左对齐开放表单和唯一SettingsShell。[已展示的登录面板原图](https://img.lunadeer.cn/i/2026/10/16/6ad1a3e7b18d9.png)来自约定图床，内部审查通过不等于用户最终审美接受。

| Owner | 唯一职责 |
| --- | --- |
| `/root/account_contract` | 本状态、账号合同、前端说明及授权当前范围入口；本批冻结后由root保存。 |
| `/root/vue_integration` | 新 `web/src/` 与工程配置/共享UI；产品源码已停写，保留dev进程。 |
| `/root/account_backend` | 独立后端树的account/API/装配、后端说明、真实服务与frp；当前服务保留。 |
| `/root/vue_tests` | 新账号unit/浏览器harness与测试README；作者检查结束，源码停写。 |
| `/root/vue_visual_review` | 未参与实现的安全、视觉与交互独审；最后保留最小复现probe，不扩大测试。 |

## 已打通的流程与检查边界

- 真实首链覆盖登录、受保护返回、Session/profile、刷新、退出及401；真实API边界另有一项通过。`browser-run-04`完整旅程通过受控SMTP邀请正文→兑换→普通账号登录→资料保存与409保草稿/显式采用新版本→外观预览/取消/保存/刷新→改密后当前Session200及另一旧Session401→找回与真实SMTP reset正文→重置后目标Session401→新密码登录且主题保留。
- 头像前半已实际完成SVG拒绝零上传、PNG预览/取消、真实上传存JPEG及刷新读取。后半在修正测试Node专用CA信任后定向通过：DELETE204后GET丢响应只重读、DELETE一次；真实PATCH提交后丢响应进入Unknown，冻结草稿并显式同key/body重放，服务端版本不二次递增。受控响应丢失不冒充自然网络或PG提交Unknown。
- 正式挑战测试另有实际1/1通过：连续错误密码401、合法 `commit_state=not_started` 不误入Unknown、错误proof400、真实键盘旋转验证200和同login key登录200，390px无横溢。作者早期scratch验证与正式spec运行分别保留。
- 最终源码全量unit为6文件35/35；`format:check`、类型检查及production build均exit0。defaultfalse dist的旧骨架受影响兼容3/3通过；账号真实行为用的是5245的 `VITE_ACCOUNT_ENABLED=true` dev经TLS代理，不能把false dist称为业务输入。账号作者证据为5条独立通过测试加头像前半已执行步骤，精确命令/原失败见[账号harness](../tests/frontend-account-rebuild/README.md)，基础兼容见[框架harness](../tests/frontend-rebuild/README.md)。
- 独审接受登录panel与三项设置、forgot/reset浅深主题和1440/390整页，键盘、reduced motion与原生200%已覆盖。V3外观保存焦点环裁切经12px滚动余量修复，在390与原生200%同输入复验关闭，profile/password共享样式亦通过。真实跨身份reset确认当前管理员user/session保持200、目标旧Session200→401，密码清空且fragment移除，无伪注销。
- S1旧实现真实GET/PATCH401迟到清新Cookie已修：普通认证错误不Set-Cookie，显式成功logout/换会话保持。最终24小时r2、后端 `9dd3044c` 的独立双tab浏览器复验通过：B新登录后释放A的原GET/PATCH401，两次Cookie数量和值保持、Session200；logout204后Cookie0/Session401。验证采用只允许目标IP18443的本机CONNECT固定拨5254，浏览器实际leaf SPKI和nonce匹配，保持规范Origin及完整TLS；这是本机验证，绝不代证公网frp。
- 原登录成功离页误弹、邀请初导航错误均已修并定向复验，保留原失败。头像run05因Node route.fetch缺少fixture CA导致默认失败日志含临时Cookie/CSRF，已真实logout204→Session401撤销、敏感行清理并改0600，独审复核；无trace、截图、上传或Git跟踪材料。原外层失败不改写为整例通过。

## 保留的服务、期限与公网断点

| 资源 | 当前事实与恢复位置 |
| --- | --- |
| public r2 | 目标体验URL `https://154.12.86.233:18443`，本地TLS5254终止，frp TCP18443直透；实例与唯一代理已启动并保留，尚未完成公网验真。 |
| 有效期限 | started `2026-10-16 04:24:20 UTC`；实际expires **`2026-10-17 04:23:20 UTC`**，约24小时。页面、PG、MinIO、outbound、SMTP的10张实际CA/leaf期限均已核覆盖；进程或环境退出仍可能提前结束，不是永久部署。 |
| 运行与停止 | 私有目录 `/workspace/scratch/account-preview-public-r2/`；`runtime.json`为0600原子manifest，`run-state.json`记录运行/停止，`credentials.txt`为0600最小凭据文件。密码、Cookie、CSRF和一次性链接不写入文档或普通输出。 |
| 本地作者组合 | `https://localhost:5250` → 5245新Vue dev，长驻Go测试进程内真实App+独立PG17/vector0.8.1、固定MinIO和受控SMTP；不是独立cmd/Central二进制部署。5250默认45分钟，未为public重启或改origin；恢复须核实际进程。 |
| 静态预览 | 5244/HTTP18081保持 `VITE_ACCOUNT_ENABLED=false`；公开 `/login` 的禁用输入、官方Logo和预览提示已浏览器核实，账号API请求为0。原型5241、业务dev5245及既有frp18081按授权保留。 |

公开实例不要求frps配置HTTPS/证书，也不依赖用户Nginx；现有443应用保持。临时专用CA和IP SAN会产生普通浏览器的不信任提示，用户已知；保持 `DevMode=false`、Secure Cookie和Host/Origin/CSRF校验，不修改系统信任。严格测试只信本轮专用CA/SPKI。准确启动与停止命令见[后端预览说明](../docs/development/backend/account.md)，不把历史manifest视为服务仍在。

**当前首断点是公网验证链路。** frp登录与代理启动成功，但本环境管理HTTP代理在上游TLS校验时拒绝私有CA，返回503 `CERTIFICATE_VERIFY_FAILED`；其面向客户端的IP证书还被改签为DNS SAN。没有取得公网nonce，没有经该失败链发送凭据；这些事实不证明服务器端口不通，也不证明公网可用。root已向用户异步询问直连结果；当前以此外部阻塞冻结，服务保留到上述期限，后续直接访问事实到达后补充，不无期限等待或把本机验证补写成公网通过。

SMTP仅连接本任务私有sink，邀请/重置正文链已验证，不代表外部邮箱已配置或送达。202只表示受理；SMTP配置后不回退backend_log。公开演示的恢复渠道限制继续明确，不向真实用户/外部邮箱发邮件。

旧45分钟public-r1已正常退出并完成原App/依赖清理，旧manifest/凭据已撤，不能再交给用户。作者Playwright均已退出；兼容preview5242原进程closed、exit143且无监听。新public-r2、frp和授权保留dev不收掉，工具退出与服务保留分开。

## 保存与下一步

产品与作者测试已停写；本批必要文档和独审最小probe由各owner明确冻结后交root保存/推送，WIP保存不等于main或生产发布。无需重复已通过检查。下一可见结果是用户直连或其他获授权路径取得公网身份标记及真实登录结果；环境恢复则先核实24小时资源/证书期限再重启独立轮次。用户图片继续先上传图床并提供原图链接。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
