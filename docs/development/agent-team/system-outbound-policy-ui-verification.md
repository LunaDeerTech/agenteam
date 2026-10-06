# System 出站规则管理 UI 验证记录

## 1. 接受结果与固定输入

主线程已接受34路径并提交推送 `1ff044264a54c948e512db9aafad24ec5e0aa3c2`，核远端一致。此记录接受[工作卡rev1](../work-items/d27-system-outbound-policy-ui.md)的完整页面结果，技术§1–7 SHA256 `343f09b6facd95c000df2f2b35bceeb73f3a7e93d2974ce18304703266d0ab6b` 保持；不表示完整D27或平台完成。[原规格报告](system-outbound-policy-ui-spec-verification.md)保留唯一STATIC finding及其关闭，不把规格阻断改称产品RED。

实际业务基线是 `213cf5c3f552e6b05b541ce02afc1dd65ce9db93`，包含已接受出站HTTP `a942779`。作者input05 SHA `c0318ca2ece3efb5e7e8bcedf2c30d96f0845b6466f0ee93fed2c174f061ea7a` 固定33源、38dist、1014 Git依赖；第34[前端README](../frontend/README.md)最后单独完成。final34清单 SHA `2e4f2ea47c4e7ddcc7aa6ec9f656bff5d8e8ca5f6c984e8ac12254b99ef484bd`、README SHA `bb2988144e87d10ea24c3245ad967596780f38b6db5d10167c0d851f5c5d8d1f`。README前态 `3e068776925891fc28ed42ec46587331e60f0dc1a2aaca279ac9d5d97b567262` 来自 `2cefd225` 档案修订，不能说仍是213cf5c产品README。末件未重跑业务；34个Git blob与最终清单匹配，提交不是被倒填成当时动态受测HEAD。

页面通过两个固定GET/PUT端口，提供规范CIDR／显式全部端口或受限端口列表、HTTP选择、完整保存及明确的冲突处理。当前GET、编辑基线、草稿与历史receipt分开；没有lookup、自动重试、目标试连或Reload。未确认写保留原key/body/expected_version，显式确认要求同完整identity及仍合法的原CSRF。第十状态域复用唯一Cookie owner，逻辑截止／取消不能提前释放真实尾部。该结果不改变PolicyService与网络分类规则。

## 2. 分阶段证据与原失败

| 阶段 | 已执行结果及复用 | 保留边界 |
| --- | --- | --- |
| API／规则语法 | 作者首轮176 PASS＋1前提失败，受影响1例修后PASS；独立两组actual0／0.471s。正式Go oracle消费95个固定Git源、53向量，GET样本414763B | 旧Model Credential mock缺target／错误200是测试前提；oracle不是真实HTTP或新目标连接 |
| owner | 作者99 PASS，补充轮3 PASS＋1前提失败，再复验受影响例；独立owner01缺Request-ID首错，owner02两组actual0／0.712s | 原owner-pure02把SMTP202设为200，finally外前置断言使私有cancel Promise未显式join；process actual1不补成actualjoin。后轮finally释放与等待单列。30s是虚拟时间配合实际read/cancel门闩 |
| 页面／App | page-state01 actual1／2.222s，strict receipt后GET503过早清草稿是真正产品RED；原同字节选中spec经controller修复，page-state02三例PASS／2.227s。page-check01 actual0／34.097s，42文件1517测试、format/type/build通过 | 实际两路径变化：controller修复逻辑及已授权View两处文字；不说整轮只改controller。独立app01 A PASS／B fixture前提FAIL，app02仅B PASS、A skip；原失败缺DOM／请求序列不被后轮日志回填 |
| harness／执行准备 | 两个静态前提修复后，受影响编译／vet／list／type／gate通过；独立14个离线命令按原记录保留 | CauseRef语义摘要与receipt digest不同；管理路径族断言须覆盖。广域compile原actual-15不是PASS。独立closure01误写objects路径、type01漏DOM.Iterable原失败分别保留 |

