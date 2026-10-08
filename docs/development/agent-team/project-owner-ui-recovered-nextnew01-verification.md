# Owner 工作区 nextnew01：三成功轮与有限视觉验收

接受作者 `recovery01`、`identity01`、`layouts01` 三个真实场景的完整通过结果，以及独立负责人对固定原件、owned 退役和八张截图可见区域的复核。三个原始运行没有重新执行；本次归档只核固定字节、JSON、PNG 头、Git 引用、格式和链接。独立实际 A/B、旧 16 组、README #22、完整 D27 与生产能力不在此接受范围内。

[批次独立终局报告](project-owner-ui-recovered-nextnew01-verification-evidence/originals/independent/nextnew01/review.md)、[作者批次结果](project-owner-ui-recovered-nextnew01-verification-evidence/originals/author/nextnew01-result.md)与[机器结果](project-owner-ui-recovered-nextnew01-verification-evidence/originals/author/nextnew01-result.json)按原字节保留。作者结果中的 `visual_review_pending=true` 是交接时状态；后继独立负责人已经逐张原尺寸查看，结论只覆盖下文可见区域，没有改写作者历史结果。

固定 harness 为 Git `367156d89773660c4a671a4b73d5ea7a16e24f50`（Go candidate04、browser-v5），UI19 为 `088f4d3490db4d86781090f0602299901c5f3247`，driver SHA 为 `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e`。[来源映射](project-owner-ui-recovered-nextnew01-verification-evidence/source-map.json)保留 42 项固定 Git/blob/SHA；955 个仓库输入、工具闭包和 final04 单源差量沿已提交的 [edit03 档](project-owner-ui-recovered-edit03-verification.md)复用，没有重扫全图或复制源码、依赖、资产实体。

[小批次准备](project-owner-ui-recovered-nextnew01-verification-evidence/originals/preparation/handoff.json)与[准备独审](project-owner-ui-recovered-nextnew01-verification-evidence/originals/preparation/independent-review.md)仍保持原来的 false 授权状态。实际三轮使用单独的 [root truecopy](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/recovery01/frozen-input.json)，SHA `43a01651a84d3bde69f6b5be732056e13d4925df0924c1f443257580a8a62e2e`；它只相对原 final04 改动 `permitted_groups`、`root_authorization`、`root_authorized_resources`、`status`，按顺序允许 new-recovery、new-identity、new-layouts。上一轮精确 top、实际 wait、owned/TCP 双清和输入核同全部完成，才进入下一轮。

以下时间来自各轮原 raw、command、result 和独立终局；direct 指 shell/fixture 命令实际执行时间，TCP tail 是其后的补充观察，不能用浏览器耗时代替全链耗时。三轮外层实际 exit0、driver accepted、watchdog join 均有原记录。

| 作者轮次与独审 | 精确 Go top | 运行原件／字节 | Browser／Go top／direct 秒 | TCP tail 秒 |
| --- | --- | --- | --- | --- |
| [recovery01](project-owner-ui-recovered-nextnew01-verification-evidence/originals/independent/recovery01/review.md) | TestAccountProjectOwnerWebOriginalRecovery | 55／415,806 | 5.1／11.63／56.354 | 39.384153 |
| [identity01](project-owner-ui-recovered-nextnew01-verification-evidence/originals/independent/identity01/review.md) | TestAccountProjectOwnerWebIdentityAndOwnership | 52／427,099 | 11.0／18.31／67.830 | 38.381247 |
| [layouts01](project-owner-ui-recovered-nextnew01-verification-evidence/originals/independent/layouts01/review.md) | TestAccountProjectOwnerWebLayouts | 59／995,513 | 7.0／14.31／64.058 | 39.384670 |

三份 handoff（[恢复](project-owner-ui-recovered-nextnew01-verification-evidence/originals/author/recovery01-handoff.json)、[身份](project-owner-ui-recovered-nextnew01-verification-evidence/originals/author/identity01-handoff.json)、[布局](project-owner-ui-recovered-nextnew01-verification-evidence/originals/author/layouts01-handoff.json)）所列 166 份运行原件共 1,838,418 字节，逐 SHA/bytes 核合；各自 launch 原件也独立保留。schema 与客户端验证的成功记录来自实际作者轮次，本次没有重新调用验证器。

