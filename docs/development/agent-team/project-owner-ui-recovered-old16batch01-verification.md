# Owner 工作区 old16-batch01：前两轮通过、第三轮失败与退役验收

本批结果为 **2 PASS／1 FAIL／13 NOT_RUN**。接受前两轮作者实际执行结果与三轮 owned 退役证据的独立复核；个人资料与头像轮 `oldprofile01` 的原 FAIL 保留，没有完整 Profile/Avatar 接受。后 13 组、独立实际 A/B、README #22 与完整 D27 不在此接受范围内。

[独立正式报告](project-owner-ui-recovered-old16batch01-verification-evidence/originals/independent/review.md)、[独立 evidence](project-owner-ui-recovered-old16batch01-verification-evidence/originals/independent/evidence.json)和 [STOP manifest](project-owner-ui-recovered-old16batch01-verification-evidence/originals/independent/manifest.json)固定后归档。[作者批次 JSON](project-owner-ui-recovered-old16batch01-verification-evidence/originals/author/old16-batch01-result.json)的 SHA 为 `adc742d93b38b0d0fe9594a331b118d286cacc0f006f9983f3faa7ba438472a1`；[作者摘要](project-owner-ui-recovered-old16batch01-verification-evidence/originals/author/old16-batch01-result.md)及各 handoff 中“独审待完成”保持交接时原文。本次只核文件字节、Git、JSON、格式、链接和固定记录，没有运行新测试、schema/client、browser、Go/list、资源或探针。

固定输入仍为 Git `367156d89773660c4a671a4b73d5ea7a16e24f50`、Go candidate04/browser-v5、UI19 `088f4d3490db4d86781090f0602299901c5f3247` 与 driver SHA `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e`。[来源映射](project-owner-ui-recovered-old16batch01-verification-evidence/source-map.json)继承 [nextnew01 永久档](project-owner-ui-recovered-nextnew01-verification.md)的 final04/955 输入绑定，并增加本失败独审所需的六个 Git 路径，共 48 项 Git/blob/SHA。独审七个 Git367 引用中的 `web/src/api/client.ts` 与已有固定版本字节相同，复用原映射；其余六项含原 personal spec、personal Go fixture/test、authentication spec/Go test 和 usePersonalSettings。未复制源码实体、全部 29 legacy JS、955 源图、工具或依赖。

[准备 handoff](project-owner-ui-recovered-old16batch01-verification-evidence/originals/preparation/handoff.json)与[准备独审](project-owner-ui-recovered-old16batch01-verification-evidence/originals/preparation/independent-review.md)只证明 16 个原 group/top 的选择、顺序、预算及退役条件，原授权 false 不动；原 list-tops01 结果仅复用名字枚举，不是本批业务通过。三轮实际使用单独的 [root truecopy](project-owner-ui-recovered-old16batch01-verification-evidence/originals/runs/oldauthlife01/frozen-input.json)，SHA `332a391ee12bf8f4142242363ce8e725e2683b8358013814b74361f340f818c5`，只相对原 final04 改 `permitted_groups`、`root_authorization`、`root_authorized_resources` 和 `status`。

每个旧 case 的内部预算、body、重试策略沿原 config 保留，没有套用 Project 新 case 的 45 秒。外层 top 120 秒含 cleanup、包 6 分钟、TCP tail 75 秒及每轮 fresh disk 门槛沿原 driver。只有前一轮实际 PASS、direct/adopted wait、watchdog join、owned/runtime/TCP 双清、输入核同后才能进入下一轮；任一 FAIL 先全退役再停止，不自动重试或继续。

| 原轮次／handoff | 精确 Go top | 原结果／Browser 摘要 | Go top／direct 秒 | 运行原件／字节 | adopted wait／TCP tail 秒 |
| --- | --- | --- | --- | --- | --- |
| [oldauthlife01](project-owner-ui-recovered-old16batch01-verification-evidence/originals/author/oldauthlife01-handoff.json) | TestAccountAuthenticationWebSessionLifecycle | PASS；1 passed，5.0 秒 | 7.60／53.563 | 24／240,998 | 4／39.394405484 |
| [oldauthrevoke01](project-owner-ui-recovered-old16batch01-verification-evidence/originals/author/oldauthrevoke01-handoff.json) | TestAccountAuthenticationWebRevocationAndExpiry | PASS；revocation 4.4 秒、expiry 3.1 秒 | 12.42／64.721 | 24／335,581 | 8／37.384781927 |
| [oldprofile01](project-owner-ui-recovered-old16batch01-verification-evidence/originals/author/oldprofile01-handoff.json) | TestAccountPersonalSettingsWebProfileAndAvatar | FAIL；1 failed，原摘要未列 browser 秒 | 8.54／58.746 | 24／324,196 | 4／38.399376900 |

