# D07：邮箱 canonical 再输入与 SMTP wire 闭包修复

修订：rev1。状态：十路径已获作者检查、独立最终组合验收及主线程采纳，提交推送 `f670cb1fe1f07ebd21bdb90a2b96cd565c33f05a`，远端一致；[正式报告与不可变证据](../agent-team/email-canonical-roundtrip-verification.md)保留原失败、实际退出/双清、分轮复用与限制。固定接受基线 `819aba1b8f764328f1e2e67b53c274fad0db877d`；技术§1–7 SHA `f62bf7a195d9bf501d33cebf48e43680e5f58db112d00b7dc417583f757aaf15`原字节保持。原被审候选全文SHA `c48e3a5e71b9f454cdf32786d9e864480bf9fa91ea7128d565022ac711e71c63`没有精确全文副本，原技术与接受页首全文可由固定规格Git恢复，不补造旧页首。下文保留规格阶段原事实；本卡只接受邮箱闭包修复，不接受SMTP UI、出站HTTP或完整D07/D27，不改其原失败记录。

## 1. 已有证据与完整结果

目标是让既有合法邮箱的小写 canonical 可重新输入，并可安全用于实际 SMTP 信封和邮件头；旧合法输入的 canonical 字节、数据库身份/索引、命令 MAC 与历史回执保持。正式 SMTP GET→仅编辑其他字段→PUT、TestSMTP 的两次规范化及 worker 发送必须闭合，不能用前端隐藏转换代替后端修复。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[D07 设计](d07-account-session-smtp-design.md)、[Account 生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP 正式设计](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)与[后端说明](../backend/account.md)。D07 已接受的裸 ASCII addr-spec、最长254字节、全小写 canonical、不去点/不去+tag规则保持；不重新选择邮箱产品规范或 RFC 子集。

| 固定来源 | 已证明的事实与限制 |
| --- | --- |
| 独立 API01 | 私有根 `/workspace/scratch/agenteam-smtp-settings-api-independent-2u508r5g`，`review.md` SHA `8380e6ce82ffc2d7dc7018de2d10bcc38017516038b6dfc7d1da3d04f40b5f66`；原 API raw SHA `482f39c89cccc6561da32d5555921dd91b1fec64136fc29ee6d8f3d4742a9738`。实际1/0.598s、11通过/1失败；候选先 lower 再验前缀，原后端拒绝的 raw 进入受控 fetch。不是正式服务成功或浏览器结果。 |
| Go1.27.1 oracle | 同根 `oracle-results.json` SHA `cb2bf82f99339c2b68dcb2a3d9ba43a8020d15901d7ec72eabdb22cc68509675`，实际0/6.147s。精确原 NormalizeEmail 接受 `a@[IPv6:2001:DB8::7]` 并输出 `a@[ipv6:2001:db8::7]`，后者重新输入却拒绝；原函数非幂等。仅函数 oracle，无正式 HTTP/DB/SMTP。 |
| 后端静态诊断 | `/workspace/scratch/agenteam-smtp-email-closure-diagnosis-fo8u14al/diagnosis.md` SHA `e306e7a6a758cdf58ae6f1f36a4fdea9b44cf31136417dd08154e9af0cb76bdc`；`git-basis.json` SHA `fa6eb80b62a0aa3f22c6ec74420eeb735260fcfdc83e7c26ba03309195a4eb0f`。14相关路径在40c904c与819aba1逐字相同。GET/编辑/PUT、二次入队和 worker 影响为源码证明，尚未执行这些正式组合。 |

固定调用链：`account/validation.go:10`；`smtp_settings.go:101` 每次 PUT 先 Normalize，保存/GET原样返回小写；`smtp_commands.go:18` 首次 Normalize 后，`delivery_intent.go:181` 再 Normalize 且要求等于参数；`accountmail/smtp.go:380` 对持久材料再次直接 mail.ParseAddress。Go1.27.1 `net/mail` 只识别精确 `IPv6:` 标记，整体 lower 导致闭包断开。TestSMTP 原失败可发生在 planned 命令已持久之后，不称零写；worker 的 message 校验发生在可能已连接/EHLO/TLS/Auth之后、MAIL FROM之前，不称零网络 I/O。

