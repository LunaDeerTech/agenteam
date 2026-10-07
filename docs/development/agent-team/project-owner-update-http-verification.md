# D08 Project Owner Update HTTP 验证

2026-10-07：[规格](../work-items/d08-project-owner-update-http.md)的完整19路径（18技术源及后端README末件）已独立有界通过并由root采纳，产品 `61bed1fcf47c362f420f6bb45158b227af581acf` 已提交推送；root已核远端一致。固定前置为 Owner-read `901eb546`，规格 `03d1c107`、[规格归档](project-owner-update-http-spec-verification.md) `221ec4ba`。[完整19末件结论](project-owner-update-http-verification-evidence/originals/independent/final19/result.json)继承[18技术独审](project-owner-update-http-verification-evidence/originals/independent/final01/review.md)，不把早期原报告的待README状态改写。

## 1. 接受结果与证据组合

当前Owner可严格PATCH名称/描述，并以原key查证仅update的历史观察；保留presence、原body/version、当前Session/Owner先于历史回执、safe ProjectRef及原Unknown分类。默认root使用同一ProjectAuthority、真实Audit/Outbox两侧gate、正式catalog和真实Service；只有实际Drain返回nil才记Joined，Force不伪造join。当前GET与历史回执分开，HTTP不增自动查证或重试。Project创建/初始化/生命周期、UI及生产Resolution/Invocations/D24仍未绑定。

| 固定证据 | 实际结果与适用范围 |
| --- | --- |
| [作者最终交付](project-owner-update-http-verification-evidence/originals/author/author-final06.md)、[离线组合](project-owner-update-http-verification-evidence/originals/author/offline-summary06.json) | 原HTTP20顶层/110嵌套普通与race，加06受影响5顶层/34嵌套；app13顶层/69嵌套普通/race、必要编译/vet/两cmd通过。未变Project库按1819实际选中文件及generated TestMain匹配复用。是版本组合，非最终同次全包重跑。 |
| [作者native三轮](project-owner-update-http-verification-evidence/originals/author/native-driver-v02/result-summary.json) | 3顶层/7子测、8 listeners；direct3及adopted3均实际wait exit0，每轮owned进程与全部任务TCP含TIME_WAIT双空。自然PATCH约30.02s、lookup约2.01s，执行预算与后续退役尾部分记。 |
| [独立HTTP06](project-owner-update-http-verification-evidence/originals/independent/http01/review.md) | race 1顶层/11嵌套（9叶）；缺Flush零业务调用、精确64KiB PATCH/1KiB lookup及+1拒绝、Body.Close与cancel callback独立阻塞尾部通过。 |
| [作者PG五轮](project-owner-update-http-verification-evidence/originals/author/pg-driver-v01/result-summary.json)、[独立最终结果](project-owner-update-http-verification-evidence/originals/independent/final01/result.json) | 作者新4顶层/6子测、旧9顶层/20子测通过；独立A01补真实缺锁/旧plan/已提交Logout、原writer回滚与原key重试，B02验证默认root、8192-byte UTF-8描述、当前v3对历史v2、重放/lookup隔离/resolve/实际Drain。A01复用，B01失败后仅B02重跑。 |
| [独立真实原body/schema](project-owner-update-http-verification-evidence/originals/independent/schema01/result.json) | B02原PATCH8545B、lookup8607B按同一字节通过Draft202012/FormatChecker和固定本地refs；method/path/status/Content-Type/length/target/run/candidate/schema指纹均匹配。作者另有364B/426B响应保留，不拿它们替代B02验证。 |

## 2. 原失败、修订和边界

[作者原失败说明](project-owner-update-http-verification-evidence/originals/author/preflight-first-red.md)及[独立初审](project-owner-update-http-verification-evidence/originals/independent/static01/review.md)保留app名义ProcessID/测试Store接口编译红、readyz测试假设和真实gate/Unknown/root覆盖缺口。candidate02只作私有typed-ID适配与测试修正，04补所需真实组合。[U04原发现](project-owner-update-http-verification-evidence/originals/independent/static02/U04-addendum.md)纠正04只具deadline却可能先执行业务再发现缺Flush的问题；05仍在Unwrap panic前缺尾部所有权，06将空adapter/budget/defer先安装，再有界解析各能力，生产问题闭合。

