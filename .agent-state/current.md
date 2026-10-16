# 当前状态：新 Vue 账号入口与个人设置实施中

2026-10-16。用户已授权先实现新 Vue 登录、找回/重置、相关接口与会话，以及全部既定个人设置，并优先保护公网入口。完整内容、接口、恢复和验收唯一来源为[本轮合同](../docs/development/work-items/frontend-account-rebuild.md)。既定邀请兑换纳入闭环，不新增开放注册、邀请管理、MFA、设备管理或邮箱修改。

## 目标与组合基线

- 主树 `/workspace/agenteam`、分支 `ai/frontend-vue-rebuild-20261016`；本轮账号工作起点 `c13c2db5`。骨架从此前已保存的 WIP 接续，现有后续提交和他人改动不回退；恢复以分支实际最新提交与差异为准，不在本状态回填自身提交号。
- 后端树 `/workspace/agenteam-account-backend`、分支 `ai/account-entry-backend-20261016`，同起点 `c13c2db5`。后端 `255bb564` 的六路径已由root集成；随后S1与public参数化修复继续在后端树推进，不把已集成输入与新候选混称同一版本。
- 新 `web/` 从零建立客户端、Session、权限和业务状态，不导入、复制或包裹 `web-crape/` 的实现；成熟后端领域、密码学、CSRF、事务和会话安全规则保留。现 Account HTTP 已覆盖七页，首版保留实际 wire，按真实需求做必要兼容/安全修正。
- 目标为登录、邀请、找回、重置和 profile/appearance/password 七页完整实现；当前七页/头像已有可调用实现，完整账号旅程已分轮通过，头像/Unknown等定向范围仍待验证。其他正文维持骨架。设置继续宽页头、靠左开放表单与唯一SettingsShell；用户最新要求登录加框，已落为仅login的居中轻边框/克制阴影面板，包含官方Logo、标题与表单。该版独审中，新截图待上传，不当作用户最终视觉接受。

## 会话与所有权

`/root` 是唯一协调会话，负责全部 Git、分支/worktree、checkpoint/push及最终集成，执行者不做 Git 写。当前工程与测试紧耦合共享主树、按路径单写；后端独立树。原 `vue_docs` 已完成停止，当前状态已移交 `account_contract`。

| Owner | 唯一写入范围/职责 |
| --- | --- |
| `/root/account_contract` | 本状态、本轮合同、必要前端说明、`web/README.md`、任务台账和设计入口当前范围；不写产品源码/API schema/后端说明。 |
| `/root/vue_integration` | 新 `web/src/`、工程配置/锁、共享 UI、styles、theme、路由、client/session与页面；作者检查和首链联调。 |
| `/root/account_backend` | 独立后端树中的account/API/装配、后端说明及本轮真实服务能力；资源由其下属fixture owner按实际隔离。 |
| `/root/vue_tests` | 新账号unit/浏览器harness及对应测试README，具体范围以root续派为准。 |
| `/root/vue_visual_review` | 未参与本轮实现的安全与视觉/交互独审，按root实际派工恢复；默认只读，材料放忽略目录。 |

合同子任务 `account_surface_audit` 已只读完成并结束，未改文件或运行测试。运行时席位有限，独审按root实际恢复安排，不把计划实例写成已启动。团队要求模型/推理档位沿仓库规则；工具无service-tier参数时不虚报已配置Fast。

## 当前流程、首断点与下一结果

- 已完成最小契约：七页内容、真实提供方/调用方、端点/DTO/CSRF/幂等、单一实际账号请求owner、身份代际、确认后再读与Unknown、头像/主题/密码及邀请恢复边界。前后端owner已收到，可并行实现，无需等待全部文档。
- 已打通5250真实首链：受保护返回 → 登录 → Session确认/profile → 刷新 → 注销 → 401。首轮成功跳转误触发离页保护的失败已定向修复，修后只重跑首链通过；真实API边界1项独立通过，不能合称一次全量运行。
- 修复公开邀请页初导航被匿名监听跳回login后，`browser-run-04` 完整旅程1/1、31.4秒exit0：受控SMTP邀请正文 → Vue兑换 → 普通账号登录 → 资料保存与409保草稿/显式采用新版本 → 外观预览/取消/保存/刷新 → Vue改密后当前Session200、另一旧Session401 → Vue找回和真实SMTP reset正文 → 重置后目标当前Session401 → 新密码登录且深色偏好保持。原首链离页误弹与邀请初导航两次失败保留；不称外部SMTP或公网验收。当前下一个未验范围为头像与Unknown定向场景、S1最终修复和公网入口。
- S1独审已真实复现另一标签页新登录后，旧GET401及PATCH `/me` 认证401迟到清除新Cookie。已确定后端窄修为普通认证错误一律不Set-Cookie，保留显式成功logout与成功换会话；safe-read候选三个pure已过，扩大至unsafe错误的最终编译与独审复验尚未闭合。5250 r1仍使用旧后端输入，未重启，不能把候选修复写为该服务已生效。
- 同标签页全部Account请求仍经一个实际owner至body/取消尾部结束，跨标签Cookie边界由后端另保。登录/改密成功后再读Session失败不重复POST；HTTP无命令lookup，Unknown只按原意图与有效上下文显式重放，改密响应丢失可能必须重新登录。