## 2. 唯一规范化契约

令 `S` 为固定819aba1原 NormalizeEmail 实际接受的完整字符串集合，`lower` 为既有 ASCII 全小写输出。本修复接受且只接受 `S ∪ lower(S)`；对全部合法输入仍返回 `lower(input)`，保证 `N(N(x)) = N(x)`。原 `S` 的输出逐字不变，新增的只是既有 canonical 再输入资格，不把输入先无条件 lower 后判合法。

先保留现1–254字节、ASCII33–126、无首尾空白的检查，以及原 `strings.ContainsAny` 排除的圆括号、尖括号、逗号、分号、反斜杠和双引号。旧成功分支继续要求 Go mail.ParseAddress 成功、Name为空、Address精确等于整个输入。仅在原分支失败、输入已经**整体小写**且 domain-literal 标记为精确 `@[ipv6:` 时，允许在私有解析副本把该标记改为 `@[IPv6:`，再次执行原完整裸地址资格检查；输出仍取原输入全小写。标记必须位于唯一实际 domain-literal，不全局 Replace，不忽略尾部/前后缀，不吞解析错误。

| 输入代表 | 修复后资格与输出 |
| --- | --- |
| `A@[IPv6:2001:DB8::7]` | 原S合法，输出仍为 `a@[ipv6:2001:db8::7]`。 |
| `a@[ipv6:2001:db8::7]` | 新增 canonical 再输入合法，输出不变。 |
| `a@[iPv6:2001:db8::7]`、`a@[IPV6:2001:db8::7]` | 仍不合法，不能泛化任意大小写标记。 |
| `A@[ipv6:2001:db8::7]`、`a@[ipv6:2001:DB8::7]` | 仍不合法：非旧S，也不是完整小写 canonical；不得先 lower 偷渡。 |
| `a@[IPv6:192.0.2.7]` 与其全小写输出 | 保留 Go 原实际接受的 prefixed IPv4 形状，并允许其 canonical 再输入，不能额外要求真正 IPv6 地址。 |
| `a@[::ffff:192.0.2.7]`、`a@[0:0:0:0:0:ffff:c000:207]` | 保留原无前缀 mapped 形状和文本，不新增前缀、不压缩。 |

普通域名、IPv4 literal、旧 dot-atom 域中的 `!`/下划线及其它原合法字符保持原资格；不用 DNS host/outbound parser 替代邮箱 parser。无前缀普通 IPv6、zone、非法 IPv4、非法 literal、display name、comments、多地址、quoted/escape、CR/LF/NUL、其他控制/空白、非 ASCII/非法 UTF-8等原拒绝项保持。不能用 `netip.Addr.String()` 压缩或规范化 IP 文本、IDNA、trim、去点/去tag等产生第二种 canonical；同一IP的不同旧文本不在本卡合并为同一账号。

NormalizeEmail 签名与原 `/email` / `EMAIL_INVALID` 安全错误保持。smtpFields 继续把相应错误映射到 sender_email；不删除 TestSMTP/insertDelivery 的第二次校验或相等断言，不改任何调用者来跳过本函数。实现可抽本文件私有旧资格帮助函数，不能增加可变全局、缓存或另一套宽松 parser。

## 3. SMTP canonical 到 wire 的窄适配

`accountmail/smtp.go` 内私有帮助函数先调用同一 `account.NormalizeEmail`，并要求结果与材料原值**逐字相等**，只接受已 canonical 的 sender/recipient。Accountmail 的 runtime.go 已依赖 Account，增加此调用不形成导入环；不新增公开邮箱 API 或反向依赖。

