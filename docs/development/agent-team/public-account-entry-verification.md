# 公开 Account 入口验证记录

**已接受本卡25路径结果。作者12个真实顶层按未变语义分轮闭合，独立最终PASS已获主线程采纳；源码提交推送 `787a5c7eeadf5e5f37bab97cbf030b4a745b72af`，主线程确认远端一致。** 记录日期为 2026-10-06。本页对应 [公开入口卡 rev1–rev5](../work-items/d26-public-account-entry.md)，包含邀请兑换、找回申请、密码重置及共享认证/个人设置回归，不表示完整 D26/D27 或平台已完成。

作者为 `frontend_recovery`，未参与实现的 `frontend_verification` 独立核定最终结果，核心与UI原探针按各自固定目录归档，资源由 `verification_recovery` 统一协调。本归档由 `management_reads` 仅复制固定原件、核对哈希和文档；没有执行业务、浏览器、Docker、SQL、网络或被归档的脚本。旧会话丢失的候选与日志摘要不计为本次通过证据。

## 固定输入与结果边界

[最终候选路径清单](public-account-entry-verification-evidence/candidate-files.json)绑定已接受 input13 的 25 路径，manifest SHA-256 `f824cf28b414a2b41276ae383342fd9bb861987784623929b0e35690ceff9fd4`；业务基线为已验个人设置 `c54f73f3324caa11608d84e5d207141985eb6074`。12 个前端生产源和 15 个 dist 资产自 input08 起逐字不变；后续 input09–13 仅修测试观察或握手。实测 dist 位于冻结 `input13/web/dist`，与冻结 input08 同字节；活动工作区 `web/dist` 是较早产物，未作为输入，不声称它匹配或已经受测。最终 985 项固定输入由 25 个授权源、945 个 c54 依赖和15个 dist 哈希组成。没有服务端生产、SQL、OpenAPI、依赖锁、共享 UI/theme 或 personal composable 改动。

完整 25 源文本按原 SHA 保存在归档，历史 core01–03/input01–13 用去重原件重建；其余依赖按 Git 基线恢复。[dist 清单](public-account-entry-verification-evidence/dist-hashes.json)只保存指纹，不复制产物大树。原件中的当时阶段与 pending 描述保持，现行结论以本页和 `status.json` 为准。原manifest的 `task_revision` 仍有rev2/24th旧文字，按独立最终报告保留原SHA；实际25路径与rev3–5授权及精确差量相符，未修改已运行manifest。

| 门槛 | 已有事实 |
| --- | --- |
| 前端 pure/build | input08 `npm run check --prefix web` 实际 exit0：151 测试/12 文件，格式、类型与生产构建通过；后续生产及 dist 不变，按明确范围复用。 |
| 受影响测试编译 | Go 1.27.1、offline/readonly integration race compile/vet 通过；input12 两个 browser spec 用 ES2022/DOM/DOM.Iterable 严格 tsc 通过，input13 仅旧 spec 窄差量和 tsc 经独立静审。 |
| 作者真实矩阵 | 原 input09 六组9 PASS/3 FAIL；input12补验Privacy/Theme PASS，input13最后Password PASS，合计12/12。各原exit1保留，不称同一输入的一轮完整矩阵全绿。 |
| 独立核心/页面风险 | C-F1、history capability key、204空流原反例分别保留红绿证据；匿名 owner/Unknown/actual-tail 的受控纯组合保持其限定范围。 |
| 独立真实跨账号 | input08：A申请/重置B后，B原Session401且持久撤销；A原User/Session及原CSRF继续完成真实偏好写，精确命令/Audit/Session后态通过。 |
| 已验后端组合 | input09前端与已验management13/ledger20组合实际PASS，编译内嵌target18与真实PG journal/checksum/三表前置通过；排除活动tools14，不声称无关后来main变化均已测。 |
| 最终接受 | [独立最终报告](public-account-entry-verification-evidence/reports/independent-final.md.txt)核固定25源/985闭包、原退出及双清后PASS；主线程已核SHA并提交上述源码。 |

实现沿同一个 Session Cookie owner 增加五个严格 typed POST、匿名 CSRF与有效Session并存、原 key/context/body 的显式重试。链接在建立 history 前捕获并移除，材料仅在私有闭包；显式身份选择不自动注销。重置严格204后确认命令并清材料，再在同一owner核对当前Session，核对失败不重新POST。详细规则归任务卡，本页不新增业务规则。

## 原反例与独立修复证据

[独立核心报告](public-account-entry-verification-evidence/reports/independent-core03.md.txt)保留 C-F1：确认重置后的Session GET deadline仍公开旧authenticated身份。core01原探针失败，core03用同一probe加6个受影响分支实际7 PASS，其他5项按选择器未运行；旧cwd误用而重复原红的准备错误也保留。修复隔离旧可见身份，同时保留真实owner尾部，不再次reset POST。Unknown/同owner/原key+CSRF+body/410/CSRF失效组合按不变路径复用，未冒充真实PG Unknown或Cookie竞争。

