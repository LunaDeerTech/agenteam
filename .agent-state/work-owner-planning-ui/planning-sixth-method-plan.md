# Planning06 读取观察闭集与原文档边界计划

仅有界只读方案，未实施、未运行，尚不足以宣称下一完整候选就绪。技术基线 `39cfa444`，独审记录 `155ae593`，原 Planning05 FAIL 三记录已存 `6207f13d`。本次唯一新文件，不修改六技术源、产品、Go、fixture、dist、driver 或预算。原 Planning03/04/05 FAIL、作者 Recovery13 的有限范围均不回填。

## 原请求与取样时序

原件：`.agent-state/work-owner-planning-ui/planning-fifth-failure.json`；原 evidence `output/ai/work-owner-planning-ui/planning-05-evidence/TestAccountProjectWorkPlanningWebStructureAndTasks` 的 `work-failure-observation.json`、`work-native-consumption-diagnostic.json` 与下列原 response sidecar；原 log `/tmp/wui-op5/ui-bb9e9a2ece144bb0.log`。Project 为 `01a12482-a81f-757f-a6d9-499e834abaa0`，seed Milestone 为 `01a12482-ab40-788d-85b0-a447e23715c6`，seed Sprint 为 `01a12482-ab40-7028-ad7b-b53b9988175f`。

四个原请求均为无 query 的正式详情 GET，sidecar status200。表中时间仅使用 helpers 同一 Node 相对时钟；不把 native/public 各自文档相对时钟混算。

| 原 sidecar / PW 坐标 | 原 URL-kind / XID | 首快照原 failed_at | 原消费观测及缺项 |
| --- | --- | --- | --- |
| response028 / Node18 | `/milestones/{seedMilestone}` / `01a12482-d261-7dcf-833b-b68ecd8f21a3` | 4834.253ms | Doc2 native6/public call3，304B/CL304、EOF/reader/outer尾齐，getMilestone typed fulfilled1、当前identity/最终notbusy；detail_target_present=false，不能称该parent原ID曾在详情DOM可见。原body的公开id/project/version1相符。 |
| response044 / Node32 | `/sprints/{seedSprint}` / `01a12482-d8ee-72a8-aa4c-421280306901` | 6513.171ms | Doc3 native2，435B/CL435与EOF/尾齐，public call_id缺失。原body公开id/project/parent/version1相符，不能补原 Session 返回对象、identity或发布。 |
| response045 / Node33 | `/milestones/{seedMilestone}` / `01a12482-d8ff-7439-b011-b7ac3d9d32b2` | 6528.766ms | Doc3 native3，304B/CL304与EOF/尾齐，public call_id缺失；不能借下次同ID Get补本次。 |
| response078 / diagnostic已停，无Node原row | `/milestones/{seedMilestone}` / `01a12482-ea83-7be7-ae2b-77823bf62e19` | 11013.677ms | helpers仍记同原PW Request/response；native/public已停止，无该次消费证明。只有原sidecar200/公开id/project/version1，不能视作typed API/EOF/尾已证。 |

首 immutable afterEach snapshot 为44,743.533ms、page_closed_at=null，上述四请求 stage=finished、requestfinished/Response.finished返回/observer_rejected均null。原四Promise在44,783.230–44,784.607ms、context关闭后各追加一次stage=finished/reason=operation-rejected；这些是同原请求的晚事件，不能回填首snapshot。seen.verify已经进入Promise.all(tails)；其后的headersComplete=false发生在afterEach关闭ledger之后，不作为最初等待原因，也不证明当时有pending raw headers。最终schema/client/complete及Go持久后验未到。

另外必须保留一个独立阻断：helpers原GET Sprint在7649.848ms开始、7670.551ms failed，未采response/XID；Node35 status0。Doc3另有native5 GET Sprint fetch-rejected/signal_aborted、无headers/EOF，public call1 getSprint rejected1、identity仍当前；但native.bound_original_request=false，没有同PW对象/XID绑定。不能仅凭同URL/时间或后继response047给它认领，也不能以成功消费接缝接受它。原具体取消发起点未采，保持unknown。

## 已定位的真实接线