## 公网与恢复渠道

- 用户最新选择本地独立TLS5254终止HTTPS，frp TCP直透公网18443，固定PublicOrigin为 `https://154.12.86.233:18443`，证书含IP SAN。后端已通过DNS与IP/域名18081同页200及相同SHA确认IP；不要求frps配置HTTPS/证书，不依赖用户Nginx或远端管理通道，现443应用和18081保持。临时自签证书的浏览器不信任提示已告知用户，不能宣称公开CA信任。
- public5254目前只有参数化入口、精确origin allowlist及专用CA/IP SAN支持，尚未启动、无frp18443映射、未公网验真。候选编译始于safe-read S1输入，最终unsafe范围仍在更新，不能据早期编译宣称最终版本就绪。启用前须确认响应确属本轮服务、安全Cookie及真实登录旅程；禁止DevMode/去Secure或向HTTP18081发送账号材料。
- 本地r1为 `https://localhost:5250` → 新Vue5245，真实Central+本任务独立PG17/vector0.8.1、固定MinIO和受控SMTP sink已启动，私有manifest位于 `/workspace/scratch/account-preview-runtime-r1/runtime.json`（0600）。前端启用 `VITE_ACCOUNT_ENABLED=true`；所有浏览器测试统一消费5250，无额外5252监听。服务/资源由后端owner维护，测试owner只启动Playwright；恢复时重新核实进程和nonce，不凭历史端口判定仍运行。
- SMTP只连接本任务受控sink，邀请201与正文取回已有真实链，不证明外部邮箱送达。可选私有正文通道仅供受控harness，敏感材料不进日志/源码。请求202只代表受理，smtp不承诺送达，backend_log保持真实受限渠道；SMTP配置后不回退日志，未另授权不向真实用户/外部邮箱发送邮件。
- 旧HTTP静态体验地址为 `http://mcpc-terra.lunadeer.cn:18081/system/users`，5244默认 `VITE_ACCOUNT_ENABLED=false` 不发账号API；新本地业务dev使用5245。原型5241/静态5244/frp保留要求继续有效，不覆盖其他映射。历史公网HMR经本环境代理被403拒绝，不能推断用户直连结果。

## 可复用的已验范围与检查限制

- 本轮新增受控测试：transport10/10、session7/7已过；Session上下文代际改动后7项定向复验仍exit0。真实浏览器首链、API边界、完整旅程各1项分轮通过，头像与Unknown定向未验，命令与原失败见[账号harness](../tests/frontend-account-rebuild/README.md)。这些unit不代证浏览器Cookie或真实PG；S1有独立真实反例，尚未复验关闭。
- 前端owner报告登录panel版整体typecheck通过，随后token同页/身份失效变更仍待下一组合检查；头像子owner四文件Prettier通过并停写。这些局部结果不代当前整个工作树最终check或视觉接受。
- 账号工作前的共享框架、八控件、AppShell/AccountShell/单一SettingsShell和40叶空正文已有作者与独立检查：unit14/14；浏览器12场景以首链1+控件4+路由7的分轮结果组合通过；不是一次最终全量。390px横滚前提失败已改用320px真实溢出并定向通过；原失败保留。
- 官方四个浅深SVG已原样放入 `web/src/assets/brand/`，长项目名在390px挤压owner/箭头的问题已定向修复并独审。框架浅深/桌窄、键盘/回焦、低高度、reduced motion及原生200%均有原输入证据；这些结果不代证本轮业务、后端/Session或用户最终审美接受。
- 新旧隔离此前静态核对：归档569文件blob/执行位一致，新lock独立179包，40叶、11/3/11设置与3重定向；当时源码无旧client/session/权限导入。随后账户实现须重新检查实际影响范围，不把此前“全部正文为空/无API”误写成当前永久事实。
- Node24.19.0/npm11.9.0、Playwright1.56.1与Chromium151.0.7922.173此前可用；依赖按新锁恢复，不链接旧前端依赖。运行说明见[web](../web/README.md)，骨架harness见[说明](../tests/frontend-rebuild/README.md)。账号检查按本轮合同与测试owner新harness。
- 文档只作相关链接、格式和一致性自查，不代跑产品测试。日志、截图、dist、缓存放忽略目录；用户展示图片先上传配置图床，提供公网图与原图链接。

## 检查点与下一步

root已保存并推送本轮前端/合同/测试阶段检查点，最新测试两文件为 `396bc2ff`；恢复以任务分支最新提交和实际差异为准，不在本状态抄写全部提交序列。接下来完成头像/Unknown定向检查、S1最终候选及独审复验，再完成public5254/frp18443实际接入与公网验真。owner约每5分钟或形成可恢复成果时停止相应写入报告root；本状态冻结后才随checkpoint保存。Git保存持续获授权，WIP不代表PASS或main交付。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
