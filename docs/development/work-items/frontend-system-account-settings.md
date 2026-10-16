# 新 Vue 系统设置：用户与账号

2026-10-16。用户已接受中性炭灰深色主题，并选择先完整实现系统设置中的用户、待注册邀请、账号安全和SMTP四页；本轮从 `cd1a5df6` 接续。本工作项是四页行为、接口与验收的唯一合同，实际进度见[当前状态](../../../.agent-state/current.md)。此前[账号工作项](frontend-account-rebuild.md)的“不含邀请管理”是其原交付范围，本轮邀请管理已获新授权。新Vue继续独立实现，不移植 `web-crape` 客户端、Session、权限或业务代码，不继承旧UI文件库存、固定页长或旧验收结论。

## 1. 页面与完成结果

| 路由 | 用户任务与真实结果 | 当前依赖 |
| --- | --- | --- |
| `/system/users` | 管理员分页查看姓名/邮箱、用户名、角色和注册时间；确认邀请注册后的新用户出现。只读，无角色切换、禁用、删除、代改资料或他人项目入口。 | 已装配Account用户目录。 |
| `/system/invitations` | 查看有效待注册邀请及最近投递；创建、重发邀请、人工重试投递、确认撤销；明确受理、实际尝试和未确认请求的区别。 | Account邀请、精确latest_delivery投影及邮件retry。 |
| `/system/account-security` | 编辑Session期限、重置链接期限和挑战阈值；保存/取消，冲突保草稿并明确采用当前配置。 | Account settings singleton及版本写。 |
| `/system/smtp` | 同页“配置 / 测试与任务”两区，默认配置。保存/停用传输配置，以单密码框保持或替换凭据；显式测试、读取任务详情、人工重试。 | SMTP singleton、Secret及正式邮件management读口。 |

系统设置仍保留既定11栏目；Providers、模型、平台模型用途、Runner、出站规则、系统审计、运行信息七页继续骨架，本轮不接正文。它们已有蓝图与后端能力，后续按完整流程接入，不因本轮选择另增栏目。现有四个公开账号入口及个人设置继续使用已实现能力。

四页共用已确认的设置整框、宽页头与左对齐开放正文；用户/邀请为目录，安全/SMTP配置为表单，任务详情就近展开或进入独立内容区，返回保留来源。页面级动作放在标题同一行右侧：用户页刷新，邀请页刷新/邀请；窄屏按空间重排。description可选，标题已足够时不添加解释性副标题；删除用户/邀请的常规介绍与SMTP配置的常驻技术说明，必要操作影响、字段帮助、错误和恢复提示仍就近保留。表单保存/取消仍靠表单，不随页级动作上移；本次不批量删除其他页面的真实内容。正式Logo和已接受深色tokens保持，技术ID/版本只在必要详情中显示。`/system`仍默认用户页；普通用户直链显示无权限，不挂载管理正文或发管理请求。

## 2. 最小 HTTP 合同

以下路径统一前缀 `/api/v1/system`，均要求当前管理员；写使用当前Session CSRF和稳定 `Idempotency-Key`。精确类型见[Account OpenAPI](../../../api/openapi/account.json)、[common](../../../api/openapi/common.json)及实际 `internal/central/account/http_system.go`，不新建同义API。

| 方法与路径 | 输入 | 严格成功及含义 |
| --- | --- | --- |
| GET `/users` | `limit,cursor?` | 200 `{items:SystemUser[],next_cursor?}`；SystemUser为既有User加canonical UTC `created_at`。 |
| GET `/invitations` | `limit,cursor?` | 200 `{items:Invitation[],next_cursor?}`；仅仍有效邀请，每行含精确 `latest_delivery`。 |
| POST `/invitations` | `{email}` | 201 `{id,job_id,version}`；邀请/投递意图受理，不代表送达。 |
| POST `/invitations/{id}/resend` | `{version}` | 202同形InvitationReceipt；使用Invitation ID/version。 |
| POST `/invitations/{id}/revoke` | `{version}` | 204空body；确认链接撤销，不是DELETE。 |
| GET/PUT `/account-settings` | GET无body；PUT `{version,session_idle_seconds,session_absolute_seconds,password_reset_seconds,challenge_after_failures}` | 200 Settings；PUT是匹配原命令的历史配置结果。 |
| GET `/smtp` | 无body | 200安全SMTP当前配置；不返回密码/Secret引用。 |
| PUT `/smtp` | version、传输字段、`credential_action`、两个重试值；可带非空password | 200 `{applied_version,settings}`；原命令确认与当前配置分别解释。 |
| POST `/smtp/unconfigure` | `{version,auto_retry_count,retry_interval_seconds}` | 200 SMTPResult；清传输/凭据，保留明确提交的重试策略。 |
| POST `/smtp/test` | `{recipient}` | 202 `{job_id}`；测试任务受理。 |
| GET `/mail-jobs/management` | `limit,cursor?` | 200 `{items:MailJobManagement[],next_cursor?}`。 |
| GET `/mail-jobs/{id}/management` | 无body | 200精确目标的安全管理详情。 |
| POST `/mail-jobs/{id}/retry` | `{version}` | 202 `{job_id,version}`；新的投递周期，使用源Job ID/version；返回版本不保证为1。邀请页同样使用此口。 |