独立 history sanitizer 原探针在 input02 的根、嵌套及数组属性名/值中发现 capability，input03同probe通过；保留正常router state和一次私有捕获。旧静审报告被随后204反例与最终 input08 报告补充，不单独作为最终PASS。

[独立 input08 报告](public-account-entry-verification-evidence/reports/independent-input08.md.txt)记录204空ReadableStream原四例在input07为2 FAIL/2 PASS，input08同四例全部PASS。必须实际EOF且累计零字节才确认；非空/读取错误拒绝，abort及reader.cancel完成由同一promise join并释放lock。作者原真实204后UI uncertain没有采样到Response.body形态，因此控制反例不能冒充该浏览器原始body的直接证据。后续原deadline反例在input08再次通过。

## 作者真实分组与原始失败

[作者最终报告](public-account-entry-verification-evidence/reports/author-final.md.txt)及 `final-result-index.json`、`final-browser-matrix.json` 原件已归档。各轮 `command/result/summary`、原始 raw、源/依赖前后指纹、exact IDs、PID/starttime、实际wait及双清都通过[逻辑原件索引](public-account-entry-verification-evidence/original-map.json)定位。未被selector选择的包不是额外通过或业务跳过。

| 轮次 | 实际结果与保留事实 |
| --- | --- |
| new01/input03 | driver exit1。邀请浏览器通过但Go后态text/uuid比较失败；reset私有IPC立即查询失败且未记录精确错误类别，不能后来写成已观察ErrNoRows。原cleanup=false，后续归属确认和定点双清另记。 |
| new01-retest01/input06 | exit1；邀请通过，Reset实际202后CDP `Network.getResponseBody: No data found for resource with given identifier`。 |
| new01-retest02/input07 | exit1；202 shape及真实Outbox IPC已推进，实际204后UI uncertain；独立空流反例促成input08修复，不推断未采样原body。 |
| new01-retest03/input08 | exit1；邀请通过，Reset再次在首202 CDP取body失败，尚未204。 |
| new01-retest04/input09 | exit0；Invitation、PasswordReset两顶层/PW/PG通过，含实际202 shape、Outbox、204、两个旧Session撤销及新旧密码事实。 |
| new02/input09 | exit1；IdentityNavigation PASS。Privacy在known202后原4s内未见reset行，unknown和布局尚未运行；原cleanup=false与后定点双清分别保留。 |
| auth01/input09 | exit0；SessionLifecycle、RotateChallenge及对应浏览器例通过。 |
| auth02/input09 | exit0；RevocationAndExpiry、LayoutsAndProduction通过。 |
| settings01/input09 | exit1；ProfileAndAvatar PASS，Theme在真实409后旧 `conflict.json()` CDP取body失败，后续断言未完成。 |
| settings02/input09 | exit1；AuthorityAndProduction PASS，Password在实际401后旧 `refused.json()` 同类失败，未完成最终后态。 |
| remaining-retest01/input12 | driver/observer exit1，129.94s；Privacy PW10.4s/Go16.03s PASS，Theme PW10.5s/Go15.82s PASS。Password在更早 `wrong.json()` 的真实change-password400采样再红，未走到weak及401观察点；不能把未到的weak称作本轮失败。 |
| password-retest01/input13 | driver及observer实际exit0，总140.63s；Password PW7.7s/Go13.06s PASS，两个400字段错误、旧密码401、Session替换/撤销、精确2命令/2Audit和后续偏好写均完成。其他11顶层按不变范围复用。 |

CDP采样根因始终未被确认，不能写成产品body丢失或生产cancel导致。独立另以一Chromium/两context的40个纯本地合成202响应比较读取方式，全部可读，没有复现正式fixture错误；这一诊断不能证明任何采样方式普遍可靠。

作者准备阶段的缺依赖、exit127、安装失败/挂起及实际终止、缺一份不变主题文档资产，core02修复回归、204新测试预期形状错误、input10 vet取消泄漏、input11 tsc遗漏DOM.Iterable等原件保留。后续正确命令或修复没有重写此前退出值，也未升级package/lock或降低旧断言。

## 测试接缝修订的依据与限制

rev2只增加旧authentication fixture的私有 `recoveryLogPath` 字段及 `cfg.AccountRecoveryLog()` 赋值，使新helper读取本fixture精确受限日志；不搜索父目录、不猜路径。正式Account维护每10s调用Recover，单轮2s，后续Accountmail循环1s；202只确认受理，原4s/默认5s等待不能覆盖正常周期。新握手由浏览器IPC起算同一绝对20s截止，PG查询、精确日志匹配、应答和浏览器剩余poll共用，不叠加新20s；原45s/PW、2m/Go顶层、6m/包预算保持。

input10独立发现到期后诊断沿父ctx再查询可能多用1s，input11改为过期不发新查询、仅使用已有安全快照；未到期错误诊断仍沿同一握手ctx。input12实际阶段记录出现原4s附近仍accepted/pass0，之后processed/intent/outbox/log事实成立；这是新轮证据，原new02数据库已销毁且无相位记录，仍不能追认为旧首红唯一根因。

