# Owner 工作区独立 A/B 九轮真实验收

限定接受独立验收负责人**本人实际执行的 A3＋B6 九轮**及 owned 完整退役，另接受本轮自产8张布局图的可见区域检查。九轮均使用各自 root 单轮授权、fresh fixture、原输入门禁及真实浏览器/API；没有以作者日志代替独立执行。本结论限冻结计划的九个代表，不扩为完整D27、生产绑定或后续品牌验收。

[正式报告](project-owner-ui-independent-ab-verification-evidence/originals/final/review.md) SHA 为 `4942c4deed2b32dca67827fcb66d7ec11bc63dd0c2822329928ded26a6a0a00e`；[evidence](project-owner-ui-independent-ab-verification-evidence/originals/final/evidence.json)、[原件索引](project-owner-ui-independent-ab-verification-evidence/originals/final/original-index.json)和 [STOP manifest](project-owner-ui-independent-ab-verification-evidence/originals/final/manifest.json)分别固定为 `33fa1db9…`、`dde86f3f…`、`1f7c094c…`。完整SHA、逻辑来源与永久实体见[来源映射](project-owner-ui-independent-ab-verification-evidence/source-map.json)。

| 单轮原 handoff | group | Go top／browser 秒 | schema／native client | 实际资源ID |
| --- | --- | --- | --- | --- |
| [A1](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-a-read01.json) | new-read | 17.41／8.6 | 46／16 | 7 |
| [A2](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-a-edit01.json) | new-edit | 12.74／6.3 | 19／7 | 7 |
| [A3](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-a-identity01.json) | new-identity | 18.08／10.2 | 17／11 | 7 |
| [B1](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-b-recovery01.json) | new-recovery | 13.11／5.3 | 19／8 | 7 |
| [B2](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-b-summary01.json) | old-summary-recovery | 12.71／8.6 | —／— | 7 |
| [B3](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-b-sumauth01.json) | old-summary-authority | 16.05／10.9 | —／— | 7 |
| [B4](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-b-auth01.json) | old-auth-lifecycle | 5.88／3.3 | —／— | 7 |
| [B5](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-b-smtp01.json) | old-smtp-delivery | 20.84／14.2 | —／— | 9 |
| [B6](project-owner-ui-independent-ab-verification-evidence/originals/handoffs/ind-b-layout01.json) | new-layouts | 14.70／7.5 | 16／8 | 7 |

五个 Project 轮合计 **117份 schema 校验、50次原生浏览器同字节公共客户端校验**，client计数为 list10／resolve12／get23／problem5。四个 legacy 轮没有这些 Project checker，不能把表中空项当作零次失败或套用Project覆盖。Setup、IPC helper、PATCH及其他写响应不计入浏览器GET client；原body、request ID与运行时URL／Owner的绑定由冻结实现和当轮实际成功checker共同证明，运行中Map没有另存为完整网络transcript。本归档只核原校验结果文件，没有再次运行schema／client checker。

A1覆盖读、身份／路由与 dotted Resolve→Get；A2覆盖显式编辑／改名、确认后当前读取失败与重读，原两处harness定位器修复后的完整后段已执行。A3覆盖Logout／Owner、checking／新Session、迟到尾部和共享Cookie owner，以及Project／System／Summary草稿聚合。B1实际提交后丢失HTTP响应，再经unknown后的显式original lookup／replay、历史archived receipt与本地放弃；in_progress／not_observed仍是受控状态，不构成PostgreSQL COMMIT-ACK丢失证明。

B2／B3分别实际复跑Summary恢复和权限／导航代表；B4覆盖登录、刷新、Session CSRF与退出；B5仅证明自有SMTP端点协议结果及显式重试，不证明外部邮箱送达。私有Skills、archived、lifecycle与authority SQL是fixture事实准备，不据此接受D10、生命周期参与者、生产角色管理API或Summary生成／Invocation。

B6矩阵前实际经过键盘打开／保存／取消、离开确认焦点与Escape恢复、冲突重读后显式采用；矩阵内检查主题／媒体、导航、焦点与document/body/main/form横向溢出，之后到达archived readonly、读取错误、空列表及verifyBodies／complete终局。这些行为不等于每张截图分别展示这些状态。

以下8张PNG由独立执行者自产并逐张查看，8文件／8不同SHA；归档仅校核原bytes和PNG尺寸，复用其正式视觉结论。

| viewport／motion | light 原图 | dark 原图 |
| --- | --- | --- |
| 1440×900／no-preference | [light desktop](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-light-1440-no-preference.png) | [dark desktop](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-dark-1440-no-preference.png) |
| 1440×900／reduce | [light desktop reduce](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-light-1440-reduce.png) | [dark desktop reduce](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-dark-1440-reduce.png) |
| 390×844／no-preference | [light mobile](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-light-390-no-preference.png) | [dark mobile](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-dark-390-no-preference.png) |
| 390×844／reduce | [light mobile reduce](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-light-390-reduce.png) | [dark mobile reduce](project-owner-ui-independent-ab-verification-evidence/originals/runs/ind-b-layout01/screenshots/project-owner-dark-390-reduce.png) |