阶段独立原报告：[API](system-outbound-policy-ui-verification-evidence/objects/b40b6486f6eed6bd0faf6b2b19097c6d9cd8f2aa182ce1c9a3f18aaec0bed28a)、[owner](system-outbound-policy-ui-verification-evidence/objects/20acf2532ab100f903edac9f1dde5d2b93711f6e6feca3822b0eff165a4d5cb2)、[页面](system-outbound-policy-ui-verification-evidence/objects/3d9372203a7fc5a322a7e4479499c195bac0d7a8da553287e791b89f026b68a0)。作者[汇总](system-outbound-policy-ui-verification-evidence/objects/01e1d555e585032c40419be22cc59e0452a4f6eaaf99027d58336ca624f339c4)保留其冻结时“独立与README待定”原时态，后续终局由本报告及末件补充，不改旧原文。

## 3. 真实轮、分版本复用与清理

以下是原command实际wait记录；资源列为exact IDs／所属PID-starttime／adopted实际wait。四个新场景和三个旧场景按输入与未变语义组合，不称input05整套重跑。

| 原轮 | 输入／业务事实 | child退出／秒；外层 | IDs／PID／wait |
| --- | --- | --- | --- |
| read01 | input02，Read PASS：browser11.8／top14.68s | 0／84.641；外层1 | 7／100／4 |
| mutation01 | input03，原事务状态前提FAIL | 1／57.826；1 | 7／85／4 |
| mutation02 | input04，Mutation PASS：5.4／9.98s | 0／57.422；0 | 7／86／4 |
| authority01 | input04，原取消读取标题前提FAIL | 1／60.426；1 | 7／85／4 |
| authority02 | input05，Authority PASS：9.0／13.89s | 0／60.328；0 | 7／87／4 |
| navigation01 | input05，Navigation PASS：18.5／21.55s | 0／69.084；0 | 7／86／4 |
| old-delivery-navigation01 | input05，TempDir ENOSPC，browser/node均0 | 1／50.441；1 | 9／82／0 |
| old-delivery-navigation02 | input05，旧投递Navigation PASS：14.3／20.44s | 0／69.941；0 | 9／90／4 |
| old-settings-outcome01 | input05，旧SMTP配置Outcome PASS：6.4／12.33s | 0／62.514；0 | 7／83／4 |
| old-delivery-outcome01 | input05，旧投递Outcome PASS：7.7／14.99s | 0／68.681；0 | 9／89／4 |
| independent01 | input05／execution02：A完整PASS8.57s，B登录返回前提FAIL15.49s | 1／125.159；1 | 7／153／8 |
| independent02 | input05／execution03：仅B完整PASS10.38s，browser4.5s | 0／58.171；0 | 7／84／4 |

read01的两扫exact ID／PID／runtime均空，但baseline第一扫false、第二扫true，原外层退出1不改写。原第一扫current完整快照未保存，原因未证。只读followup01 actual1／1.155342s实拍后来既有MinIO完整Mounts数组顺序变化，不能倒填原因；driver02只增加完整mount对象多重集合排序及每扫raw/current/canonical差量保存。followup02 actual0／1.199625s两扫确认当前基线与原资源／PID／runtime，接受的是业务PASS加当前清理恢复证据，read未重跑，也不称原driver绿。

mutation修订仅新browser的两事务409精确`not_committed`谓词及有界安全摘要；原第一actual commit_state没有采样，第二`IDEMPOTENCY_KEY_REUSED`分支未到达。authority修订仅显式已确认／普通标题前提及调用点、水位计数和安全布尔诊断；原实际替代标题／DOM未记录。input03→04→05的22 web源、38dist、fixture与driver02均未变，不削取消尾部／零额外GET／显式刷新等强断言。

旧Nav首次ENOSPC发生前fixture已启动，不能写成“没有资源”；原9IDs／82PID实际双清。root随后只清两个已结束任务的可再生GOCACHE，源码／证据／modcache及活动缓存不动；before/result与三次`df>=5GiB`门禁原件保留，不把可用磁盘快照说成连续保障。除read的原baseline例外外，各已完成轮都有原baseline不变、输入前后同字节、exact ID双absent、所属PID/starttime及runtime空、monitor0；独立两轮另有browser runtime空。