05人工循环writer在旧WithRequestID的stateOf提前stack overflow、尚未进入新handler的原exit2/raw保留；06只在正式RequestID建立后注入该测试，不外推旧middleware整体能防环。独立首graph误传plan SHA，在Go子进程前门禁拒绝；probes01首编译三处缺nil参数也保留。B01辅助SQL `SELECT key` 的原DATABASE_SQL_FAILED及全轮清理原件保留；[probes03归因](project-owner-update-http-verification-evidence/originals/independent/probes03/attribution.json)与[单行修订](project-owner-update-http-verification-evidence/originals/independent/probes03/delta.diff)仅改为 `command_key`，未改产品/导入/文件集合；受影响compile/vet/list/输入门禁后仅重跑B02。

held COMMIT的reached只代表收到尚未转发的原帧，writer未终局；释放同一原COMMIT并观察server terminal才证明该终态。原WithoutCancel确认尾部与HTTP/Drain实际退役分列；30s为发布/I/O预算，不承诺整个handler必在30s返回。正常3s与合法100ms耗尽关停分别验证；forced root返回不证明所有内部组件已join，其后私有proxy/backend释放及实际等待不能倒填root join。作者Logout排队不冒确定抢先，独立A已提交Logout补互补顺序。正式Bootstrap/Invitation/Redeem/Login与旧SQL身份、Skills测试前置/liveProcess辅助的边界保持，不提升为生产绑定。

## 3. 资源、保存范围及归档自查

[八轮精确资源核对](project-owner-update-http-verification-evidence/originals/independent/final01/pg-rounds.json)涵盖作者5轮及独立A01/B01/B02：每轮实际wait、7个精确资源（共56个不同ID）、owned/runtime双清及前后输入一致，失败B01亦完整清理，没有forced tail或monitor error；测试窗口已归还。原固定观察期PID1 Z由217到250，32个新增daemon shim以实际PID/starttime集合单列，另1个轮间git Z（676396/start2562836）不归任务。daemon不是owned、未task-wait，不称全机清零，也不把环境恢复前176当本轮基线。

[来源映射](project-owner-update-http-verification-evidence/source-map.json)保存285份去重原件（含两份root检查原记录）、88份明确派生JSON摘录，另保183个同字节来源别名及32项仅指纹来源。重复的大input map从摘录中省略，原SHA/bytes、省略字段和保留值逐项可核，摘录不称原件；大raw graph不能仅凭本档完整重建。native完整观察JSON及首/最终两次原TCP样本保留，中间重复raw不逐项归档。产品19源及README复用接受commit，不复制源码树/cache/binary/私密runtime。所有原失败、实际命令、必要raw与实际wait/清理证据均按这些明示保存范围处理；原Markdown引用保持其来源位置语义，原patch/trace空白不改。

重启前收集命令已exit0；恢复后257个已落盘文件hash全部匹配，没有重跑产品或覆盖原件。归档worker仅自查来源及副本字节/hash、派生字段、JSON/脚本语法、正文链接和UTF-8/LF/新增格式，未运行原脚本、Git、Go或资源；其中原件空白自查仅枚举14份文件的尾空白，并非完整Git空白检查。系统管理员统一会议initial/update含首轮标题、Project无override/复制默认及compaction/Execution Summary既定规则保持；ready503、完整D08–D28/E01未完、E01未开始及Object runtime join/OpenAI tools独立验证/SPA concurrent-publication三停止不变。

root随后对原378路径实际执行 `git diff --cached --check`，得到16份原diff/TCP文件的357项：尾空白、diff上下文space-before-tab及空EOF。前三次checker使用过窄预期集合而失败，未改变源字节；[最终核对原记录](project-owner-update-http-verification-evidence/originals/root/root-raw-whitespace-review.json)按原因保留该过程，并确认均为原件；[Git原输出](project-owner-update-http-verification-evidence/originals/root/root-staged-diff-check.txt)逐字保存。16/357限定该原378路径检查；新增原输出自身也含被展示的空白行，同样保原字节，不改原raw/hash或增加ignore规则，也不递归归档后续检查输出。这些检查器失败不是产品失败。
