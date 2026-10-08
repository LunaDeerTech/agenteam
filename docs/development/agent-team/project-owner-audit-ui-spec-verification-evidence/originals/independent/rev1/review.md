Audit UI rev1 完整 SPEC STATIC：需修正 D1；当前候选不通过，STOP。

固定卡 SHA：efeadf1bf77236a7d6825f901197240651baa1123e2463c4e4dbe697c5fe796e；作者 freeze：144efac25182a9d8a1360cb3b125c9a170e8ec9de1fa43a1498f79769f495d4f。已逐节通读完整 rev1，复用准备阶段正式 HTTP4089/Owner24 来源，只补必要当前源码与枚举核对。未读或接受作者修订。

D1 是唯一必修。卡第27/106/183行要求双导航及当前项保持，但 ProjectNav.vue:17 仅在 route.path === settings 时设置 aria-current；useProjectWorkspace.ts:77 的 settings 固定为 /settings/general，由 ProjectWorkspaceView.vue:19 原样传入。因此新 /settings/audit 必定不选中“项目设置”。原20技术路径不含 ProjectNav，shared UI 又只读，workspace 只允许暴露 currentReadContext，实施者无法在现闭集内完成要求。

最小修订：仅追加 web/src/components/layout/ProjectNav.vue，形成21技术＋README22；保持设置 href/default 为 /settings/general，首页/品牌/其它共享UI不变。当前项用原 projectRoute 的精确同项目 general/audit 叶闭集，不能任意 prefix 匹配。同步路径计数、唯一写者/只读例外，并在既有新 App/真实 navigation 测试中断言两叶当前项、原默认目标以及其它项目/非法尾部不误选。root 已同意这一修订方向，原 rev1 仍保留。

其余未发现规格必修：31个精确11键输出分支、53个非空 action/25个非空 resource 过滤值及空筛选、13 Service/9关联与跨字段metadata；独立1MiB/原caps、Owner而非admin默认分支、Session完整identity与actual tail；稳定Get上下文、checking/路由复用/guard取消、显式重读；安全原body＋标准schema＋公开client原参数/状态/媒体/RequestID；正式app.Run/同库producer与受控生命周期边界；新3/旧10精确top、7/SMTP9资源、45/120含cleanup/6m/75s预算和实际wait/双清；8图可见范围及独立亲跑最小代表。workspace 现有 canonicalize 已保留解析后的 suffix，无需为 audit 另改原读写状态机。

作者 selfcheck01 的旧top少 Session 字样 FAIL 保留；selfcheck02 只修该名称后的作者静态 PASS不代替本独审。前卡24接受来自root正式0a939a7f通知，来源文档历史pending/后续header变化不冒技术漂移。

本结论仅规格 STATIC。没有运行 Go/Node/browser/build/schema/client/资源或 Git；没有修改产品或仓库文档。完整实现、动态及资源可执行结论均未授。修订后只需定点核D1及全文差量/数量一致，不重做已审契约或启动产品检查。