版本、计数、秒数遵common canonical十进制字符串，版本比较/后继不得经过JS Number；SMTP端口独用整数。ID、UTC微秒时间、闭合DTO/null/可选字段及成功状态需严格校验。分页接口只支持cursor/limit，没有搜索、角色筛选或总数。后端允许limit1–100；新Vue先以10项试用，按本次limit核响应，25只是后端默认值和旧UI选择。cursor不透明，仅保留于当前身份/页面内存；上一页重读原cursor，成功后才推进位置，刷新回首项，不自动补抓全量或把页内过滤称全库筛选。`CURSOR_INVALID`提供显式回首项。

在现有新Account传输上增加受限的分页参数编码和系统端点，不开放任意query透传；继续同源、no-store、拒redirect和真实body/cancel结束后释放owner。保持当前1MiB JSON/Problem响应界限；普通JSON写16KiB，只有正式PUT `/system/smtp`允许32KiB，按最终UTF-8序列化字节核算，不能整体放宽其他账号写预算。

## 3. 页面规则

**用户目录。** 显示名为空回落邮箱，角色显示管理员/普通用户；注册时间标明UTC、可见至秒，完整微秒时间可访问。空、加载、失败、分页及刷新各有真实反馈；分页控件状态改变后焦点仍可用并播报完成，不落到无任务上下文的BODY。其他用户字段是文本，不借目录建立代操作权限。

**邀请。** 创建只填写邮箱，不输入密码、角色或期限；沿既有邮箱验证，不偷偷trim。邀请从首次创建固定24小时，重发/重试沿原链接且不续期；已有有效同邮箱由服务复用，已注册邮箱拒绝。重发受服务60秒限流，不能从latest_delivery时间推算剩余限流；重发不推进Invitation版本，不能要求receipt.version=原version+1，只核目标ID及合法版本。撤销前明确邮箱与链接失效影响，取消零写。邀请版本与邮件任务版本严格分开，目标及版本在确认时捕获，刷新不偷换。

列表的 `latest_delivery={job_id,accepted_at,phase,attempts,version,channel,attempt_result,reason?}` 是该邀请跨重发/重试周期最近一次已接受任务，不能按邮箱扫全局任务推导。详情解释当前尝试渠道与结果；channel可为null，不用当前SMTP配置补历史。`retry_wait`可保留上一attempt的unknown；`sent`只表示SMTP接受或受限日志持久写入，不保证收件箱到达。后台日志渠道只说明由获授权运维协助，页面不展示/复制token、恢复链接或日志正文。

**账号安全。** 四字段分Session、重置与挑战组，共一个保存边界：idle 900–2592000秒、absolute 3600–7776000秒且idle≤absolute、reset 300–7200秒、challenge 1–20次。默认值只作规则说明，表单必须由真实GET建立；不提供关闭挑战或自动写回默认值。期限改变仅影响随后签发的Session/token，已签发对象保留期限；挑战阈值影响下次尝试；邀请仍固定24小时。保存不宣称撤销旧Session。PUT结果须id匹配、version为原version+1、四值等于原意图；历史结果不覆盖较新当前观察。