三份 handoff 所列 72 份 run 原件共 900,775 字节，加上 3 份独立 launch 原件全部按 SHA/bytes 核合。撤销/过期 top 包含两次浏览器运行，不能计成两轮 Go top，也不能机械按每轮 4 个 adopted 计算。本批没有 Project safe HTTP/body/schema/client 原件或额外 legacy body 验证结果，不借用前批 52 schema／27 client；三个 images 列表为空，没有截图、当时 DOM 或 network trace。

第一轮实际通过登录、refresh、Session CSRF、退出等原断言，Go 确认相应 Session 已被 logout 撤销、成功 Audit 数为 2。第二轮 revocation 使用真实改密撤销另一 Session；expiry 先把一个 owned Session 行的期限移到过去，再由真实 GET/router 观察过期，不称等待自然期限。这两轮是作者实际执行加独立原件复核，没有独立实际 A/B。

第三轮的[原失败日志](project-owner-ui-recovered-old16batch01-verification-evidence/originals/runs/oldprofile01/raw.log)明确指向 Git367 的 `tests/account-captcha-web/e2e/personal-settings.spec.ts:441:27`，该原 spec SHA 为 `155bba1a7bf85ac5020c97b68dd45ade920b0e5016e6dd1f97d7a0202b1699ba`。最早失败是 `await duplicate.json()` 抛出：

```text
response.json: Protocol error (Network.getResponseBody): No data found for resource with given identifier
```

固定 `responseFor` 297–312 已等待到匹配 `PATCH /api/v1/me` 的 Playwright response，且 status400 断言通过。随后 CDP 获取正文抛错；441 的 `field_errors` 包含 `/username`／`ALREADY_EXISTS` 内容断言未完成。不能把本次记成 field_errors 值不符或 447 的 UI 断言失败，也不能根据 400 推断错误 JSON 正确、页面已经显示 aria-invalid 或产品完整通过。

按固定执行顺序与实际失败位置，之前已走过登录/Session/初始资料检查、首次用户名和显示名提交的精确请求值断言、成功提示，以及 reload 后 settings-admin canonical 和显示名回填，再提交重复用户名。这是冻结源加实际失败位置的顺序证据，没有额外 DOM/网络原件补强。缺失的原 body、当时 DOM、trace 使本轮不能确定 400 的具体内容、产品是否取消或保留了 body，或 CDP 丢失 resource 的底层原因。

441 内容断言、445 起 aria-invalid、449 当前 GET 保持 username，以及后续取消/清空显示名、native 最终输入、三种静态头像上传/回读、三类非法头像拒绝、移除和 storage 安全、`result(completed)` 均未完成或未到。Go requirePersonalFacts、8 次 committed writes/Audit、3 次上传与无 active avatar reader 的最终断言也未到。原 raw 中测试标题提及头像，不是头像验证实际通过。

独审把最早失败定位到测试 CDP 的正文取证层；静态产品使用原生 Fetch Response stream reader，读完后 finally join cancel/release。两种取证通道不同，可以说明观察层位置，但不能证明 cancel 就是此轮 CDP 错误的原因，也不足以排除全部产品因素。原第三轮 direct/outer exit1、driver accepted=false、Go FAIL 都保留；watchdog complete/joined 仅说明预算观察已完成，不把失败转成测试 PASS。

独审建议的最小后继修正，是在这一个额外 legacy spec 中复用既有 `settingsJSONFor`，给 `SettingsJSONTarget` 加入精确 `PATCH /api/v1/me` status400，再以同一次原生响应的观察值执行原 field_errors 断言。既有 helper 保留原 fetch promise/Response，不等待观察结果再交产品；同源精确 method/path、status 双核、calls==1、只 clone 有界错误 JSON、UTF-8 fatal／600000-byte 上限，并恢复 fetch 和 join read/cancel/release。clone/tee 仍是有界插桩观察，不是未插桩的 stream/owner tail 证明。

上述建议没有在本档执行或回填本轮：400、field_errors、aria-invalid、当前 GET、所有头像后段/facts、预算与重试必须保留，不能吞掉错误、跳过正文或另发请求替代。后继正式 freeze、独立差量、一源输入差量和真实 Profile 结果应另按授权范围交付。本档只读 Git367，未读取或覆盖正在修改的 spec；原 v04 失败记录及失败守卫保持，后继输入不能混回本次运行。