1. `helpers.ts:1424 workOrdinaryCompletionEvents.request`：planning存在时只登记选中的reorder后立即return，排除了默认原detail GET闭集。`observe:1754` 未selected的四GET遂进入原 `r.finished()`；PW failed本身不使该Promise完成。此次不是header/typed解码失败的证据。
2. 默认R13闭集（已验源码 `903a6622`，当前默认逻辑仍保留）是无query、Project/target UUIDv7 的 GET milestones/sprints/tasks detail，加其独立Lookup/replay闭集。当前只需要其中三个正式GET detail methods，不能顺带开启list、带query、Lookup或任意PATCH。
3. `publication.ts:156` 安装后，原 `getMilestone/getSprint/getTask` wrapper捕获精确两个UUID参数，直接返回Reflect.apply得到的同原Promise；沿原settle标记typed-detail-returned。`useSession.ts:3880 readWork` → `runAuthorized:1433` → actual finally1600之后才resolveVisible；正式API `work-planning.ts:854/966/1089` 分别经过parseMilestone/parseSprint/parseTask，校验Project、target和typed字段。已有native消费谓词仍要求唯一XID/原PW Request/native/public call、EOF/有效CL、reader/release/outer实际尾、Session fulfilled且同当前user/session/epoch、每原doc双observer首explicit/pending0/end和Nodejoin。
4. 默认R13 detail消费谓词 `native.ts:771` 要求原对应get方法/target/typed-detail-returned；**不要求**父级ID在当前详情DOM中出现。028的detail_target_present=false不得放宽或补成true，也不应为复用该默认合同新增一个无事实支撑的parent-DOM判据。产品 `useProjectWorkPlanning.ts:407 selection` 依次读Task→Sprint→Milestone或Sprint→Milestone，`runRead` live后一次publishSelection；原spec仍用current/ready、列表顺序、冲突草稿与采用当前值断言UI结果。父级原Promise消费与最终选中对象UI发布保持区分，不从最终DOM反推某个早GET。
5. `spec.ts:45 go()` 调用page.goto。Doc2/3都在go/ready后才installPublication；`startInitial:1101` 先listMilestones再loadSelection，故Doc3原044/045已发生在包装前。只把Node GET选择compose回来不能补这两次public缺项。
6. 原六槽后仍有 `go(page,path(data,main,task))`（spec约520），因此冲突/current-read实际在**第四document**。当前第三doc六槽后立即finish，自然丢掉后继请求。不能把第三doc的首次退休简单挪到seen.verify前并跨该goto：这会再丢第三doc自己的原end。

## 唯一窄方案（待方法审，未授权实现）

### A. 只组合原已验detail GET闭集

仅原Planning top显式启用：六reorder仍优先走现固定六槽；非reorder只允许同本Project、无query、无body的三种现有详情GET走R13原ordinary选择/消费校验。其它读取、列表、Lookup、冲突PATCH、创建/更新保持原normal finished门。不会因开启ordinaryCompletion顺带继承所有默认可选域。

复用现原Request事件登记与唯一terminal；normal只等待该原requestfinished及一次原Response.finished/null，failed不启动无必达finished，仅进入待证明集合，最终必须通过原typed decoder及同native/public/owner/reader/outer/文档尾联合。六reorder现body/observe共享的已缓存terminal与finished保持；不引入第二消费者竞争等待。R13 raw-header/Lookup/replay ledger、at_request/later_verified、默认路径不改。错类型/Project/target/重复XID/缺call/缺原返回/任一原尾都拒绝。

### B. 保原请求，确定在初始化detail前安装public包装

#### B1. 原四doc的初始化顺序与证据限度

| 原document / 原导航不变 | 源码规定的先后关系 | Planning05已有证据与不能补出的部分 |
| --- | --- | --- |
| Doc1：enter → 登录后回到原Explore | Project读取资格 → 首Milestones list → current_sprint_id为null时empty；首业务创建后才有详情读取 | response008 Project、009 list；原Project公开值current_sprint_id=null。原init无对象，后续创建/排序仍须实际原调用绑定。 |
| Doc2：原page.goto(seedMilestone) | 首list → getMilestone(seed) → publishSelection → 原ready → 现public install | response022 Project、023 list、024 seed详情。028是后续父级详情，不能把024或后来028互作证据；024原返回对象未在包装中观察，不予倒填。 |
| Doc3：原page.goto(seedSprint) | 首list → getSprint(seed) → getMilestone(parent) → publishSelection → 原ready → 现public install | response042 Project、043 list、044/045详情；两原public调用缺失已证。拟屏障只能使未来同一原刺激下的这两次尚未开始的方法从入口受观测。 |
| Doc4：原page.goto(seedTask) | 首list → getTask(seed) → getSprint(parent) → getMilestone(parent) → publishSelection；之后原409/read-current又顺序调用这三个方法 | response068 Project、069 list、070/071/072初始详情与076/077/078后续读取；第三doc已finish，第四doc原native/public缺失。不能把070–072补给076–078。 |

这张表的“→”来自实际源码await，不以sidecar文件号推断异步完成。原008/022/042/068的公开Project JSON均有current_sprint_id且为null，只证原轮；新轮必须核真实值，不写fixture或强制清空currentSprint。原fixture `project_work_planning_web_fixture_test.go:187 seed` 仅创建Milestone、Sprint、两个Task，不执行Sprint start；不会为备选导航新增fixture事实。

#### B2. 首list前置关系的源码证明和必要拒绝边界