| 轮次 | Sidecar／独立完整 body | 原 schema status0 | 原公开 GET 客户端：List／Resolve／Get／Problem | 安全响应按 method/status 分类 |
| --- | --- | --- | --- | --- |
| recovery01 | 19／10 | 19 | 0／2／6／0，共 8 | GET200×13、PATCH200×5、POST200×1 |
| identity01 | 17／9 | 17 | 2／4／4／1，共 11 | GET200×16、GET404×1 |
| layouts01 | 16／9 | 16 | 2／2／4／0，共 8 | GET200×13、PATCH200×2、PATCH409×1 |
| 合计 | 52／28 | 52 | 4／8／14／1，共 27 | 52 份响应，按 body SHA 去重保存 |

每个 sidecar 的十个字段、body 原 bytes/SHA、input_hash、source_run、request_id 关联均沿原件核对。原客户端通过实际 X-Request-ID 与浏览器内存中的 URL/owner/status 关联，同一完整 body 构造 Response 后交公开读取 decoder；该逐请求内存关联没有另存完整 transcript。下表编号均指各轮 `response-NNN.json`，不能把准备、外部更新 helper 或已取消的 transport tail 计为浏览器客户端读取。

| 轮次 | 计入公开 GET 客户端 | 排除的准备／helper／其他响应 |
| --- | --- | --- |
| recovery01 | Resolve 005/016；Get 006/009/013/015/017/019 | setup 001–004；IPC 011 GET/012 PATCH；007/010/014/018 PATCH 与 008 POST lookup |
| identity01 | List 011/017；Resolve 006/008/013/015；Get 007/009/014/016；012 GET404 Problem | setup 001–005；010 为后端已完成、浏览器随后取消的 held GET；没有 Project IPC 更新 |
| layouts01 | List 005/016；Resolve 006/014；Get 007/009/013/015 | setup 001–004；IPC 010 GET/011 PATCH；008 PATCH200 与 012 PATCH409 |

POST lookup、PATCH 成功与 PATCH Problem 由原 schema 及实际页面操作覆盖，不增计公开客户端 mutation/lookup/Problem 重放。身份轮次的 Session/logout/System Summary 操作也不冒充这些 Project schema 响应；布局中的注入读取错误不是正式 Project Problem。

恢复轮次实际完成三段。007 正式 PATCH 已提交到版本 2，proxy 保存完整回执并核真实 commands.completed，然后保持 200/原 Content-Length、只发送一个 `{` 后关闭，页面进入结果不确定。008 正式 lookup 返回 committed，`result.project` 与 007 相同；查证次数增加、没有额外 PATCH、command/audit/event 不变的断言实际通过。存留的回执 body 是截断前后端完整字节，不能说页面已完整收到。此轮真实 lookup 状态只有 committed，未建立真实 PG 的 in_progress/not_observed 三态或数据库 COMMIT 确认丢失验收。

第二次丢响应的 010 为 active 版本 3；IPC 011/012 更新到版本 4，辅助归档事实准备后 013 为 archived 版本 6。用户显式重放原请求，014 与 010 的完整 body SHA 相同；随后 015 仍与 013 相同，页面保留较新归档描述和只读状态。固定 Go 对原 key/path/body/CSRF 摘要和回执字节的私有比较，以及 same_original、replayed=1、dropped=2 和唯一 facts 断言均实际执行；私有明文 key/body/观察 ack 已随 runtime 退役，没有归入持久证据。第三段 018 已提交 duplicate 项目版本 2，用户放弃本地追踪后重读 019 仍得到同 body，事实数量不变；放弃本地记录没有回滚服务器命令。没有完整 DOM 或永久私有 IPC 数值记录，接受依据是冻结断言实际完整 PASS、Go 完成握手和正式响应原件。

身份轮次实际覆盖：一次受控 Session 读取失败进入 checking 并隐藏 editor，同身份恢复保留草稿；退出确认选择继续保留草稿，正式 Logout204 后重新登录取得新 Session ID，旧草稿不恢复。held Project GET 的后端已完成、浏览器传输仍未完成，期间退出按钮受共享 Cookie owner 约束；导航列表并释放实际传输尾流后旧 editor 不再发布，后续请求均为 GET。这是固定浏览器传输尾流场景，不是后端事务尾流或全部 Cookie mutator 并发矩阵。