可见区域字段与焦点可读，移动端纵向堆叠、身份值换行，未见重叠或横向裁切。原选项为 `fullPage:true, animations:disabled`；390截图没有捕获内部滚动区下部全部描述和操作，不证明动画过程、原生zoom或数值对比度，也不外推后续品牌像素。

[独立false准备](project-owner-ui-independent-ab-verification-evidence/originals/preparation/freeze.prepared-final05.json) SHA `061863b0…` 与[九轮计划](project-owner-ui-independent-ab-verification-evidence/originals/preparation/rounds.json)保持原文；[final05重绑定报告](project-owner-ui-independent-ab-verification-evidence/originals/preparation/rebind-final05-report.md)当时明确没有新门禁或实际A/B PASS。后续九份immutable grant逐轮另发，与各轮frozen-input逐字节一致，按SHA合并实体而保留独立来源映射；没有把准备状态回写为执行结果。

九轮首个原driver输入门禁各接受1165文件，before／after相同，实际digest均为 `6bb347c9ea8c32d19dd0176302019b64ddeaec778ad14dbd8011a1bc88436f55`。955 repository paths／66 sets／210 runtime files沿原final05、UI19／Go04／browser-v5、Git80ec单legacy修正及当时53资产继承。新case45秒／workers1／retries0，原legacy预算、top120秒含cleanup、包6分钟、TCP75秒及fresh5GiB保持。十项必要Git/blob/SHA来源只引用已有永久映射，未重扫源码／工具图或复制依赖；根README的picture在固定图外，也不能借九轮结果声称后续品牌已验收。

实际 **65个互异资源ID：37容器＋28网络**，仅SMTP为9，其余各7。每ID两次absent，**9 direct＋36 adopted实际wait、9 watchdog join**；owned process／runtime／browser-runtime双空，baseline保持。每轮启动晚于前轮TCP终局，monitor／forced／retry均0。37个新增daemon-owned PID1 shims逐身份单列、未wait；owned退役不等于全机无进程／无僵尸，TCP补充轮询不等于完整短连接轨迹。

末B6 TCP双清为 **2026-10-08 09:34:35.005724 UTC**。[root原3恢复记录](project-owner-ui-independent-ab-verification-evidence/originals/root/restore-after-indab01.json)时间09:35:36.635591 UTC晚于末尾清理，SHA为 `d22c8a582143a991e05b831ab20dab4000636c4ebe32208042e3b4288021aae4`，记录原3精确恢复、reader_preflight=[]、53保留stage。[原窗口交换记录](project-owner-ui-independent-ab-verification-evidence/originals/root/exchange-indab01.json)单独保留。原恢复记录当时尚待正式九轮摘要的文字不改；正式末件另行固定，窗口关闭后的恢复不构成运行中漂移。本归档没有读取current dist或主机。

此前作者[read02](project-owner-ui-recovered-read02-verification.md)、[edit03](project-owner-ui-recovered-edit03-verification.md)、[其余新三轮](project-owner-ui-recovered-nextnew01-verification.md)与[旧2＋14限定组合](project-owner-ui-old14-success-verification.md)仅作为已接受版本组合引用，不冒充本九轮或全部legacy16独立重跑。原[read01](project-owner-ui-recovered-read01-verification.md)、[edit01](project-owner-ui-recovered-edit01-verification.md)、[edit02](project-owner-ui-recovered-edit02-verification.md)、[oldprofile01](project-owner-ui-recovered-old16batch01-verification.md)及[格式／类型失败](project-owner-ui-legacy-profile-json-verification.md)继续保留；本次成功不能补写旧轮缺失的正文、UI或终局事实。独立准备时的记录器技术FAIL与有限文件PASS限制亦按原报告保留，没有升级为新的产品结果。

归档共 **479逻辑原件／5,562,402字节、403个不同SHA**：包含完整429 run原件／4,556,881字节、9 launch／164,240字节、9 handoff／89,071字节、9 outer终局、9 grant及必要准备／末件／root记录。去重后新增 **385实体／4,705,177字节**；18个既有永久实体／88,855字节原位复用，承载85个逻辑引用。相同SHA的76个额外逻辑引用没有丢弃；八项旧作者组合引用另列，不混入479件计数。没有复制whole source、runtime、cache或完整闭包。

[归档自查](project-owner-ui-independent-ab-verification-evidence/archive-checks.json)仅记录SHA／bytes、JSON、来源／链接和文本格式；[原格式例外](project-owner-ui-independent-ab-verification-evidence/format-exceptions.json)按实体保留204份无末尾LF的原JSON与9份含尾空白的原raw，不normalize。生成文档／JSON保持UTF-8、LF和末尾换行。归档阶段未运行Go、Node、浏览器、schema/client checker、业务、资源、Git、网络或停止项probe；原件核完后STOP交root，README #22和后继品牌收口由各自授权任务处理。