对已校验 canonical，仅将实际 domain-literal 的 `ipv6:` 标记转为 `IPv6:`，其他所有字节保留；随后用同一 Go mail.ParseAddress 核 wire 的 Name为空、Address等于完整 wire。无该标记的旧合法地址原样保留。不能压缩IPv6、改变local-part大小写、插入额外引号/转义、改地址选择或将邮箱literal当SMTP连接目标。错误沿原 accountmail 安全 invalid/outcome 映射，不暴露 parser 原错误或地址。

每次发送只生成一对已校验的 senderWire/recipientWire，**MAIL FROM、RCPT TO、From、To四处共用这一对**；From显示名仍由 mail.Address安全编码，SenderName原CR/LF/NUL检查、Subject/Message-ID、正文、dot-stuffing与64KiB消息上限保持。允许整理同文件的私有 message/准备函数签名，但不改变 DeliveryMaterialFields、数据库值、持久材料、Audit、JobID或receipt。临时 wire仅在当前材料 Use/发送生命周期内，原message/password清理及lease实际join继续执行。

校验仍在既有 message准备边界完成，并在 SMTPMail准入/MAIL FROM之前拒绝非法材料；不声称拒绝前必然没有连接或AUTH。原 TCP/TLS、出站解析/固定禁止地址、BeginSend/首写permit、checkpoint、终局与unknown重试风险、超时及实际关闭顺序原样保留，不增加自动重发、fallback渠道或协议成功假响应。

## 4. 存量与其他 Account 入口影响

不迁移、不重写 users.email、invitations/password_resets/reset_requests 的 canonical_email、SMTP from_address、delivery_intents.recipient；原小写CHECK、唯一索引、privacy counter及已持久材料原字节继续有效。原合法输入的 NormalizedEmail 不变，因此 SMTP Settings semantic中的From、smtp-test recipient、invite/reset/login语义及MAC派生输入不变；namespace/key/expected_version、历史Session规则与旧回执均不修改。保存后GET显示小写canonical，wire大小写绝不回写到任何身份/命令事实。

| 原入口 | 本修复带来的闭包与必须保留的规则 |
| --- | --- |
| SMTP Update/Get | 大写标记旧合法raw可保存，GET的小写canonical可直接进入新PUT；只编辑host等字段不再因旧sender被拒。原 applied_version历史与current settings区分、配置/凭据两阶段、空密码/移除及Unknown保留不变。 |
| TestSMTP / insertDelivery | 首步canonical可通过第二次Normalize+等值检查，唯一test intent/event/Audit正常接受；原同key/body重放继续原command/JobID，不能因曾planned便改新key。原后读失败不成为未接受证明。 |
| CreateInvitation/兑换/重发 | 新创建规范化、存量link.email与用户小写身份可继续复用；旧raw和canonical命中同一唯一性约束/语义，不能创建两个邮箱身份。重发不重新发明邮箱、续期或绕限额。 |
| Login/LookupLogin、Challenge创建/验证/消费 | canonical可作为raw再次使用；同规范值的账号查找、subject/counter与挑战email绑定一致，其他邮箱、browser、key仍隔离；不放宽验证码或密码验证。 |
| RequestPasswordReset/恢复/投递 | canonical请求与原合法raw语义一致，公开存在性隐藏、限额/间隔、链接生命周期与已有投递渠道保持；不为该邮箱暴露新响应字段。 |

代表性纯向量与正式组合覆盖上述共同函数消费，不因共用函数便声称全部Account回归已重跑。已持久的原回执不因校验修复过期；规范输入等价不能成为前端改写**已冻结 pending body**的理由。旧source确实可能留下planned的事实与成功回执分开，不能把原TestSMTP错误记成零command。

## 5. 唯一候选路径与测试支持

以下十路径待独立静审和主线程另授唯一 backend_worker；当前架构只写本卡。只有前两项修改生产逻辑，未列入候选的固定调用链源码保持只读。