`App.vue:165 onMounted`接入现Project/Work controller；Work的`afterNavigation:1143`在真实地址切换后设置initial，再调用`startInitial:1101`。该函数在第一个await前同步把initial=false；新doc的Milestones page初值inactive，于是调用`loadMilestones:512 → loadPage:456 → runRead:388`。runRead在调用Session方法前同步置reading=true；`listMilestones:3903 → readWork:3880 → runAuthorized:1433 → 实际API listMilestones:833`先产生原可见Promise，实际fetch之后才可能进入PW route。因此**route进入时原list公共调用及其原Promise已存在**；此时安装的现详情wrapper完全不捕获list，也不能声称观察过它的入口/返回。

同一稳定初始化代的`await loadMilestones()`没有完成前，startInitial的下半段不执行；`loadSelection:445`、`selection:407`均在其后。selection内部每个get各有独立await和current校验，没有Promise.all或预发父级get。并行入口也有具体边界：重复startInitial因initial=false返回；`blocked:131`含auth.busy/reading，loadSelection先拒绝blocked；runRead再拒绝auth.busy/reading/disposed。原首list尚未消费时，Session owner和reading仍持有，busy变化的sync watcher不能凭此启动另一个正常详情读取。View的重读入口只是`ProjectWorkPlanningView.vue:136/205/225`真实点击事件，当前spec在go/ready完成前无此点击。创建后的refreshAfterReceipt尚未触发，正常新doc也无Project home重命名触发canonicalAddress分支。

这不是对任意身份/路由变化的全局证明。`watch:1186`可在Project读取资格/identity变化时abandonRead、增加generation并清reading；`afterNavigation`也可改代。拟屏障必须锁定该原page/document、完整预期URL和当前真实Session identity/Project上下文；若持有期间发生文档/路由/identity/上下文变化、任何先到的详情Request、第二初始化list或提前新mutation，单调失败，不能换后继list或新document补位。现public安装检查只核authenticated/identity，**不独立证明Project读取资格未变**；未来实现若没有同一现Workspace/Work上下文的实际读取证据，则B仍未ready，不能用该安装返回值代替这项门。禁止为此增加第三observer或给产品加接口。

可供实施审检查的现有只读接缝是同根组件已provide的`project-workspace`：`useProjectWorkspace.ts:140 currentReadContext`提供正式identity、projectID、generation/readGeneration，`publication.ts:218`已有唯一marker/root-instance限定读取。若获授权，B只能复用该接缝，在同一现publication安装中私有保存初始化上下文，并在放行及首详情入口严格核同资格；不能把workspace_marker的存在当成已核上下文。marker目前仅projectRefresh配置要求，planning需显式请求这一只读资格检查而**不启用Project refresh完成豁免/不包装新产品方法**。字段缺失、初始前置不成立、monotonic generation变化都拒绝。此接线仍是方案，尚无实现或控制结果。

#### B3. 拟屏障操作与原预算

不能靠“ready之前试一次”、未界定轮询或新GET补旧。拟只为原四个document的**第一条原** `GET /api/v1/projects/{本Project}/milestones?limit=50`设置安装屏障：在原goto/login刺激之前登记route handler，在本Project首Milestones list进入时同步占位，再严格核method/query/body；首错误也占位并失败。暂缓其原网络派发，在这条已开始的正式list调用期间执行既有installPublication，必须得到明确installed以及原Node调用实际join后，再对**同一route**调用一次continue。URL/header/body完全不改，不生成响应、不读取/替换body、不发另一个GET。原list仍走普通finished；若它以后failed且未通过原正常门，仍整轮失败，不能借屏障纳入新消费例外。

当前native.installPublication只等待boundedJoin并丢弃安装返回enum，不能直接当B的成功证明。未来拟在原方法的planning-only分支返回/严格核本次原安装结果和join，默认路径不变；`publication.ts:156`已有资产加载/singleton/authenticated检查并允许此刻owner正忙，但assets-unobserved、owner-unready、already-installed、expired或其他值均不得冒成功。原资产动态import需同loaded module，不得另发产品读取或用安装前已started detail Promise补写绑定。

所有route callback、原install及continue在启动前登记owned尾，关闭后禁止新准入；原45秒test、现Session30秒owner时钟、afterEach/ctx关闭、driver/outer预算持续计时，**不暂停、不延长、不新设宽限期**。暂缓时间只消耗这些原剩余预算。安装拒绝/超期/取消不能留下悬空route；仅在原context仍可用时对同route正常continue一次并保FAIL，否则通过原context关闭收尾，原route Promise真实settle后才记join。timeout不写joined，close后的迟到安装/continue不升级。具体PW route在关闭中的真实settle尚未执行控制；若无法在原预算内证明所有原尾，本方法拒绝，不进入实际候选。

