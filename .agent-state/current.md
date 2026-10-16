# 当前状态：新 Vue 账号入口与个人设置实施中

2026-10-16。用户已授权先实现新 Vue 登录、找回/重置、相关接口与会话，以及全部既定个人设置，并优先保护公网入口。完整内容、接口、恢复和验收唯一来源为[本轮合同](../docs/development/work-items/frontend-account-rebuild.md)。既定邀请兑换纳入闭环，不新增开放注册、邀请管理、MFA、设备管理或邮箱修改。

## 目标与组合基线

- 主树 `/workspace/agenteam`、分支 `ai/frontend-vue-rebuild-20261016`；本轮账号工作起点 `c13c2db5`。骨架从此前已保存的 WIP 接续，现有后续提交和他人改动不回退；恢复以分支实际最新提交与差异为准，不在本状态回填自身提交号。
- 后端树 `/workspace/agenteam-account-backend`、分支 `ai/account-entry-backend-20261016`，同起点 `c13c2db5`。后端候选由 root 完成 Git 集成后再共同验证。
- 新 `web/` 从零建立客户端、Session、权限和业务状态，不导入、复制或包裹 `web-crape/` 的实现；成熟后端领域、密码学、CSRF、事务和会话安全规则保留。现 Account HTTP 已覆盖七页，首版保留实际 wire，按真实需求做必要兼容/安全修正。
- 登录、邀请、找回、重置和 profile/appearance/password 七页完整实现；其他未讨论正文维持原骨架。继续已确认宽页头、靠左开放表单、唯一 SettingsShell 和 `docs/logos/` 官方品牌，不自动迁回 Agent/项目正文。

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
- 首条目标链：受保护页 → session401 → 登录 → session200 → profile → 注销 → 重新登录入口。当前首断点是新Vue实现与本轮隔离真实服务尚未完成组合，未宣称账户旅程已通过。
- 后续链：真实邀请兑换、找回/一次性重置，全部个人资料/头像、外观保存/取消、改密与其他Session撤销。测试和独审须针对新工程，不复用旧前端作为本轮结果。
- 已知必要安全接缝：当前受保护GET的401也可能清Cookie，故全部Account HTTP包括头像读取经过同一实际owner；登录/改密成功后再读Session失败不能重复POST；HTTP没有命令lookup，Unknown只按原意图与仍有效上下文显式重放，改密响应丢失可能必须重新登录。

## 公网与恢复渠道

- 用户已明确保留现有443应用，给同域名增加独立HTTPS端口18443；目标PublicOrigin为 `https://mcpc-terra.lunadeer.cn:18443`，使用该origin根路径。443根入口实测返回302至 `/login?next=%2F`，本轮不接管。18443尚未部署，真实服务器Nginx应用属于外部依赖，后端owner准备可审核配置；公网入口、安全Cookie与真实旅程未验证，本地隔离联调继续。
- HTTPS及真实安全Cookie未验证前，不把账户后端接到HTTP18081，不用DevMode/去Secure绕过。先在本任务自有loopback隔离环境验证真实服务。公网经frp验证返回确属本轮服务及真实登录旅程后才交地址，并保留体验进程。
- SMTP提供方和本轮配置由后端核查。请求202只代表受理，smtp不承诺送达；backend_log是已有受限恢复渠道，不冒充邮件服务。未另获授权不向真实用户/外部邮箱发送邮件，凭据/恢复链接不入普通日志、文档、截图或报告。
- 原静态体验地址为 `http://mcpc-terra.lunadeer.cn:18081/system/users`，此前报告dev5244与frp有效、页面与本地index一致；本状态不据旧PID承诺进程仍在。原型5241/静态dev5244/frp保留要求继续有效，变更入口前由服务owner核实占用，不覆盖其他映射。历史公网HMR经本环境代理被403拒绝，不能推断用户直连结果。

## 可复用的已验范围与检查限制

- 账号工作前的共享框架、八控件、AppShell/AccountShell/单一SettingsShell和40叶空正文已有作者与独立检查：unit14/14；浏览器12场景以首链1+控件4+路由7的分轮结果组合通过；不是一次最终全量。390px横滚前提失败已改用320px真实溢出并定向通过；原失败保留。
- 官方四个浅深SVG已原样放入 `web/src/assets/brand/`，长项目名在390px挤压owner/箭头的问题已定向修复并独审。框架浅深/桌窄、键盘/回焦、低高度、reduced motion及原生200%均有原输入证据；这些结果不代证本轮业务、后端/Session或用户最终审美接受。
- 新旧隔离此前静态核对：归档569文件blob/执行位一致，新lock独立179包，40叶、11/3/11设置与3重定向；当时源码无旧client/session/权限导入。随后账户实现须重新检查实际影响范围，不把此前“全部正文为空/无API”误写成当前永久事实。
- Node24.19.0/npm11.9.0、Playwright1.56.1与Chromium151.0.7922.173此前可用；依赖按新锁恢复，不链接旧前端依赖。运行说明见[web](../web/README.md)，骨架harness见[说明](../tests/frontend-rebuild/README.md)。账号检查按本轮合同与测试owner新harness。
- 文档只作相关链接、格式和一致性自查，不代跑产品测试。日志、截图、dist、缓存放忽略目录；用户展示图片先上传配置图床，提供公网图与原图链接。

## 检查点与下一步

本轮合同与状态首批已可保存；owner约每5分钟或形成可恢复成果时停止相应写入报告root，由root checkpoint/push。Git保存持续获授权，WIP不代表PASS或main交付。接下来完成首条真实链、基础失败与安全检查后尽早联调；独审、页面完整性及公开HTTPS按真实依赖滚动关闭，未完成范围如实保留。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
