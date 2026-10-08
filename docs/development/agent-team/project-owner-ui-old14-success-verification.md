# Owner 工作区 OLD14：十四轮作者实际成功与退役验收

限定接受 **OLD14_BATCH01 的14个作者真实单 top PASS、固定原件及 owned 退役的独立复核**。先前两个 auth 真轮只按未变依赖有限复用，组成“旧2＋新14”的覆盖；没有在 final05 重新执行全部16，也没有以原件复核代替独立实际 A/B。README #22、品牌接入、完整 Owner 工作区／D27 与生产绑定不在本档接受范围内。

[作者批末件](project-owner-ui-old14-success-verification-evidence/originals/author/old14-batch01-result.json)的 SHA 为 `f8f36ced07c67ffd731ee75d505816b5e246d8db7f600655002ed193602b70f1`；[原摘要](project-owner-ui-old14-success-verification-evidence/originals/author/old14-batch01-result.md) SHA 为 `b865904baf3174d573a4b2b951d5c06c30e6f184d0caaaa3f6d1287289248a08`。作者 STOP 后，独立负责人分三段复核固定原件，root 通读正式末件后授权本归档：

| 固定独核范围 | 正式报告 | evidence／STOP manifest |
| --- | --- | --- |
| 前三 personal 轮 | [e1808d3c](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-personal3-result-review/review.md) | [evidence](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-personal3-result-review/evidence.json)／[manifest](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-personal3-result-review/manifest.json) |
| 第4–8轮 | [8de9ebad](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-domain5-result-review/review.md) | [evidence](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-domain5-result-review/evidence.json)／[manifest](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-domain5-result-review/manifest.json) |
| 后6轮及整批组合 | [f23c6c6d](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-batch01-result-review/review.md) | [evidence](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-batch01-result-review/evidence.json)／[manifest](project-owner-ui-old14-success-verification-evidence/originals/independent/old14-batch01-result-review/manifest.json) |

整批正式报告 SHA 为 `f23c6c6d52230d56e7f36e41a7e04a6ab0b72d76591ea31c38096db16129619c`，末件 manifest SHA 为 `7ed69703f932955f9390fb91600632f72318ce591dce0121da19ef9ec86cc616`。前三与中五报告各自保留当时尚未接受后续轮次的原文；最终组合由整批报告明确，没有重写历史报告。

本档行政基线由 root 移交为 `7410e866`。实际受测源码仍按固定输入绑定：UI19／Go04／Project browser-v5 和工具、锁依赖沿已有验收档继承；本轮唯一 legacy 源差量是 Git `80ec2ab378c7420d65240bf9c1b8a48e871e5acd` 的 `personal-settings.spec.ts`，SHA 从 `155bba1a…` 变为 `a0fede03ce80118de78a9ba033540bca25587288927791f2daa761c48ea30603`。[窄修永久档](project-owner-ui-legacy-profile-json-verification.md)固定两 hunk、原断言和 helper 的观察边界；本归档继承其 Git/blob/SHA 引用，没有执行 Git 或再读当前产品源树。

[final05 原 handoff](project-owner-ui-old14-success-verification-evidence/originals/input/final05/handoff.json)、[569字节 closure delta](project-owner-ui-old14-success-verification-evidence/originals/input/final05/closure.delta.json)及[独立 PREPARED 报告](project-owner-ui-old14-success-verification-evidence/originals/independent/final05-review/review.md)固定单源变化：955 repository files、66 file sets、171 Python modules、20 schema resources 和1165显式输入均沿原闭包继承，没有新 import 或 package-file-set 差量。本次只保存这个小 delta 和必要元数据，没有复制或重建整张图。

一次原作者 filegate 的[原 raw](project-owner-ui-old14-success-verification-evidence/originals/input/final05/source-check.raw)与[meta](project-owner-ui-old14-success-verification-evidence/originals/input/final05/source-check.meta.json)记录 exit0、1.669532177秒、1165文件匹配、缺失／差量／pending为空，PID289921／start1580359实际 wait 后两次身份 absent。该检查是纯文件准备，不等于资源启动或第二次完整图重验；本归档没有重跑门禁。

[原 false freeze](project-owner-ui-old14-success-verification-evidence/originals/input/final05/freeze.final.json) SHA 为 `62da72e10ae28fde09bcf016756a476585fe9a99192d0bf8dcaf25b6b720bebc`，保持 false／pending=[]。root 单独发出的 truecopy SHA 为 `2c261920d79f8d454a375e609fbab243fc912bbd4136a916228d2afff8b8172f`，与14轮 [frozen-input](project-owner-ui-old14-success-verification-evidence/originals/runs/oldprofile02/frozen-input.json)逐字节相同，合并保存一份。false→true 实际只改变 `root_authorization`、`root_authorized_resources`、`status` 三个顶层键；`permitted_groups` 原已是同一14组。原计划中的 pending/false 只表示当时准备状态，不回写成事后成功，也不给后继窗口授权。

