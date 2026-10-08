# Owner 项目工作区 UI scratch rev1 独立 STATIC

**NEEDS_REVISION：唯一阻断 B1。** 已完整审阅 frozen rev1 十节，复用已完成的固定源准备，未参与规格或产品实现。原稿 SHA `182dafda155613991980d0ebacf47efda39164eb7bd2f3d798615928a035ba18`、freeze `6680d2964553812dd5002d45efe7344610abc081591d0617e758652bf09bacfd` 保持不变。root 已采纳 B1，后续只需窄审 rev2 实际差量。

## B1：合法 Owner 被新增首段禁集拒绝

§3.1 第42行把 `forgot-password`、`reset-password` 整个首段排除出 Project 路由。但固定 Account `validation.go:40–57` 允许正式注册这些用户名，CurrentUserRoutes 的 `profile.go:38–51` 也认可；旧 router 只有各自单段公开密码入口。合法用户 `forgot-password` 的项目 `demo` 可以出现在 Owner 列表，却不能经 `/forgot-password/demo` 进入，本卡要求的列表→工作区完整结果未闭合。

最小修正是只从首段禁集删除这两项，保留 `/forgot-password`、`/reset-password` 单段公开路由优先及原 query/token 清理；其合法两段 Project 与 Settings 后缀按同严格动态 return/raw path 闭集并重新 Resolve/Get。补两个合法 Owner 的 direct-link/return，以及单段公开入口不被 Project 接管的反例。不改后端、创建保留名、其它保留 namespace 或候选范围。

## 其余完整审查无阻断

- 65 来源逐 SHA 相同；候选23路径（22技术、README #22、Session #23；15新8旧）存在性/旧 hash 精确。16旧真实 top 在固定来源各有唯一声明；15相对链接均存在。额外必要 wire/commands/AppShell 从 cc850b22 读取并与当前字节核同，未消费 Audit 活动实现。
- List/Get/Resolve 三投影与 Update/lookup 历史 active 投影准确；描述 UTF-8、name 规范化、完整时间/refinement、int64版本、分页完整性、当前权限均有要求。List保守上界 5,067,008 B 小于 5,242,880 B；64 KiB 单详情/Resolve/历史投影能容纳正式8192-byte描述的最坏JSON逃逸及固定字段。端点成功 cap 不放宽旧 transport/Problem。实现时仍核实际 escaped bytes，不以这次算术代运行。
- useSession 普通 Human ProjectAction 与原 personal/System admin 条件明确分离但共享唯一 Cookie owner；身份/CSRF/epoch、私有原意图、actual-finally、Logout/S2双槽兼容足以在候选范围闭合。Project403不污染 System denied，迟到401不清新身份。
- 原 Update 同 key/body/presence/expected-version、no-op/MaxInt64、三态 lookup、粘性未知、显式原重放、历史 receipt/current GET 分离、稳定 ID/旧名复用和当前改名定位均对齐固定契约；Project 原库 WithoutCancel+3s 没有误套 Model 原 ctx。
- AppShell/SystemNav 现有 meta 分派与 SettingsShell props 足够承接本卡，无须越界改公共组件。双导航、只读生命周期、键盘/焦点、8布局矩阵、取消及 beforeunload 的要求完整；不虚构 create/lifecycle/ModelUI 或运行时已绑定。
- npm脚本、精确新/旧 selectors、45s离线/40s内部、45s Playwright/120s含Cleanup/6m包、输入/实际wait/双清/无pull等均能作为后继执行合同。是否实际完成须实现冻结和运行另证；本轮不运行任何测试或资源。普通/race/实际图、TestMain/init、dynamiccmd/CGO0与工具闭包由后继 freeze 明确，不能预先声称已可执行。
- Audit共享Go闭包必须等待接受交接或root明确固定overlay；SPA发布停止链精确不交集，普通Vite及同源harness不冒生产托管/直链fallback/发布接受；资产窗口需另授且统一custody/恢复。

原稿/全部首轮记录保留。只有自有 scratch 写入；无业务、正式卡、Git或资源修改。当前停写，等待 rev2 固定差量。
