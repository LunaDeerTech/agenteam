# Model UI 正式 main 集成检查点

- 树：`/workspace/agenteam-model-ui-delivery`；分支 `ai/model-ui-delivery`；正式基线 `3cea6076bb01693ead2755826826d189626aa3aa`。root 已从原冻结组合精确带入五测试路径、67 个必要恢复文件及卡；不得整树覆盖 main。
- 负责人 `/root/model` 拥有这些限定源、本文及 frontend README；Git／最终交付由 root 执行，global tasks 不在本写域。当前无 PG／browser／socket grant。
- 原组合已接受：authority19 52868 完整 PASS（Go39.54s／outer128.596s、13checks、31attempt／29EOF=typed=schema／2预期不完整、实际Wait／七资源与目录／TCP双清／inputs同一）；现新6/6、旧14/14、独立A/B按版本组合通过。前18轮 FAIL 保留于卡。该结果属于旧 delivery11c／迁移≤22／旧冻结资产，新 main 含后续迁移和 Variables／Work 装配，尚未完成本组合验收。
- 原树 `/workspace/agenteam`、旧 `/workspace/agenteam-delivery`、fixed54e及0906／53f备份和历史证据保持原位冻结，不将旧二进制作为本树新结果。

## 新树实施

- root 已确认保留 main 五个 Variables 契约／Audit 路径；Model25web与共享 UiDialog／UiPopover／useLayer、锁文件及Model schemas全与 main 相同，详见 `.agent-state/model-ui-recovery/integration-brief.md`。
- 四 driver 将 `DELIVERY` 从旧绝对目录改为其实际 `ROOT`：recovery/run-owned-top.py、recovery/fixture-go.py、regression/run-owned-regression.py、independent/run-independent.py。其余源码逐字96934da4，selector／预算／资源／Wait／EOF／finished／身份门槛不变。
- 新 `.agent-state/model-ui-recovery/delivery-path-controls.py` 实际c22872 exit0：四实际源绑定正控、20个旧树／其它树／重复赋值／额外改动／父目录负控；仅AST和git show，不import driver或启动资源。五源已冻结交Runner窄审。
- 离线依赖／缓存、针对性Audit/menu单元、TS／资产构建、account race／精确发现和路径窄独审现均已完成，实际结果见下节；下一申请新main authority和受VariablesAudit变化影响的必要ProjectAudit真实子集。不机械重复旧14。

## 构建与磁盘准备（已完成离线准备）

- 固定 Go `/workspace/toolchains/go1.27.1/bin/go`；显式 `GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOFLAGS='-mod=readonly -p=2' GOMAXPROCS=2`。
- 可复用原 Model 独占 GOCACHE `/workspace/agenteam/output/ai/model-ui-recovery/go-build`（1.3GiB），只读共享 GOMODCACHE 同目录 `go-mod`（197MiB）；本线不并行两次可写cache编译。root已明确授权本线独占复用，避免另建空缓存复制1.3GiB。
- 两套锁一致的 node_modules 已以hardlink共享不可写包文件，明确排除.vite/.vite-temp/.tmp/.cache/.vitest/coverage/.nyc_output；Vite／Vitest新缓存目录为本树独有。未安装／下载／执行postinstall或chmod共享inode。MinIO仅只读link原已验证固定产物。
- 初查available5,935,796,224B，距5GiB约567MB；新account／四helper／web及native输出预计105–115MB，增量Go cache/tmp另估150–400MB但不是硬上界。Vars07编译实际退出后，经root协调才启动本树唯一Go增量构建。
- 真实启动仍必须fresh同进程statvfs≥5GiB与root独占授权；原磁盘观察不是后续门槛证明。构建成功也不等于main真实通过。

## 本次离线实际结果与冻结

