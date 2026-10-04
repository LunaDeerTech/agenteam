# D07 账号、Session、SMTP 与个人资料

- 修订：1；状态：设计中；唯一活动模块D07，台账AT-0014。
- 基线：`main@57bfadb`，D06完整独立验收后工作区干净；D01–D06前置通过，最终证据见[D06主卡](d06-transactional-outbox.md)。GitHub认证既有阻塞未解除，不声称推送。
- 目标：实现正式人类身份/Session与System授权、初始化/邀请/密码恢复、内嵌挑战、SMTP持久投递及资料/头像/偏好；通过真实HTTP、数据库/SMTP/对象组合验证，不以生产stub代替后续领域绑定。
- 依据：[计划D07](../development-plan.md#d07-账号-session-smtp-与个人资料)、[账号](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[D01契约](d01-contracts/README.md)、[账号页面](../../frontend-design/layouts/account-entry.md)、[个人设置](../../frontend-design/layouts/personal-settings.md)、[系统设置](../../frontend-design/layouts/system-settings.md)。已确认产品规则不重复询问。

## 所有权与任务

| 任务 | 角色 | 独占写入范围与资源 | 状态 |
| --- | --- | --- | --- |
| S01 完整可实施规格 | architecture_worker | 新`d07-account-session-smtp-design.md`；其余源码/契约只读；不使用Docker | 已授权 |
| R01 工程可行性与有界技术证据 | backend_worker，承担research_worker职责 | 仓库只读；只在任务自有`/tmp`作报告及隔离实验，不修改依赖锁/源码；不使用Docker | 已授权 |
| V01 独立规格审查 | verification_worker | 仅读停止写入规格/稳定来源与独立副本 | 待S01冻结 |
| B/F 后续完整结果任务 | 按规格分配 | 公共契约、迁移、依赖、API与app等精确范围待规格采纳后授权 | 未开始 |

root独占本卡/计划/台账；设计规格独占给architecture，R01只交临时证据，不同写一文件。使用agenteam-design/go-development/verification/documentation技能，按AGENTS团队规则执行，子agent不得再委派或Git写。规格或活动实现未停写不审查；模块完成前不推进D08。

## S01交付要求

核对实际D03事务/锁、D04身份/Audit/Secret/cursor/outbound、D05对象头像组合与D06事件/真实Runtime/ProcessGuard，而非凭设计假设已有能力。明确数据表/索引/唯一性、Go公共端口/正式provider绑定、HTTP wire/OpenAPI、错误/权限/取消、事务/幂等/未知提交/重启恢复和具体测试场景。00010为下一个全局迁移候选，00001–00009及现有依赖冻结；新增依赖/旧文件改动逐项列来源、固定版本、必要性、授权作者与顺序。

先固定工程参数与兼容：Argon2id编码/升级/并发与实测参数、15–128 Unicode字符/不trim/弱密码来源；username规范/保留字/唯一并发；public origin/Cookie/CSRF/Origin/登录返回目标；空闲7天/绝对30天/重置30分钟/失败挑战5次等配置的范围与已签发边界。GoCaptcha + go-captcha-vue源码集成沿已定选择，固定版本、素材许可/存储/一次性与请求绑定/防重放/可访问性；不得增加Redis/独立验证服务/冷却登录/外部泄漏查询。

初始化admin@mail.com/admin幂等，不重启覆盖密码；首次仅建议改密；初始化unknown先核持久事实。邀请固定24小时、管理员授权、有效期内同链接重发，原子兑换/撤销/到期清除。重置统一公开回执不泄漏账号、同有效链接不续期；普通改密换发当前Session且撤销其他，重置撤销全部；HTTP立即失效，D25真实WS消费者未来绑定，不能虚称当前有WS产品。token不可逆验证、用于重发材料通过正式Secret引用加密，清理与未知提交可恢复。

SMTP三模式none/STARTTLS/TLS，复用正式非HTTP出站拨号/证书与策略，不自动降级。配置/保存/测试/业务投递分离，Secret凭据不回显；明确有限重试默认/上限、持久job与当前合法材料/配置校验、撤销/消费竞争、actual I/O join/ProcessGuard与unknown投递语义。无SMTP不阻断ready，只有未配置走正式受限后台日志；配置后失败不改渠道。初始密码/邀请/重置链接后台日志为已确认恢复渠道，需专用受限输出边界，不进入普通日志/Audit/前端/上下文；不能误套通用脱敏为完全禁止该渠道。

头像静态JPG/PNG/WebP真实解码、拒SVG/动画/炸弹、大小/像素/缩放重编码/元数据，正式对象引用替换和可恢复旧对象清理；email只读、本人username/display_name/avatar/theme，stable UserID与路径改名无alias。D07交付后具体哪些D04/D05/D06 System端口正式绑定、哪些Project接口留D08，必须逐项明确且组合验收。

遵循计划边界：D07完成后端事实、安全端口、HTTP和可消费协议；统一客户端/账号个人页面属于D26，系统设置页面属于D27。挑战包Vue兼容及真实浏览器可行性可用独立测试harness验证，不提前把临时页面作为生产账号UI。规格明确D26/D27接入责任，保留现有前端组件/Debug隔离。按完整结果拆后续B任务，避免仅按函数拆卡，所有公共载体尽量一次规划。

## R01有界核对

目标是给S01关闭已知技术未知：现有Session/System/Audit/Secret/对象/Outbox端口可组合性；Argon2id、GoCaptcha与Vue包、弱密码素材、静态图片编码及SMTP实现候选的Go1.27.1兼容、许可/固定版本/一手来源。只读仓库，不安装或改根依赖；可以下载公开源码或在独立/tmp module做有限编译/哈希基准，不读取真实凭据/连接既有基础设施，不使用Docker。记录精确输入、来源/日期/命令/结果，区分静态建议与实测；先把发现的端口/产品规则冲突交root与architecture，不自行定新产品范围。无需全面调研所有候选；选定可实现推荐并列未验证项后停止。

## 验收与完成门槛

S01须有稳定来源manifest、格式/本地链接检查、全部读写/命令停止声明，独立审查/root采纳后才实施。R01临时报告与实验须归档可引用的哈希/命令，不能把研究建议自动写成已定契约或已实现能力。

后续实现必须覆盖正常、无权限、真实并发撤销/兑换、unknown commit/SMTP未知结果、重启/关闭/资源join、公开隐私和日志安全，运行适用普通/race/vet/真实HTTP+PG+SMTP+MinIO组合。模块最终独立验证及无过滤兼容才标完成，单卡或纯接口不代表D07完成。工程细节在既定范围内自主决定，仅真正未决产品含义/实质范围变化报告root。