#### B4. 备选的可行部分与不兼容部分

独立Recovery的现保存源码确实采用Explore→精确empty→install→真实tree选择，父级展开后才显示子节点。这是方法参考，其动态结果/消费例外不外推本case。`ProjectWorkPlanningTree.vue:12`只把已经加载的Milestones放进tree；Sprint/Task未加载时是不可选择placeholder；`expand:1086`分别调用loadSprints/loadTasks，`select:73`才router.push正式详情路径。

若把Doc2改成Explore后点击seed Milestone，可利用原首list里的该节点，原则上不必增加list请求，但改变原deep-link导航刺激，尚未验证同原业务/请求门，当前不采用。Doc3需先额外读取该Milestone的Sprint列表；Doc4需再读取该Sprint的Task列表，均会新增原case没有的初始化请求且改变导航。后面排序框的列表不能拿来追认这些早请求，因此不能在“原导航/无新增GET”的本授权内照搬独立方法。任何此类备选都须另获范围授权并独立核正式请求/业务判据，不与B同时试跑择优。

### C. 按四个真实document退休

保原导航和所有业务刺激。Doc1/2仍在各自两槽/原末排序列表后、原goto前首flush；Doc3封完第5/6槽后继续原“从首页读取阻塞记录”及空列表断言，在原go(seedTask)前首flush；Doc4从初始化前装public，执行原外部冲突、读取当前值、保留草稿、按当前值重新编辑及放弃确认，最后原title==规划任务之后、seen.verify之前首finish。

Node保四份独立original join/end，累计AND，缺任一doc不可由后doc覆盖；六slot仍仅属于前三doc各两槽，Doc4不能有reorder槽。每GET也按唯一Request/XID/document_id+native/public call绑定。封槽不是observer退休；不能用Doc4新响应填Doc3的缺项。最终六槽联合改在全部原业务刺激后评估，原9类/12成功写、唯一key、完整History/Outbox/版本/冲突delta/Activity及所有Go后验不改。

### D. 无headers取消缺项保持硬拒，诊断只补原时点

A–C不授权将上述Node35视作消费成功、声明expectedIncomplete、扩canceled-task-read为Sprint或仅凭fetch-rejected接纳。该缺项使本方案目前不能承诺修后whole候选已齐；若继续出现，仍整体FAIL，不借安装屏障改变时序后“没遇到”证明旧取消原因。

为未来同原动作定位，可沿既有first immutable snapshot增加闭集phase和pending计数：安装/三doc槽/第三doc后继/第四doc冲突/finalverify-tails/schema/decoder几个固定phase、已登记/已settled tails整数、原headers pending/errors计数及是否sealed/closed。同步首snapshot深拷贝且在封闭前采；late event只追加原有有界enum/原Request坐标，不覆盖firstsnapshot，不改变成功判据。没有新的背景轮询或第三observer。若需对无headers原GET建立真实cancel/owner关联，须另有具体正式取消动作/原Promise/同Request绑定方案和授权；当前保持unknown及拒绝。

## 拟验证及范围

仅当方法审和root再授权后，拟仍限原四TS/helpers-native-publication-spec与两既有控制。必要离线控制：Planning组合detail只选原三方法；normal finished一次/failed零finished而必须全尾；四doc不同call_id重置仍严格归属，缺Doc1/2/3 end或Doc4 public、早退休/pending/后继晚settle都拒；原首list安装屏障的错首query/重复/安装失败/ctx关闭/held原操作必须受控settle和actualjoin；原body/GET/六槽默认与R13 ledger/decoder/Recovery AST保持。已验六槽及未变旧矩阵复用，不重跑Project57/R13全矩阵，不增加预算或业务请求。

屏障控制必须使用当前真实Session/API＋Workspace/Work controller组合证明B2先后关系，PW route/传输延迟一侧明确受控而非冒真实browser：首list已调用/原Promise已返回之后才触发屏障，持有期间三detail调用数仍0；安装并放行后严格按各doc选择链开始且每次保同原Promise和typed结果。另核重复initial触发、错误旧上下文、持有中正式资格/路由变化、安装前已started detail、Session原30秒超期与context关闭：这些均不补绑定或升级。原deadline保持同值，hold绝不暂停时钟；原操作受控settle/actualjoin才能结束控制，未settle负例不得声称收尾完成。现有native/PW零detail计数只能作为先到请求的拒绝证据，不能单独证明未派发的公共方法从未开始；该部分必须由真实controller组合控制和上述正式源码入口顺序共同支撑。

原Node35无response/XID取消不能作为正控，可新增明确拒绝控及首phase诊断控。没有该缺项的真实原取消方法证据前，不把新方法控或A–C的接受表述为整Planning可通过。技术冻结后仍需actualdiff独审、root checkpoint与另授fresh原完整一次；无自动重试。