- Runner路径窄审d496e4/7b7bd3实际0，无must-fix：四ROOT实解析本新树、12覆写负控；原selector/预算/resources/gate/argv逐字不变。未执行driver main。
- Audit/client/metadata/menu三个实际web spec，74234 actual0，148项；Vue应用类型3729 actual0。两browser spec首TS28518 actual2为命令ES2023缺现有String.isWellFormed声明；仅改lib ES2024后62553 actual0，无源改动。限定Prettier94161 actual0；两Go gofmt无输出。
- 新树 account race构建36615 actualWait0；072e6c精确六新top，6372cb精确两ProjectAudit旧top，均actual0。文件 `output/ai/model-ui-recovery/account-delivery.test` 为58,286,985B，SHA256 `d8c1a058e73d06c95cd121c023f19d435997851d2d19ca229ba8eef38f4a7af3`。它由main3cea+本fixture实际构建，不是旧54e2复制。
- 四fixture helper98176顺序构建、各actualWait0；Vite63221 actual0，286modules/64assets，写本树私有dist；native b386c1 actual0，7modules/89.09kB。原web/dist未写。
- d7e52b实际新资产的公开singleton+AccountFailure精确导出解析通过；只提取原driver输入表达式预飞26source/6binary/5formal/64asset，路径均新树实际文件。Python schema依赖可用，无main/网络/资源执行。
- 构建结束可用5,550,333,952B，私有Go tmp空；此非未来真实启动门槛。全部构建/检查actualWait已终态，没有活Go/Node检查。
- 上述76范围已由root保存为f013f484；Runner最终91f83d只读核26source/6binary/5formal/64assets及真实ELF/JS闭包，无新增must-fix。其接受仅限就绪。

## 新main首次authority实际FAIL（已完整释放）

- root fresh独占授权后，session49546于2026-10-09T19:52:18.165644Z同进程statvfs确认5,966,323,712B≥5GiB，直接exec本树原driver；固定d8c1a058与本树实际assets/helpers、原预算和gate不变。
- Go57.95s／outer147.133s，工具实际terminal exit1。失败停在pending生命周期拒绝Resolve的原finished054；仅13safe responses，原055–059、credential153、reference178及其后均未到。旧组合authority19不冒本组合通过。
- 同请求409，native EOF262=CL，2read、2cancel均settled且release成功；PW在动作后约47.9ms aborted，原finished直到pageclose约36.46s后才拒。实际typed Problem、Request-ID相等、当前身份及loading→error DOM已采，但problem_instance_matches=false、selected_bound=false，slot end／hooks退休未观察，不接受新Resolve gate。held008原Session联合gate通过271.657ms且PW正常finished，非异常路径正例。
- direct1058181／Node、四adopted实际Wait（均0），watchdog／resource observer／root／proxy handlers／prepService join；七ID双absent、desc双空、runtime空/private removed、TCP双空、inputs同一全部齐。窗口已释放。最小必要记录在`.agent-state/model-ui-recovery/main-authority-first-failure.json`，原件留原output目录。
- 只读核实后再决定有意义增量；不改finished门槛，不盲重跑，尚未运行两ProjectAudit真实top。当前无新真实资源grant。


## 后继限定诊断与未接受方法提案

- root已在78a94f33保存上述三个FAIL记录。实际typed constructor成功；instance false来自observer额外endpoint literal与正式Account安全`/api/v1`不一致，不能据此推产品错误。slot/end缺失另因denied只在原finished拒绝后的finally收尾；实际pageclose先于该拒绝，142已join samples不证明browser owner/final hooks退役。
- 四诊断技术路径只修producer实例契约及控制。真实boundary＋Request-ID middleware Recorder→正式common.json→实际client/Session/Workspace/View/PW转换observer：旧61963红actual1，修后7462绿actual0（20＋7／unhandled0）；strictTS17807与原adapter9/source identity89705 actual0。Runner69969／6f595c独审通过，原054–059／预算／gate、生产源、account/helper/dist不变。这里只接受诊断窄修。
- 另新增`resolve-rejection-owner-controls.cjs`与`resolve-rejection-method-proposal.md`供方法提案，不在Runner四源接受内。actual Session project-read四格51209 actual0：typed404/409都是cancel尾实际settle→公开busy=false→原Promise rejected；abandon／受控expiry先cancelled拒绝且busy仍true，尾释放后才busy=false。生产函数未替换，transport cancel Promise与expiry时钟明确为double，无资源／0unhandled。
- 方法草案只覆盖六个现有预声明denied调用的409 PROJECT_NOT_ACTIVE／404 NOT_FOUND；403及其它请求保原finished路径。所有typed/current identity/DOM/layout/严格EOF/cancel/owner与有界end/退休条件列为必需，原055–059与完整root尾保留。尚未独审或实现，禁止据此改变原失败或重跑。冻结本次4诊断源、2提案源、本文与卡共8路径供root保存；尚无真实资源grant。

## Resolve 六声明方法恢复中的 WIP（尚未独审／真实验收）

