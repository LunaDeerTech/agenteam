from pathlib import Path
import json,hashlib
V=Path('/workspace/scratch/project-initialization-convergence-verification');A=Path('/workspace/scratch/project-initialization-convergence-author');R=Path(__file__).parent;H=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();J=lambda p:json.loads(p.read_bytes());ref=lambda p:{'path':str(p),'sha256':H(p)}
manifest=A/'candidate03/manifest.json';assert H(manifest)=='0c7ab9129a9757cd7f2def5bfe3375e176910b935e047bc532f328ecf8f1886b';m=J(manifest)
for x in m['files']:assert H(Path(x['snapshot']))==x['sha256']
rr=V/'pg-runtime01/runtime-result01.json';assert H(rr)=='160f0a5f9f3433bbdd709066018da33f0f7ec785dd9eaeb5831964b0c02a767e';run=J(rr)
assert run['status']=='PASS_INDEPENDENT_TWO_PG_ROUNDS'
for rnd in run['independent_rounds']:
 for x in rnd['references']:assert H(Path(x['path']))==x['sha256']
for p,sha in [(A/'author-results01/result.json','04d9b34d1637d342627cdd3e7ad45d74b3a1c4648e2afbdcfee2248b6556ab0a'),(A/'author-results01/resource-daemon.json','71db926923ed91800e446839296eb877d71d8492325972f805330f9c0137b9b4')]:assert H(p)==sha
source={'baseline':m['baseline'],'spec_commit':m['spec_commit'],'formal_spec_original_sha256':m['spec_sha256'],'effective_candidate':ref(manifest),'paths':m['files'],'repo_installed':False,'all_final_snapshot_hashes_match':True,'production_sources_unchanged_since_unit01':True}
(R/'sources.json').write_text(json.dumps(source,indent=2)+'\n')
(R/'resource-daemon.json').write_text(json.dumps(run['all_six_resource_daemon'],indent=2)+'\n')
refs=[A/'offline-summary01.json',A/'integration-offline-summary01.json',A/'author-results01/result.json',A/'author-results01/review.md',A/'author-results01/resource-daemon.json',V/'unit03-static/result.json',V/'unit04-offline-review/result.json',V/'candidate01-static/result.json',V/'candidate02-static/result.json',V/'integration-offline-review01/result.json',V/'pg-driver02-static/result.json',V/'pg-offline01/final-freeze01.json',V/'pg-offline01/summary01.json',V/'pg-runtime01/freeze.json',V/'pg-runtime01/closure.json',V/'pg-runtime01/source-gate-result01.json',V/'pg-runtime01/execution-freeze.json',rr,V/'author-pg-review01/completed03-result.json',V/'author-pg-review01/freeze.json']
review='''五技术源的完整限定独立验收 PASS，固定 candidate03；资源窗口已归还 root，当前无活动独立 driver 或所属资源。此结论接受 scratch 候选及其 overlay 执行结果；五源尚未安装仓库，README 第六末件未在本次接受范围内。

生产两源自 unit01 未改变。optional InitializationConvergenceAuthority 在真实同 Store / 同 Tx / caller 已持本 Project EX 的约束内核正式 registeredService actor 和原 request 三字段，匹配原 Creation、Project、owner 与双向关联，检查 accepted / initializing / failed / completed 的字段 NULL、初始化及历史 snapshot 一致性。当前 active-only 与旧成功 gate 的语义分工按已接受正式卡执行。未新增事务、锁获取、写入发布、Skills 通过信号或旧接口改写。当前 completed 合法改名/version 不被误等同于历史初始 snapshot。

验收版本组合：unit01 两个测试 helper 静态缺陷经 unit03 修正，unit04 仅测试格式；candidate01 的 fixture bounded Force 返回不等于终局问题经 candidate02 增加实际 Background Drain 关闭，candidate03 仅一空格格式。production 2 原字节，旧实现不改。完整五源 STATIC、作者普通/race Project 各 54 top / 181 子例（新增 2 / 70）与 contract 各 29 / 15、相关 compile/vet、真实 full20/dynamic 图、两个 cmd build 均已核固定原件。私有 probe 的五项必要离线 actual0/输入一致/实际 wait 双清；不重复父纯组或全图。

真实资源组合共六轮：作者新 Facts 1/18 与 TransactionBoundary 1/9，旧 mapping/plan/SkillFacts 1/6 与完整锁计划/poison 1/0；独立 A 1/3、B 1/4，合计 6 top / 40 子例。每轮精确 selector/full20 原 fixture，120s top 含 Cleanup / 包6m、actual0/direct wait、watchdog join、无 forced tail、7 精确资源和 owned/runtime 双清、输入相同。独立 A fixture49.616s/top1.04s、B51.868s/top1.00s；A 全终局后才启动 B。

独立 A 真正区分 live foreign Store Tx、SH 不满足 EX，以及原 caller 未取消时只取消第二次 gate 调用；均要求原事务忽略 gate 错误也仍被 poison，caller 写标记实际回滚。B 以四状态可提交的本域坏事实做拒绝/零观察变化/修回通过，另核 completed 历史初始值与当前合法改名、version9。复用作者新真实 PG helper、migrator、同 Store Account 构造依赖及 registeredService，私有调用顺序和断言独立；不是第二套独立 fixture 实现。快照证明范围为两本域行和所列副作用计数，不称全库所有字段不变。closed-store 作者代表还带过期 token，不独立证明各错误优先级。旧两个 selector 仍使用原测试身份/Skills adapter，不称生产 Skills 或 Account/Human 创建 E2E。

资源/daemon 以实际集合计算：六轮 42 个不同资源 ID，逐轮双扫精确不存在；PID1 baseline449→473，24 个不同 containerd-shim Z，轮间没有额外增加/删除。它们非 owned、未 wait，不称全机清零；独立两轮分别465→469→473。host TCP 是补充元数据轮询，不能证明完整短连接或 tuple 所有权，不能授权非 owned signal；各轮真实退役两扫空，75s 仅尾部观察，不延业务预算。原 helper/component 内部 join 限制不被外层进程 wait 冒领。

原件全部保留：两个 unit STATIC harness 缺陷、两次 gofmt diff 非零、旧指纹/隐式 CGO 实际图额外文件补冻、IC-PG-01、v01 TCP deadline 静态问题；作者两次汇总器和本独审两个静态检查脚本的 bool/list 形状误假设亦保留。这些未冒称真实 PG 首红；六个真实轮均首次 PASS。driver v02 只修 deadline，独立 runtime 只改 selector/overlay，所有资源和收尾函数 AST 复用固定接受版本。

本结果不绑定 production Skills、Project 创建 HTTP、root 初始化、Resolution/Invocations 或 D24；不把完整 D08/D10 宣称完成。Object runtime join、OpenAI tools 独审、SPA 发布三个停止保持，未触探针。管理员统一 Meeting Summary 已决不重问。仓库安装、README 末件及 Git 交付仍由 root 协调；本轮无仓库或 Git 写入。
'''
(R/'review.md').write_text(review)
result={'status':'PASS_COMPLETE_FIVE_SCRATCH_TECHNICAL_SOURCES','sources':ref(R/'sources.json'),'review':ref(R/'review.md'),'resource_daemon':ref(R/'resource-daemon.json'),'support':[ref(p) for p in refs],'blocking':[],'new_product_runtime_tops':2,'author_old_regression_tops':2,'independent_tops':2,'total_real_top_count':6,'total_real_nested_count':40,'total_real_round_count':6,'total_resource_ids':42,'task_daemon_nonowned_unwaited':24,'no_repository_installation':True,'README_pending':True,'resource_window_released':True,'no_active_independent_commands_or_resources':True,'test_failures_in_real_rounds':False,'limits':['Scratch overlay acceptance only; repo installation pending.','New fixture proves Project facts with true Store/service, not Skills/Human create E2E.','Snapshot only covered rows/counts, not all database fields.','Old compatibility adapters retain original test boundary.','Host TCP is supplementary, not complete trace.','PID1 daemon delta is nonowned/unwaited, no global-zero claim.']}
(R/'result.json').write_text(json.dumps(result,indent=2)+'\n')
freeze={'status':'FROZEN_COMPLETE_FIVE_TECHNICAL_ACCEPTANCE','files':{n:H(R/n) for n in ['freeze-final.py','sources.json','resource-daemon.json','review.md','result.json']},'no_more_writes_to_this_final':True}
(R/'freeze.json').write_text(json.dumps(freeze,indent=2)+'\n');print(json.dumps({n:H(R/n) for n in ['sources.json','resource-daemon.json','review.md','result.json','freeze.json']},indent=2))