**SMTP配置。** 首次GET成功才允许编辑；404/失败不等于未配置。传输字段为host、port、encryption(`tls/starttls/none`)、username、sender_email、sender_name，另有auto_retry_count 0–5、retry_interval_seconds 10–3600秒。按用户最新指令，原“身份认证”组改名“发件账号”，将账号、单密码框与发件邮箱/名称（From Address/Name）放在同组；wire仍用sender_email/sender_name，不新增同义API字段。重试计数是首次之外的自动尝试次数；当前配置不承诺连接/投递可用。未配置时保留重试编辑及明确“配置SMTP”入口，不猜主机/端口/认证；保存未配置重试策略走unconfigure。

密码框不回填：已有凭据时留空保持、填写新值替换，日常表单不要求选择keep/replace/remove模式，也不把清空输入解释为移除。日常保存的wire使用keep：留空省略password，非空带新password；后端没有replace枚举。首次无凭据时按正式username/credential配对校验，同为空才是无认证，非空账号须有新密码；已有凭据也不能仅靠清空账号/密码绕过配对。新密码1–2048 UTF-8 bytes，拒NUL/CR/LF及非法Unicode，不trim；只显示credential_present安全状态，不用掩码假装读取旧密码。另保留不抢主流程的次级“移除已保存的认证”按钮及确认，仅无未保存修改时可用；有草稿时禁用并提示先保存或取消，不暗中覆盖草稿。该明确动作才提交username为空及credential_action=remove。host、邮件地址、显示名及字节规则沿正式SMTP规格。真实“停用SMTP”仍是独立危险动作，确认清传输/凭据并提交捕获的两个重试值，不暗示取消在途投递。`SMTPResult.applied_version=原version+1`，`settings.id`匹配singleton且`settings.version≥applied_version`；当前settings可与原写不同，不能要求它等于原草稿。

**SMTP测试与任务。** 切区先处理脏草稿/未确认意图，默认配置区不发送或自动读任务。新测试需已确认当前configured=true，填写收件人后显式请求；历史原请求恢复不被较新的停用状态遮断。任务列表包括invitation/password_reset/test，详情用management DTO的 `attempt_channel/attempt_result`，旧兼容 `channel`不是实际尝试证明；InvitationDelivery.channel才是其当前attempt事实，两个相似DTO不可合并。邀请accepted_at是接受时间，MailJob.created_at是意图创建时间。只显式刷新，不自动轮询。请求受理与任务结果分开；严格202后只读取已返回JobID，不重发已确认POST。

邀请页和SMTP页的人工重试共用规则：源阶段sent/failed/cancelled/unknown才可能允许；已记录尝试须有终局attempt_result，unknown且无终局记录不可重试，合法cancelled/null不能只因无渠道就拒绝。版本须可推进，仍由服务原子核材料有效性、谱系及权限；UI提供禁用原因而非保证许可。确认说明“使用当前合法配置和原业务材料，可能重复投递”，新JobID与源ID分开保留；不延长邀请或重置链接有效期。

## 4. 权限、所有权与恢复

1. **同一实际owner。** 四页全部HTTP经过现有新Account协调器，和登录、头像、个人设置共用实际fetch/body/cancel尾部；系统域不建第二队列。Caller的页面/管理权限current与Session current取交集，不能被command覆盖。可见超时、取消、离页不提前放行新身份请求。
2. **系统权限独立。** 当前Session确认admin后才读取。增加管理权限代际；当前系统403撤下全部管理显示、草稿/秘密与恢复资格，但保留有效本人Session，不伪注销。重新检查Session并恢复当前管理员事实后才重开管理读取。401/CSRF失效沿既有会话规则；旧身份、旧页面或旧管理代际响应/finally不得发布。普通同身份重验保留未提交草稿，真实失权/换Session清除私有材料。
3. **草稿、确认、当前观察分开。** 版本冲突保留原草稿/版本，显式重读仅展示当前值，用户明确采用后才能新建意图；不自动合并/换key。dirty离页、关闭编辑/确认层、注销及SMTP切区共用继续/放弃选择；焦点回到仍存在的触发器或合适任务控件。严格写成功后立即保留确认，后续读取失败只允许重读，不把已确认写改为Unknown或再次提交。
4. **无公开Account lookup。** 已派发mutation的transport/取消/坏响应、COMMIT_UNKNOWN或unknown/committed Problem保留不可变原method/path/key/body/version/身份/CSRF。原意图仅在同一合法上下文显式重放，或明确放弃追踪；不自动发新key，不拿Session/列表/配置GET当回执。一次未确认不会被后来拒绝、相同字段或版本推进抹除。上下文失效清恢复资格仍不证明服务回滚。
5. **SMTP后读例外。** PUT SMTP及unconfigure写后再读当前配置；test及retry写后GetMailJob。因此其5xx/RESOURCE_BUSY，即使Problem写not_started/not_committed也可能已提交；test/retry的404/NOT_FOUND同样如此。无既往Unknown且严格not_started/not_committed、status<500、code属于INVALID_ARGUMENT/VERSION_CONFLICT/INVALID_STATE/RATE_LIMITED/RESOURCE_DELETED时才作为明确业务拒绝；其余已派发不确定结果保留原意图，IDEMPOTENCY_KEY_REUSED不得换key。401/403/CSRF清恢复资格，仍不声称原写未发生。该规则按端点应用，不改变普通GET含义。
   新后端候选将已确认提交后的投影读取失败标为 `commit_state=committed,retry_hint=replay`；前端保留原值，显示“操作已提交，回执尚未读回”，原key/body重放取严格回执，不把已提交再说成未知是否提交。若后读本身产生COMMIT_UNKNOWN，对外改为DEPENDENCY_UNAVAILABLE+committed+replay，避免错误lookup提示；不新增公开lookup或改成功DTO。旧实例与新候选的实际验证范围以current为准。