三轮各 4 容器/3 网络、7 个实际 ID，全批 **21 个不同 ID**，每轮逐 ID 两扫 absent；owned processes/runtime/browser-runtime 双空、Docker baseline 保持。三个 direct 均完成实际 wait，exit 分别 0/0/1；16 个 adopted（4+8+4）均逐 PID/starttime 对照 observed，actual wait/exit0；三个 watchdog complete/joined，monitor/cancellation/forced 均为 0。

| 轮次 | direct PID／starttime | 实际开始 UTC | TCP 最后空扫 UTC | 新 non-owned PID1 shim：PID／starttime |
| --- | --- | --- | --- | --- |
| oldauthlife01 | 241248／1336254 | 07:11:27.820251 | 07:13:02.150733 | 241473／1336470、241826／1336713、242193／1336933、242793／1337112 |
| oldauthrevoke01 | 250475／1358509 | 07:15:10.369423 | 07:16:53.834427 | 250910／1358990、251365／1359406、251733／1359626、252356／1359839 |
| oldprofile01 | 260240／1375349 | 07:17:58.772437 | 07:19:37.294799 | 260608／1375727、260964／1375974、261314／1376181、262053／1376578 |

时间均为 2026-10-08 UTC。各后继开始晚于前轮 TCP 终清；最后两扫 active/time-wait/new-host rows 均为空。三份 Docker baseline 相同、六份 input-before/after 同 SHA `af0a368fa712ae80c72e6edf0ac19ecaa6914b59c33ce5b1a189c2cb9b65a932`，三份授权/driver/verification input 一致。12 个新增 PID1 daemon shim 为 non-owned、state Z、未 wait，与 16 个 adopted 分开；历史非 owned 进程未触碰。不称全机清零，host TCP 只是补充增量轮询，不是完整短连接 trace 或所有权证明。

[root 交换记录](project-owner-ui-recovered-old16batch01-verification-evidence/originals/root/exchange-old16batch01.json)的 53 份固定测试资产在三轮间保留；[root 恢复记录](project-owner-ui-recovered-old16batch01-verification-evidence/originals/root/restore-after-old16batch01.json)为 07:21:10.668955 UTC，晚于末轮 TCP 终清，reader-preflight=[]，原 3 文件的路径、bytes、SHA 与交换前一致。退役后有意恢复不算运行中漂移。本档只消费固定交换/恢复记录，没有读取当前 host、资源或 globaldist。

第三轮失败退役后，顺序 4–16 的以下原 group 均 **NOT_RUN**，精确 top 和顺序见原[批次结果](project-owner-ui-recovered-old16batch01-verification-evidence/originals/author/old16-batch01-result.json)：

| 顺序 | 未运行 group |
| --- | --- |
| 4 | old-personal-theme |
| 5 | old-personal-password |
| 6 | old-invitations |
| 7 | old-providers |
| 8 | old-models |
| 9 | old-selection |
| 10 | old-summary-recovery |
| 11 | old-summary-authority |
| 12 | old-account-security |
| 13 | old-smtp-settings |
| 14 | old-smtp-delivery |
| 15 | old-outbound-policy |
| 16 | old-public-entry |

原完整 16 组的 114 ID 是准备计划，不能当作实际结果。只有 SMTPDeliveryTestAndRetry 计划额外 SMTP 容器/网络使该组为 9，其他 15 组计划各 7；Invitation outcome 和 SMTPSettings 不加 SMTP 资源。这些未启动组不能从前两轮或历史旧版本继承实际通过。

归档共 104 个逻辑原件引用、1,221,820 逻辑字节；新增 62 个实体、834,054 字节，复用 18 个已提交实体、129,964 字节（37 个逻辑引用），以提交 `3ae43e9b4d68e80b99d7c6075eb8f3ee831f12bb` 锁定共同原件。原 FAIL、原成功和历史时点均保留，没有复制全树/cache/assets 或私有材料。[格式例外](project-owner-ui-recovered-old16batch01-verification-evidence/format-exceptions.json)只有三份原 raw 的 18 处 trailing whitespace：life 31/33/36；revoke 32/34/37/41/43/46；profile 31/33/35/36/38/47/48/50/51。所有本档原件均无缺 EOF、CRLF 或单独 CR；不沿用前批的格式例外计数。

[归档自查](project-owner-ui-recovered-old16batch01-verification-evidence/archive-checks.json)给出来源/实体字节、48 Git、JSON、链接、原失败和退役记录的有界核验。此前 read01/edit01/edit02 失败档和五新作者分版结果保持各自范围；本批未完成的 Profile/Avatar、余13、独立实际 A/B、README22、完整 D27、生产 Skills/root/创建 HTTP、ready503、D08–D28/E01 与三项停止边界均不由本报告解除。归档完成即 STOP，无新增业务接受或资源执行。