另一 Cookie context 对原 Owner 项目得到 404 NOT_FOUND、无编辑表单，能打开自己项目；主 context 切换管理员后打开其自有项目。草稿分两段：先验证 Project 离开到 System 时继续保留、显式放弃后离开；随后四用途 Selection 与 Meeting Summary 两份草稿同时存在，history 返回的聚合确认及继续/显式放弃实际通过。Project 草稿此前已经放弃，不能称三域草稿同时存在。Session、CSRF、密码和私有 IPC 比较值没有持久化；无完整 DOM 或截图补证。

布局轮次在八格截图矩阵之前实际完成键盘进入 Project/Settings、键盘保存确认/取消、离开确认焦点、Escape 关闭后恢复链接焦点并保留草稿，以及真实冲突后 fresh Get 与显式采用。矩阵之后又完成 archived 只读/禁用保存、注入读取错误、独立 Cookie 空列表、无 Debug 路由/资产和 verifyBodies/complete。008 PATCH200 后 009 读取同 body；IPC 011 成功后的 013 fresh Get 与其相同；014/015 是同一归档项目。辅助生命周期终态不等于生命周期参与者或 production runtime 运行接受。

八张原 PNG 均由独立 runtime 负责人按原尺寸逐张查看，可见文字、布局、Owner ID 换行和名称焦点边界通过。桌面图能看到字段和操作区；390×844 图只捕获内部滚动区上段，描述下部与操作按钮未入图。`fullPage:true` 并没有消除内部滚动裁切，不能扩展为所有手机控件的视觉接受。每格原断言验证主题、导航/aria-current、基本信息、横向溢出、名称 focus 和 reduced-motion matchMedia；截图设置 `animations:"disabled"`。静态图和媒体查询不证明原生缩放、动画过程或量化对比度；矩阵前后的 keyboard/readonly/error/empty 断言没有在每格单独截图。

| 原图模式／存档实体 | 原尺寸 | 原 bytes | 原 SHA256 |
| --- | --- | --- | --- |
| [dark-1440-no-preference](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-dark-1440-no-preference.png) | 1440×900 | 82,868 | `f9ec12ce7553c3747cd4d376be90a67925cda88f5a5885db206b74ba5d8f90ac` |
| [dark-1440-reduce](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-dark-1440-reduce.png) | 1440×900 | 82,876 | `8b468dae3b408bef5a1663ad62c02a365c1e50ea19344a82ab907a1831b6380b` |
| [dark-390-no-preference](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-dark-390-no-preference.png) | 390×844 | 59,056 | `961fcabdf8b347ce55283a5d9e8b9daae15ce79a66b671d1f165809bb2d6339f` |
| [dark-390-reduce](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-dark-390-no-preference.png) | 390×844 | 59,056 | `961fcabdf8b347ce55283a5d9e8b9daae15ce79a66b671d1f165809bb2d6339f` |
| [light-1440-no-preference](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-light-1440-no-preference.png) | 1440×900 | 82,278 | `beb0874dd609f27d1d14c8c4e8ffd4b8b4d899b937dea1309ce4dbaf8c4a7abd` |
| [light-1440-reduce](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-light-1440-reduce.png) | 1440×900 | 82,283 | `42b05638336ec39fad43663c149d422ba713053d766d193efcfcd83fd692030d` |
| [light-390-no-preference](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-light-390-no-preference.png) | 390×844 | 58,484 | `7c308eccb819e4a16dfed77302865407a3f5519229e7cd46bcb84bb9892618d3` |
| [light-390-reduce](project-owner-ui-recovered-nextnew01-verification-evidence/originals/runs/layouts01/screenshots/project-owner-light-390-reduce.png) | 390×844 | 58,484 | `eb7af11b9ee79a1b43b2e8612ecd2119e8b0e14cae288099fc8aa4b820a148da` |

八个原路径→实体的完整映射另见[来源映射](project-owner-ui-recovered-nextnew01-verification-evidence/source-map.json)。dark390 的 no-preference 与 reduce 原图 SHA 相同，保留两个逻辑原图记录、复用同一个未修改的 PNG 实体；总计 8 个逻辑原图 565,385 字节、7 个不同实体 506,329 字节。没有修改作者图片 manifest，也没有为两张相同静图补造动画差异。

三轮各实际观察 4 容器/3 网络、7 个 exact ID，全批 21 个 ID 互不重复。三个 direct 子进程与 12 个 adopted 子进程均有 PID/starttime 和实际 wait/exit0，且与 observed 原记录相合；三个 watchdog 已 join。每轮两次逐 ID absent，owned/process/runtime/browser-runtime 两扫空，Docker baseline 保持；monitor/cancellation/forced actions 均为 0。完整 ID、adopted PID/starttime 和两扫原件通过各 handoff 与[批次独立 evidence](project-owner-ui-recovered-nextnew01-verification-evidence/originals/independent/nextnew01/evidence.json)定位。