6. **材料只在必要私有内存。** SMTP新密码、完整原body、CSRF/key仅由当前私有命令owner持有，不进URL/storage/普通日志/截图/公开响应式状态。Unknown保留精确原密码供原请求恢复；确认、放弃、失权、离页按实际owner尾部清引用，不清材料后伪称仍能恢复。UI清理不声称物理擦除全部浏览器副本。邮件token不由管理页接触。

## 5. 首链、验收与分工

先打通管理员进入用户目录→创建隔离邀请→查看真实受控投递→已有邀请页兑换普通账号→回目录刷新看到注册结果；安全配置与SMTP完整内容并行。基础正常/失败后尽早真实联调，不等四页全矩阵或文档扩写。

必要验收覆盖：四页真实加载/空/失败、普通用户直链零管理请求、当前失权及迟到结果隔离；邀请创建/重发/撤销与一次真实投递重试；安全保存/刷新和冲突不偷换版本；SMTP配置、密码留空保持/填写替换及首次配对校验、次级认证移除的确认与草稿保护、独立停用、测试受理与实际任务状态。定向覆盖SMTP写后读失败的Unknown、原key/body恢复、确认后读失败零重复写和实际owner未结束时互斥。浅深、桌面/390、键盘/焦点和适用错误整页由未参与实现者观察；沿已接受布局和主题，不借旧UI通过代证新页面。

系统安全/SMTP真实写使用独立测试实例和受控邮箱，不更改供用户试用实例的全局配置或admin资料/凭据；未获另行授权不向真实外部邮箱发送测试邮件。受控SMTP接受不等于外部邮箱送达；公网、证书和原服务期限仍按当前状态据实保留，不由本轮页面成功推断公网验真。

| Owner | 写入/交付边界 |
| --- | --- |
| `vue_integration` | 用户/邀请页面及唯一共享System client、Session/transport接线、路由与组合检查。 |
| `system_api_surface` | `web/src/views/system/AccountSecurityPage.vue`、`SmtpSettingsPage.vue`、`web/src/system/account-settings.ts`；与共享owner对齐窄接口。 |
| `account_backend` | 成熟服务/实际wire核对及隔离真实提供方；无页面收益时不重做API。 |
| `vue_tests` | 必要新前端/真实链harness与恢复说明；旧未变结果按输入复用。 |
| `vue_visual_review` | 未参与实现的有限视觉、交互与适用安全独审。 |
| `account_contract` / root | 前者本合同、current及必要说明；后者跨任务协调、全部Git和最终集成。 |

正式领域依据：[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP投递](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[用户目录](d27-system-user-directory-read.md)、[邀请投影](d27-system-invitation-delivery-read.md)、[账号安全](d27-system-account-security-ui.md)、[SMTP配置](d27-system-smtp-settings-ui.md)、[SMTP测试与任务](d27-system-smtp-delivery-ui.md)。旧卡仅复用仍成立的产品/服务规则，前端结构与本轮完成出口以本合同为准。