input09的202观察和rev3–5旧Settings观察仅对实际失败点及获准相邻点使用一次原响应clone：同源闭集method/path/status、一次原fetch、同一原Promise/Response、完整有界UTF-8/JSON读取；读/解析/cancel错误明确失败，finally实际join/read/cancel/release并恢复fetch、删除私有状态。原Playwright状态、202完整shape及known/unknown相等、VERSION_CONFLICT/UNAUTHENTICATED、两个400的field_errors原字面断言保持。

**clone会tee响应体，可能改变分支消费和时序；这些观察仅证明实际raw JSON形状，不能证明未instrument的原stream或共享owner尾部。** 独立实际204及跨账号CSRF组合不使用CDP JSON或fetch-clone观察。rev5沿同一第25旧spec增加 `POST /api/v1/me/change-password` 的400闭集，替换wrong与相邻weak采样；后者先前未到，不能写成已失败。无响应替换、拦截、重发、额外业务写入或扩大超时。

卡修订原件与指纹均保存：rev2 `1f67bc95968bf6e1de090ab0344a7f15c957215e`，rev3 `309a6c8c5a0087ec081e75c2e2e4fa984081a0ff`，rev4 `d63afeb69d48e839d24989521568b8cb548b0deb`，rev5 `11c5b9cfb63022895d0f56c10332a573e88ca0e1`。路径总数从23增加至25，产品范围未扩大。

## 独立真实业务与已验后端组合

input08独立原顶层 `TestAccountPublicEntryIndependentResetAuthority` 实际PW1 PASS 8.7s、Go15.00s、driver/observer exit0。A为B申请并重置后，B原Session确为401且持久`password_reset`撤销；A仍为原User/Session，零自动login/logout且reset-complete一次。A重置前原CSRF在同Cookie下完成真实preferences写，版本递增，精确一条B重置及一条A写入的命令/Audit/receipt归属成立。材料不在URL/history/DOM/storage，目标邮箱未猜入UI，成功清密码输入。此例不替代作者known/unknown raw-shape隐私覆盖。

[组合报告](public-account-entry-verification-evidence/reports/independent-combined.md.txt)固定manifest SHA `6443965d6806efbae71e0f83e33347bb794372d7e3e877a0e206cfcd10a6f03e`：input09前端23及15 dist、c54依赖、已验management13与ledger20，共1012文件。全部56 scoped源与其余956依赖/dist前后指纹匹配；未包含活动tools14，原adapter仍为c54字节。

原独立浏览器case/config/helper保持，Go仅增加两个只读前置：实际编译的EmbeddedSource target18/18条目/SQL18文件模式及checksum；真实root启动后的PG含goose版本0..18共19项、精确compiled journal信息及三张usage表。SQL18 checksum为 `sha256:b5d3f487853ff26bc2964a5c0958198c65f5048896935ed3fbfffc667c16a101`，不是合成历史。实际PW8.9s、Go14.76s PASS，driver/observer exit0，总107.30s。后续纯测试差量经独立核对可复用此生产组合，不声称整个未来main或无关域已验。

## 资源与尚未覆盖范围

每次正式fixture观察4个容器、3个网络的nonce及exact IDs；两次absent、原2容器/4网络ID/name/labels不变、所属PID/starttime终结、runtime/TMP清空和实际wait后交窗。new01/new02原cleanup=false均保留，只在确认残留目录归属且无进程后定点清除并另记两次复核；不覆盖原false。new02的manual schema未记录 `remaining_owned_browser`，不能补写true；原两次该字段为空，manual两次记录observed PID为空、runtime空、exact IDs absent及基线不变，协调者另复核交还。

input12末轮实际观察46个browser PID/starttime及7个资源ID，原双清通过。两个独立业务轮各观察19个browser PID/starttime、实际wait4个adopted进程，7 IDs双次absent、基线不变、runtime/TMP空。最后input13单Password实际观察19个browser PID/starttime与7个exact IDs，两次清零、原基线不变、runtime/TMP为空，observer实际wait退出0；协调者02:33:35 UTC确认窗口归还。最后原raw SHA为 `3490e92ab4277ac7c22cf33232619d0464005617d356c9cd8cb7905192a1b81c`，没有引用前轮清理替代。

真实渠道为backend_log；SMTP只按本卡strict DTO/UI纯测及既有D07后端证据，不声称新增真实邮件投递/browser验收。CSS `zoom=2`仅验证CSS重排，非原生浏览器缩放；生产SPA托管与真实Vite proxy未验。完整D26/D27、Project/D25、未决Summary、已知Object Runtime join缺陷、readyz503和其他产品接通边界保持。原tools任务受阻及Anthropic仅规格状态不由本页改变。

[归档说明](public-account-entry-verification-evidence/README.md)与只读校验脚本仅核文件哈希、候选映射与历史事实，不执行保存的驱动脚本或重新占用任何资源。