| # | 路径 | 限定用途 |
| --- | --- | --- |
| 1 | `internal/central/account/validation.go` | NormalizeEmail旧集合及canonical闭包；username/display规则不改。 |
| 2 | `internal/central/accountmail/smtp.go` | 统一canonical验证、私有wire转换与信封/头部共用；其余网络/发送语义不改。 |
| 3 | `internal/central/account/validation_test.go` | 保留原断言，增加旧S向量/幂等/新增资格及严格拒绝。 |
| 4 | `internal/central/accountmail/smtp_address_test.go`（新） | 私有地址准备、头部/信封预期与非法材料纯测，不假称已发送。 |
| 5 | `tests/account/email_canonical_roundtrip_test.go`（新） | 正式服务的TestSMTP二次校验/重放与邀请、登录/lookup、挑战、reset代表；复用旧fixture与公开挑战解法。 |
| 6 | `tests/account/http_email_canonical_roundtrip_test.go`（新） | 真实root SMTP PUT→GET→编辑→PUT、历史receipt/当前观察及严格拒绝；复用newHTTPFixture。 |
| 7 | `tests/accountmail/email_canonical_roundtrip_test.go`（新） | 正式配置/intent/worker、同一预期wire的真实邮件及终局/材料join。 |
| 8 | `tests/testsupport/smtp/fixture.go` | State只增加MailFromSHA/RCPTToSHA两个测试观察字段；既有字段/语义保持。 |
| 9 | `tests/testsupport/smtp/cmd/server/main.go` | 现MAIL/RCPT分支在同mutex下记录最近实际完整命令行SHA256，原应答/计数/时序不改。 |
| 10 | `docs/development/backend/account.md` | 最后局部说明旧S+canonical资格、存储/wire区别和后继前端边界，不重写Account架构。 |

fixture两新字段仅为任务自有内部观察：收到合法行边界后的原始命令行（包括末尾CRLF）SHA256、小写64hex，未收到时空；不保存/回显raw地址、AUTH材料或正文，也不改变服务器解析或成功/失败决策。单任务新场景一连接/一邮件分别比较明确预期的MAIL/RCPT和现MessageSHA，不能仅检查它们互相一致。新增测试不打印State整体或这些hash，公开报告只给匹配布尔与计数；原私有失败证据按权限保存，不把hash放进公开诊断。

不改SMTP Settings/Test/insertDelivery/邀请/登录/挑战/reset写服务、Account contract、OpenAPI字段/operation、SQL/迁移、模块锁文件、原fixture生命周期/网络策略、原旧测试或任何前端文件。十路径中既有fixture与backend说明须主线程确认唯一作者；共享SMTP真实窗口另移交，不能同时消费活动SMTPUI候选。缺少必要能力、路径或需改变语义先报告修卡。实施必读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)及[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)。

## 6. 有界验收与首红保留