每轮均保留24件 run 原件、handoff 和 launch。下表的原 top、browser、direct 耗时分别来自固定日志与命令；完整 selector、argv、env安全项、PID/starttime、exit、实际 wait、资源ID及尾采样由[来源映射](project-owner-ui-old14-success-verification-evidence/source-map.json)定位。

| 轮次／原 handoff | 原 Go top 秒 | browser 秒 | direct 秒 | 有限结果 |
| --- | --- | --- | --- | --- |
| [oldprofile02](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldprofile02.json) | 11.19 | 5.6 | 62.888 | profile／avatar 原 case 全部到终局 |
| [oldtheme01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldtheme01.json) | 16.24 | 10.7 | 67.249 | preview/cancel、版本竞争、导航和 dirty 保护 |
| [oldpassword01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldpassword01.json) | 14.95 | 8.5 | 68.501 | 改密一次提交、原 Session 撤销及替换 Session 后继写 |
| [oldinvite01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldinvite01.json) | 12.43 | 7.4 | 63.584 | accepted loss、原 replay、abandon、读取失败与版本竞争 |
| [oldproviders01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldproviders01.json) | 11.60 | 6.9 | 62.933 | 凭据创建／替换、共享 ref、部分成功及 reviewed rebase |
| [oldmodels01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldmodels01.json) | 16.15 | 11.4 | 67.602 | create/update/delete 丢失响应后保持原 command 和事实 |
| [oldselection01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldselection01.json) | 12.66 | 8.3 | 64.723 | accepted loss、历史 lookup、正式删除后原 replay |
| [oldsummaryrec01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldsummaryrec01.json) | 13.08 | 8.7 | 65.363 | 两次 accepted loss 保持独立原 body／receipt |
| [oldsummarynav01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldsummarynav01.json) | 15.85 | 10.6 | 68.738 | 聚合导航、Session 延续和当前权限 |
| [oldaccountsec01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldaccountsec01.json) | 10.28 | 6.8 | 60.058 | accepted 响应截断、当前 GET 与历史原 replay |
| [oldsmtpsettings01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldsmtpsettings01.json) | 14.04 | 7.8 | 66.005 | 原 PUT/POST 丢失后以原材料 replay |
| [oldsmtpdelivery01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldsmtpdelivery01.json) | 28.78 | 16.3 | 79.952 | owned 真实 SMTP 协议结果与显式 reviewed retry |
| [oldoutbound01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldoutbound01.json) | 11.08 | 6.3 | 61.281 | 两管理员、严格原 replay 及历史 Audit |
| [oldpublic01](project-owner-ui-old14-success-verification-evidence/originals/handoffs/oldpublic01.json) | 15.03 | 9.3 | 68.277 | 禁止邀请、账号重置、显式退出与 dirty/back 身份保护 |

新的 profile 原 case 已越过旧 CDP 正文错误：400及原 field_errors、aria/current GET、后续编辑和头像断言执行到终局；原 Go 日志确认三种真实静态头像格式上传／回读、小型重编码正文低于1MiB、三种真实拒绝保旧头像及精确移除，normal-read closure终局通过。最终 version／commands／audits 为9／8／8。旧 `oldprofile01` 当时缺失的正文、UI与最终 facts 仍然缺失，不能把新轮结果补入旧轮。

theme 原日志独立观察到 preview/cancel 时 version1／commands0／audit0，其余固定竞争与导航后终局4／3／3。password 终局3／2／2包含原 case 其余个人写入，不解释为改密两次；两个原 Session 撤销、新密码登录与旧密码拒绝沿原断言接受。

所有结论限各原域完整 case PASS 和 Go终局事实。邀请中的 clock／authority 与 summary 的角色 SQL 属 owned fixture 事实准备；不证明自然流逝、生产角色管理接口或生产 hosting。Provider／Model／Selection／Summary 配置通过不证明外部 Provider 调用、内容生成或索引重建。SMTP settings 只证明配置；SMTP delivery 的协议接受不证明外部邮箱送达。outbound 只验证策略事实／replay／Audit，没有新目标 probe、SMTP socket 或 Runtime 结论；account security 不扩展为追溯 Session 撤销，public entry 的私有投递／链接准备保持原 fixture 边界。