- root 已接受 Runner 对方法草案的限定独审，并授权只实现六个原 denied tuple；另明确允许既有 Model spec 的 authority-only `beforePublish` 接缝，在原 verify/check/count/dispose 后实际 page.close 与登记的原 PW Promise join，成功后才发布 completed。其它 modes、产品／Go／共享 PG 脚手架不扩范围。
- 环境恢复后实际 HEAD4b411392，10 个技术 WIP 已存在：authority/native/observer/boundary-controls/旧 adapter/spec 六修改，新增 rejection-contract、rejection-controls、rejection-adapter-controls、before-publish-controls 四源。恢复以这些实际字节为准，未从头覆盖。首版已含正式 schema 驱动的 Problem 验证、拒绝瞬间与最终身份/role/busy、原5s header deadline、退休后 native/owner 计数、原 PW Promise 登记与发布接缝；原055–059仍保留。
- 忽略目录现有 rejection-controls/result.json 为19项/unhandled0，但恢复时尚未核齐每条实际工具终态，不据文件反推本轮命令成功。先前提到的35073在恢复后 write_stdin 返回 Unknown process id，其 actual terminal 缺失，保留未确认；当前可读进程未见本线这些检查仍活，不将不存在当PASS。旧明确确认的51209／Runner7519/87336/59271/51dfc2等方法边界继续保留。
- 本次先冻结以上10源码与本文共11路径供root安全 WIP checkpoint。尚须完成首版源码复核、必要控制／TS／native bundle输入对应及 Runner 实现独审。没有真实资源授权；49546 FAIL与新main整体未完成不变。

## 首版离线终态与 PW 异常边界修正（待独审）

- root已保存首版11源为e4f6cd31。恢复后78258→4aadb2实际0（adapter16／beforePublish6）、82441→ac447a严格TS0；82172→038b06真实Boundary两精确code经正式schema及实际产品链20+7+19／unhandled0。共享Session改动影响经62629→234461实际25+113控制0验证；原Session特殊方法不扩。
- 9bb7ed实际沿原build配置write:false编译7modules，生成code逐字等于当前private native bundle；account d8c1a058／helper／应用dist不改。先前自有data-URL导入探针6de162因模块URL写法失败，未启动build；修探针后通过，不是产品缺陷。旧35073终态缺口保留。
- 自读168f53实际确认两个负例：candidate已继续后原PW提前拒绝／returnedError会只记joined。root授权收紧；contract只允许正常null或实际close后观察到的拒绝，先让当轮已排队settlement被观察，不能借close标记。82862→5d9840实际0（adapter19，含两原红及queued-rejection；publish6），均无unhandled。新19产品控制的输出标签改为method implemented／real authority not run，不再沿旧diagnostic标签误示当前gate未改。
- 本次仅三技术增量（rejection-contract／rejection-adapter-controls／rejection-controls）加方法文／卡／本文六路径冻结；其余e4f6cd31保持。Runner正作实现独审，结果未得；没有PG／browser／socket／网络授权，新main仍未完成。

- c58e5652之后作者c32d1e与Runner独立fda84b均确认另一方法回归：candidate安装不可用／5s超时会抢先拒绝原normal Promise，不能保原45s内正常finished路径。root授权的一行修复只把candidate失败交回同一已登记normal Promise；不新调用finished／请求／预算，正常拒绝仍FAIL，candidate一旦ready后原end／owner门槛不绕过。10694→d21416实际0，adapter23／unhandled0，新增直接提取实际normal+completion初值的四控（unavailable、timeout、原normal拒绝、403不启candidate）；其余控制复用。当前authority＋adapter＋本文三路径冻结待保存和Runner复核。
- Audit readiness2278f1只读确认两个旧exact top／正式Go函数／原env与继承PATH、四非法selector拒绝；现d8c binary与dist不重编／不重跑发现。两Audit只共享D27卡、binary/helpers/dist/owned_resources及其本域regression脚手架／Audit spec/client/schema，不读取Resolve case或nativeprobe。MinIO symlink被原frozen_inputs普通文件门槛拒绝的风险已报root，root负责转为同固定产物普通hardlink；未获实际窗口。

- Runner对稳定81b606a9整体方法实现的限定独审已完成，无remaining must-fix：d0c1e7 actual0＝23 adapter＋6 beforePublish；9b69c7 actual0＝19真实controller/client/workspace/View/native与锁定PW转换（layout／transport为明确替身），均unhandled0；0f81f6独立四控actual0覆盖六原Promise之一未settle、close后returnedError、安全投影、candidate失败后原拒绝传播及candidate继续后早拒绝不能吞。原fda84b红→dfbadf绿保留，独审未改作者源。只接受离线实现就绪，真实新main authority／原054未通过，35073及49546缺口不回填。D27卡、源码、产物继续freeze，本文唯一记录增量待root安全保存。