| 组 | 必须独立核实的结果 |
| --- | --- |
| 纯原红→绿 | 在固定原输入先实际运行N(N(x))与worker canonical地址准备的最小失败例，分别保留原日志/输入；源码推断不替代这些新动态记录。修后原例通过。覆盖254/255字节、旧普通/IPv4/prefixed IPv4/mapped/非压缩文本、原IPv6大写标记与其完整小写canonical、mixed和非全小写lower-prefix否例、全非法字符表。对旧S golden逐字核输出不变，normalize重复结果一致，不通过删除旧边界断言变绿。 |
| 正式HTTP编辑/历史 | 大写标记sender首次PUT成功，GET返回小写；保留sender仅改变host用新版本/key再次保存成功。原第一次key/body重放仍给原applied_version，current settings保留第二次结果，唯一原command/Audit不重复；输入canonical等价与原raw的semantic/MAC派生输入以固定旧向量对照。坏前缀/注入拒绝，未授权/错误CSRF仍拒绝。不得把候选中新建的历史receipt叫跨二进制升级实证；若做固定旧backend→候选的真实存量组合，另外记录两个输入。 |
| 正式test intent/重放 | 同时覆盖原大写标记raw和新canonical recipient，TestSMTP→insertDelivery实际成功，recipient落库为精确旧canonical；Reconcile后唯一job。same-key重放回同JobID，无第二intent/event/Audit，不重写pending原请求。可用正式服务受控第二Tx失败形成planned，再原key继续，禁止SQL伪造command/phase/receipt；此故障与旧基线原失败分列，不冒称同一原因。 |
| Account共同入口 | 正式邀请旧合法literal→兑换成为小写用户；canonical登录/LookupLogin成功且原raw查同一用户；同邮箱创建受原唯一性限制。至少一次真实Challenge创建/验证/消费在旧raw与canonical间使用同规范值，异邮箱/browser/key否例保持，阈值若需调整走正式Settings口，不直接写表。reset已注册/不存在合法邮箱公共响应形状一致、同key语义重放不重复受理。只读SQL核精确本次记录，不改业务email/时间来制造结论。 |
| 真实SMTP闭包 | 任务自有SMTP、现正式配置/账号/出站/worker；sender与recipient**同时**使用canonical literal。明确预期wire为两者仅改标记后的固定字符串，MAIL/RCPT行hash及包含对应From/To的完整已知test邮件MessageSHA逐一匹配该预期。至少普通IPv6与prefixed IPv4/mapped兼容代表；可同一已验none模式和独立场景顺序执行。实际Messages=1、mail/RCPT/data计数正确、job sent、attempt terminal/io_joined、lease释放/registry.Joined成立；不把fixture250等同真实收件箱送达。 |
| 旧边界组合 | 原 `TestCanonicalIdentityValidation`、代表邀请唯一性/重放与reset公开响应、原 `TestAccountMailSMTPThreeModesUseActualControlledSockets` 保持；fixture新字段不能改变旧mode、计数或MessageID。未变TLS拒绝、协议unknown、重试/停止/取消join及出站分类已有D07证据复用，不机械重跑全域或恢复停止任务。 |

三个新增真实顶层固定 `TestAccountEmailCanonicalRoundTrip`、`TestAccountHTTPEmailCanonicalRoundTrip`、`TestAccountMailEmailCanonicalWireRoundTrip`，可按上述同风险事实分子例。纯检查Go1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`；受影响Account/Accountmail纯测、race/vet，SMTP fixture兼容编译/vet、两integration包编译/vet与Central/Runner构建。纯/普通受控用例45s内，新增真实场景2m内，沿原 `-race -count=1 -timeout=6m` 分组与 `scripts/test-security.sh -run`，核实际命中且无以skip代通过。现PG17.x最低17.8、原迁移1–19、SMTP30s总体/现连接TLS与idle期限、消息64KiB、HTTP32KiB等预算不变。

真实测试只用主线程移交的独占窗口和任务自有nonce/精确资源；不修改生产classifier，不访问真实邮箱/外部凭据。不因新地址关闭TLS验证、扩大retry或改退出规则。所有worker、材料Use、连接、取消回调、父子进程实际结束，终局DB/Audit事实与资源exact-ID双次清零后再交窗口。独立者重点复核合法集没有误放宽、旧canonical/MAC兼容及真实信封/头部；日志不输出token、密码、材料或完整收件邮箱。

## 7. 前端接续与冻结交付

SMTPUI api-stage01及独立API01原11过/1红永久保留。后端本卡接受前，原正式raw资格仍按固定基线，不能先改前端探针期望或称候选通过；后端接受后由主线程另外移交前端窄返修，使input与DTO各按新正式集合验证：完整小写canonical可再输入，mixed及非全小写lower-prefix仍拒绝。原pending key/body/password/expected_version不改写，既有owner/Unknown/actualjoin与配置receipt纪律保持；新oracle/探针另存修订，明确原红来自旧契约，不能覆盖旧证据。

本卡冻结十路径及最小已提交依赖，交全文/技术SHA、原红与修后实际命令/退出/日志、复用证据和未验边界供独立审查；主线程采纳后另行提交、另授SMTPUI组合。后端闭包通过不等于SMTP页面、SMTP测试/任务UI、真实外部投递或完整D27通过。当前仅规格工作，无业务改动/新测试/真实资源；出站卡保持原冻结，Object/tools停止与Summary待决保持。