这些336件 run 原件没有独立持久 HTTP body、DOM、trace 或截图，safe evidence／images均为0。正文与UI结论依固定断言实际通过和原 Go日志，不另造 transcript，也不套用新 Project 的 schema／public-client计数。native clone观察仍会 tee 正文，normal-read终局不能外推为未观察 stream／Cookie-owner尾证明。

整批原件核得 **100个实际不同资源ID**：SMTP delivery为5容器＋4网络，其余各4容器＋3网络，跨轮无ID复用。逐ID两扫 absent；**14 direct＋56 adopted 实际 wait、14 watchdog join**齐全，owned／runtime／browser runtime双空、forced动作和monitor错误均为0。57个新增非owned、未wait的PID1 containerd-shim按PID/starttime另列，其中SMTP delivery为5，其余各4；它们不算owned子进程join，也不代表全机清零。

每轮 TCP/TCP6 末两次采样的 active／TIME_WAIT／new-host 增量为空，没有因TCP采样发送信号。这里只接受原补充采样的清理边界，不称完整短连接轨迹。所有下一轮启动都晚于上一轮TCP终局；top120秒含cleanup、包6分钟、TCP75秒及fresh5GiB保持，原legacy case预算和重试不变。[原命令](project-owner-ui-old14-success-verification-evidence/originals/runs/oldprofile02/command.json)记录 `GOPROXY=off`／`GOSUMDB=off`、固定Go1.27.1和只读模块配置，完整14份argv/env以各自原件为准。

最后 public TCP 尾清为 **2026-10-08 08:34:34.975849 UTC**。root 的[恢复原记录](project-owner-ui-old14-success-verification-evidence/originals/root/restore-after-old14batch01.json) SHA 为 `e54f10daff1abdd7c082d63357bceeea37b9083fdc86e004ac3ce67a77254960`，记录时间08:35:09.822798 UTC晚于末轮退役；original_restored_exactly=true、reader_preflight=[]。原3资产的路径／bytes／SHA逐项保存，53测试资产保留在 `test-dist-ui-v1`。运行时的53与随后恢复的3分阶段记录；[原交换记录](project-owner-ui-old14-success-verification-evidence/originals/root/exchange-old14batch01.json)和恢复记录都没有在归档时重测当前dist／主机。原恢复记录中的“final manifest pending”保留为当时事实，随后正式批末件和独核另立记录。

[旧16首批永久档](project-owner-ui-recovered-old16batch01-verification.md)继续保存 **2 PASS／1 FAIL／13 NOT_RUN**、原 `oldprofile01` 的 `Network.getResponseBody` FAIL 和缺失证据。auth lifecycle／revocation-expiry原两PASS的限定组合由已接受单源变化、final05准备独核与相同固定闭包支持；它们不是本轮重新调用。原格式FAIL、formatter越界后恢复、TS2488及修正lib后类型PASS仍保存在[窄修档](project-owner-ui-legacy-profile-json-verification.md)，没有被14个实际PASS覆盖。

归档共 **391逻辑原件／6,080,712字节、258个不同SHA**：336 run／5,488,348字节，14 launch／178,502字节，14 handoff／107,916字节，其余为批末件、root两记录、必要准备和独核小原件。按SHA去重后新增 **253实体／4,807,752字节**；5个既有永久共同实体／43,935字节原位复用，承载84个逻辑引用。共133个相同SHA的额外逻辑引用仍在来源映射逐项保留。历史auth2／失败／旧批四引用另列，复用原永久档，不混入391件或新增实体计数。没有复制源码树、runtime、cache、依赖或资产实体。

[归档自查](project-owner-ui-old14-success-verification-evidence/archive-checks.json)记录391项source/archive的SHA与bytes、既有实体／历史引用、JSON解析和链接／格式核验；[格式登记](project-owner-ui-old14-success-verification-evidence/format-exceptions.json)保存14份原raw中43处尾空白行，原bytes未改。原件均为UTF-8、LF且有末尾换行，无CRLF或多余EOF空行；生成文档和JSON无尾空白。归档脚本两次失误（manifest预期SHA抄录错误、误以为permitted_groups变化）在复制前退出，已按正式原件纠正；首次链接自查还因先检查尚未生成的自身报告而退出，修正顺序后通过。三个失败的原raw均保留，属于归档工具检查失败，不是产品检查或原件变化。未运行Git格式命令，由root交付时处理上述原始例外。

本次只关闭OLD14作者真轮与原件／owned退役接受。独立实际A/B需要单独真实执行和独立记录，后继窗口不由此档推定；README #22、品牌接入、完整D27、production Resolution／Invocations／D24、ready503及既有三停止保持各自边界。归档期间没有运行Go、Node、浏览器、资源、网络、停止项probe或读取活动验收窗口；两授权路径完成自查后STOP交root。