| 轮次 | direct PID／starttime | 实际开始 UTC | TCP 最后一次空扫 UTC | 新 non-owned PID1 shim：PID／starttime |
| --- | --- | --- | --- | --- |
| recovery01 | 199986／1164762 | 06:42:52.906367 | 06:44:29.985196 | 200214／1164999、200562／1165237、200927／1165433、201498／1165615 |
| identity01 | 209305／1183419 | 06:45:59.468637 | 06:47:47.055041 | 209677／1183866、210150／1184247、210460／1184425、210956／1184595 |
| layouts01 | 219440／1203932 | 06:49:24.604784 | 06:51:09.406069 | 219815／1204383、220298／1204740、220622／1204913、221223／1205109 |

上述时间均为 2026-10-08。每个后继 direct 开始晚于前轮 TCP 末扫；六份 input-before/after 都为 `af0a368fa712ae80c72e6edf0ac19ecaa6914b59c33ce5b1a189c2cb9b65a932`，三个 frozen input、driver、verification input 一致。12 个新增 daemon shim 属 PID1、non-owned、state Z，未由任务 wait，不能并入 12 adopted；历史非 owned 僵尸也未触碰。host TCP 是补充轮询，不是全部短连接 trace 或全机清零证明。

[root 资产交换记录](project-owner-ui-recovered-nextnew01-verification-evidence/originals/root/exchange-nextnew01.json)在条件串行窗口保留相同 53 份测试资产供三轮使用；没有把轮间未恢复算作输入漂移。[固定恢复记录](project-owner-ui-recovered-nextnew01-verification-evidence/originals/root/restore-after-nextnew01.json)为 06:54:53.303006 UTC，晚于全部 reader/TCP 退役，原 3 文件的路径、bytes、SHA 与交换前相同，测试资产保留于原记录所述目录。本归档没有读取后继资源窗口或当前全局资产。

这使五个新增场景的作者成功结果按版本组合为：read02 使用 browser-v3，edit03/recovery01/identity01/layouts01 使用 browser-v5。此前 [read01](project-owner-ui-recovered-read01-verification.md)、[edit01](project-owner-ui-recovered-edit01-verification.md)、[edit02](project-owner-ui-recovered-edit02-verification.md) 的原 FAIL 和未到断言边界仍保留；本批不回填旧运行。原 [API 有限验收](project-owner-ui-client-verification.md)和 [UI controlled 验收](project-owner-workspace-ui-controlled-verification.md)继续按各自固定版本复用。独立实际 A/B、旧 16 组与完整 D27/README #22 仍需另有证据；真实 lookup 三态、原生 zoom、production Skills/root/创建 HTTP、ready503、D08–D28/E01 与三项既有停止边界均不由本报告解除。

归档共 207 个逻辑原件引用，逻辑字节合计 2,222,066；按 SHA 去重后新增 161 个实体、1,759,266 字节，另复用 21 个已提交实体、150,436 字节（对应 40 个逻辑引用）。三轮完整原件、launch/handoff、批次准备/授权/交换/恢复、三独审与批次独审都按原 bytes 保留；固定产品只记录 Git/blob，不复制 955 源图、cache、依赖、资产实体或私有材料。复用来源以 edit03 归档提交 `7aa19f75cc1b125635cf5e85ab4d5d57021ca198` 锁定。

[格式例外](project-owner-ui-recovered-nextnew01-verification-evidence/format-exceptions.json)登记 89 个原件：52 sidecar、28 body、6 原 schema/client 结果共 86 个 JSON 缺 EOF 换行；recovery raw 的 33/35/38 行与 identity/layouts raw 各 32/34/37 行保留原 trailing whitespace。原件没有 CRLF 或单独 CR；7 个 PNG 实体只做字节与尺寸核验。检查只对实际路径前缀识别诊断，不把 raw 中的 `+ ...go:行号` 当作文件路径。[归档自查](project-owner-ui-recovered-nextnew01-verification-evidence/archive-checks.json)记录 SHA/bytes、42 Git、JSON、链接、图片映射、请求分类和退役原记录的有界核验。归档完成后 STOP，不追加业务重跑或动态接受。