早期广域offline compile留下作者PID89884／start540243／PPID1 Z，本任务无法reap。该进程已停止但原wait缺口仍存在，后续每轮清理不覆盖它；新版离线runner对另一个自有子进程的实际adopted wait不能修复旧缺口。此记录不称全局无残留。

## 4. 最终独立接受的边界

[最终独立报告](system-outbound-policy-ui-verification-evidence/objects/c78f2b79186616d8c743541403cb38a2c61b8093b378acf14f9d80d76ff01fea) SHA `c78f2b79186616d8c743541403cb38a2c61b8093b378acf14f9d80d76ff01fea` 接受A01＋B02组合；[33源原清单](system-outbound-policy-ui-verification-evidence/objects/e942cc442ff530c32ee554c44877b00cfecb3308abbf903915edeeae85cb4c7f)随后与第34README和Git绑定。原independent01整个命令仍exit1，不把两个顶层描述为新轮同跑。

A实际到达7项：accepted-cut后另一正式管理员推进current；Session／GET不代receipt；显式同原key/body/合法原CSRF重放历史receipt；当前规则不倒退；原command／semantic digest与Audit cause唯一；确认后GET503保留已提交草稿，只重读不多写。B实际到达8项：待决确认中pageshow／一次Session503／同Session新序号200 EOF恢复及原生focus/Tab；当前Session撤销清草稿和旧pending；重新登录得到新Session与首页，新主动导航前users0/PUT0；主动经过users后进入Outbound，users GET严格晚于水位，Outbound当前GET完整EOF；空草稿、无原重试及clean logout。

原B只实测重新登录后首页，此前宽login正则没有独立记录精确query；固定App/Login/router解释与动态观察分列。repair02只改B导航前提和分阶段零users断言，A/helper/Go/fixture/产品未改，无goto/reload。users原观察器只有method/path/status，不能称其EOF或owner join；actual owner尾部仍复用阶段受控探针，不由浏览器观察替代。其他包`[no tests to run]`不计业务组。

## 5. 图像、归档与未覆盖范围

新页面八图及旧投递页八图均由作者逐张view；主线程只看新light390、dark1440两图，没有旧八图或全部新图的root审阅声明。原PNG／SHA／visual-review保存。均为900px高视口，不能声称下方表单／错误区域已图像覆盖；真实几何、焦点、Tab断言单列。未新增截图或图审。

[index.json](system-outbound-policy-ui-verification-evidence/index.json)保存946逻辑原件、622新对象／42360348字节、117条固定Git或已永久对象引用；34逻辑源路径的70个必要版本可定位，未发现新增受测源码缺件。1014依赖、oracle95源及阶段差量用固定Git，38dist仅指纹；原raw／失败／diff不格式化，不复制依赖树、缓存、runtime凭据、可执行文件或dist。原命令env只是选定overrides，不虚构完整继承环境。离线校验只核原字节、Git、源绑定、原退出／双清／复用及文档链接，不重跑产品：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-outbound-policy-ui-verification-evidence/verify_archive.py --repo .
```

真实fixture沿锁定工具、生产dist与原45s browser／2m top／6m package／race/count1／worker1/retry0预算；没有新外部连接、目标可达／镜像健康、Runtime／Provider／MCP／Project绑定或外部邮箱接受。合成pageshow不等于原生BFCache，测试托管不等于生产SPA或真实Vite代理；原生缩放未验。完整D08–D28/E01未完成、E01未开始，Summary待决、未绑定能力／ready503保持。

同期Audit仅保留已知调度事实：已有query／HTTP有界阶段及作者native分轮证据，整卡尚未接受，本档不读取活动事务／producer候选。SPA既有scope/native通过不等于SPA接受；concurrent-publication探针由平台内容安全机制停止、无run目录，竞态未闭合、脚本返修／发布暂停，不重试／改写／转派。Object/tools原停止不变。此次只归位已接受Outbound页面及其行政入口，不扩大其它卡或资源授权。
