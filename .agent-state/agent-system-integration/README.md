# Task / Scheduler 有限交付候选

本有限范围验收与集成已完成；实际main提交及远端确认由协调索引记录。候选从main `18a27db5` 构建，选择AgentSystem `13138dbd` 的255个技术路径、7个兼容路径与B32精确增量（`3cc9f1bd`），去重后连同current/本说明共282路径；最终实际验收source为 `70d383c5`。main已有Human Skill安装、Lookup、分页目录与读取入口保留；00032–00036不重取旧分支版本，新增连续迁移为00037–00047。

Work 的真实应用装配依赖 Agent 当前事实、Execution occupancy 和 Scheduler pending；真实 Agent 创建组合进一步使用 Model/Secret refs、Mount、Skills 初始化及真实 Runtime InstallSource/Builtin/Registry。对应有限库实现和测试均在选择范围中。Go 依赖保持 `jsonschema/v6 v6.0.3` 与实际 MVS 的 `regexp2 v1.12.0`；不是只复制 HTTP 层或以空 provider 补齐构造。

## 已有接受与限制

下表除最后一行外均复用各源分支原输入的真实结果，不冒合并后重新执行；最后一行在最终delivery运行。B整源 `542574b5` 已有限静审，精确26top race及五包vet wholePASS；候选main兼容4个app top race通过，app-pure01容量wholeFAIL/0vet保留，独立app-vet02后继wholePASS。必要旧证据复用，不要求重新运行旧全量矩阵。

| 精确真实入口 | 已有有限结果 |
| --- | --- |
| `^TestAgentConfigurationSchema$` | 1 top / 3 sub wholePASS；32–35 升级、旧事实与 metadata，相关迁移已在 main。 |
| `^TestAgentRuntimeSchema$` | 1 / 4 wholePASS；37–39 迁移、事务约束、Human 安装兼容，SQL 回滚探针不是 ToolCall 执行。 |
| `^TestExecutionPreparation$` | 1 / 4 wholePASS；40 升级、claim 约束、真实 Project gate 与 Owner Task 读取，不是完整 capture。 |
| `^TestAgentConfigurationCreate$` | 1 / 2 wholePASS；真实 Source/Reconcile、默认两项 true、refs/receipt 重放及最终事务整体回滚。 |
| `^TestTaskTransitionHuman$` | 1 / 2 wholePASS；40→43/repeat、Owner 配置、真实指派、重放与整体回滚。 |
| `^TestSchedulerClaim$` | 1 / 2 wholePASS；正式 Start、Work claim 与 durable pending 同事务、重放和回滚。 |
| `^TestSchedulerLaunch$` | 1 / 2 业务通过，原 HOST_TCP delta=1 导致 wholeFAIL；原 tuple 未保存，不补认或回填。 |
| `^TestSchedulerBusyCompensation$` | 1 / 2 wholePASS；真实明确 Busy、逻辑位置恢复、用户修改保全、原事务回滚与重放。 |
| `^(TestSchedulerPendingVisit\|TestTaskHumanHTTP)$` | 2 / 4 wholePASS；有界 visit 与真实 Human HTTP/原意图 Lookup。 |
| `^TestSprintStartHTTP$` | source `ff86ff65` 的9top/两包vet、compile/native全部wholePASS，1 / 1、7.49s；真实 TLS Start/Get/Lookup/重放。 |
| `^TestSchedulerLaunchFinalFailure$` | 最终delivery compile02/native01 wholePASS，1 / 2、39.54s；真实单类拒绝、技术阻塞与标题保全、原Tx整体回滚/结算。 |

Runtime/authorization/Agent capture 的受控纯检查、Schema 核心与历史 SpecRef 适配器检查不等于真实 ToolCall/Invocation；Agent Update、完整 capture/Snapshot 与 Model loop 未获本说明中的真实成功结论。生产 Project initializer 仍未绑定，真实 Agent.Create 使用测试中的正式服务组合，不能称生产 F1 已完成。完整 Dispatcher 的 retry/finalfailure/loop、Task UI、E01 及既有 Object/OpenAI tools/SPA/Jina/Image STOP 不随本候选改变。

## 复现入口与输入

真实业务源码集中在 [tests/projectvariable](../../tests/projectvariable)；固定真实组合在 [assembly.go](../../tests/testsupport/agentconfiguration/assembly.go)。共享入口使用原 [root_chain_driver.py](../work-owner-http/root_chain_driver.py) 和 [pg_only_supervisor.py](../task-planning-recovery/pg_only_supervisor.py)，精确 selector、required inputs 和完整 cases 由原 family 表选择。保留 main 原 Human Skill domain、HTTP 及组合三个 selector，不复制 observer、预算或资源退出方法。

候选编译沿已验的私有编译入口，对 `./tests/projectvariable` 执行 integration/race `-c`，仅列举所选精确 top；使用固定 Go 1.27.1、只读离线模块、任务私有 telemetry/runtime、同进程 fresh ≥5 GiB 门和原进程 Wait/双尾。新的二进制、输出目录和实际输入清单须独立生成，不能拿原候选声称已验证合并后的源码。

在既有资源窗口及原私有环境准备完成后，实际调用仍是原入口：

```sh
python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver .agent-state/work-owner-http/root_chain_driver.py \
  --binary "$CANDIDATE" --run "$EXACT_SELECTOR" \
  --output "$FRESH_OUTPUT" --root-chain
```

保留固定 MinIO、empty Docker config、原 Schema 解释器及 nonce 资源协议；完整判据仍包括实际 Go/driver/supervisor/outer Wait、七资源十四次 absence、private/runtime/desc/TCP 双尾和实际输入初末一致。业务通过不替代 wholePASS；失败保留原材料、先定位首个具体差额，不自动重发业务、不扩大旧矩阵。

旧轮可恢复源码与方法保留在 `ai/agent-system-integration` 及原donor分支，结果保留于原树 `output/ai/agent-system-integration/`。本次结果在原delivery的 `scheduler-failure-01-control/result.json`，恢复方法保留在 `ai/task-flow-delivery`（验收后checkpoint `f9e3c14b`）。三个 `scheduler-failure-*-launcher.py` 和 `.agent-state/task-flow-delivery/app-checks.py` 只保留topic，不作为main入口或本地链接；正式复现使用上面的原通用入口。候选可重建，原FAIL/输入/日志不删除。

## 本次最终接受边界

`app/account.go` 保main Skill management两处接线，只加入Work所需Project authority；六个入口/控制文件将System exact profiles合入main generic family，strict inverse、未知输入拒绝和原退出门均保留并获有限静审。四app受影响top及独立vet已通过；failure编译/真实链已在最终delivery通过，System复用相同产品纯证据，37–46未重跑。

00047及其代码、HTTP只读schema已随B32纳入并完成静审、纯检查和本次真实链。唯一新增类别为真实Work producer返回的 `unsupported_resource_constraints_v1`：同一次原同步KnownNotCreated、私有typed marker与完整原请求/attempt相符，才可持久分类并由Work原事务生成technical-blocker/历史；其它错误不推断永久失败或retry exhaustion。该有限接受不代表完整Dispatcher、生产F1或ready。

native01于2026-10-10 19:23:00–19:25:06 UTC整轮通过：所有原Go/driver/supervisor/outer Wait0，七资源十四次absence、private/runtime/desc/HOST_TCP及outer双尾空，adopted为空，1460inputs首尾一致；资源与cache窗口已归还。compile01容量门前0Go/0PG的FAIL、app-pure01容量wholeFAIL以及旧Launch01 TCP wholeFAIL全部保留，不被后继补集或本轮通过改写。

## Owner 技术阻塞解除组合

新范围仅为 Human 一次 Transfer 原子解除真实 technical blocker 并 blocked→todo、原 key Lookup/重放，以及 caller 原 final Tx 的 late rollback；TLS 与原子性各 1 top/1 sub，共 2 top/2 sub。Scheduler retry policy 本轮只有参数/计数/身份基础检查，不是实际重试或 Dispatcher loop。48 与新测试从当前组合树编译，旧通过矩阵不重跑。

SOURCE `eca3336f`：pure01 的 16 top race＋4 pkg vet wholePASS；native01 于 2026-10-10 19:48:14–19:50:01 UTC wholePASS，TLS 11.96s、原子回滚/正常恢复 10.82s。Go 99090、driver 97437、supervisor 97436、outer 97390 均原 Wait0；七资源 14 次 absent、private/runtime/desc/HOST_TCP 双尾及外层双尾全部关闭，adopted=[]。1,468 输入首尾一致，hash `7d10773b26953f68d6aa5552d26f898633e6b84ea57fb5b9c8812602a377975b`。本批进程、资源及 cache writer 已全部归还。

原件保留于 `output/ai/task-unblock/combined-pure-01/result.json`、`output/ai/agent-system-integration/task-unblock-compile-01/`、`output/ai/agent-system-integration/task-unblock-01-control/result.json`；native 原日志 `/tmp/tub01/pg-c44e79a26bd64c1a8c9cf41c330f8e1c.log`。compile01 原 wholeFAIL 仍保留：compile/list 原 Wait0，只因预期 top 顺序错误；离线按数量和精确集合核原 list、候选 SHA 及原 758 输入后复用，没有重编译或新 list。候选 `task-unblock-race-01.test` 为 59,338,760 B / SHA256 `d3a58a89788475b9a1f5bc1c673944f8eccd2bfb4703944b8a0d02acfc450bc6`。

从仓库根运行下段，使用已推送不可变源码重建 ignored 入口，仅替换集合、root、输出及源码 pin。设置 `AGENTEAM_TASK_UNBLOCK_SOURCE` 为 root 保存的最终组合 commit；未设置时只生成待审副本，compile/native 的原 SOURCE 门拒执行。纯检查固定 16 top/4 pkg vet。每阶段仅在资源窗口内执行，wholePASS/原尾关闭后才接下一阶段；保同进程 fresh≥5GiB、原预算/Wait/subreaper/全部双尾。生成步骤只在本轮尚未运行时使用，失败材料不覆盖。

```sh
export AGENTEAM_TASK_UNBLOCK_SOURCE=eca3336f
python3 -B - <<'PY'
from pathlib import Path
import ast, os, pprint, re, subprocess
root = Path.cwd().resolve()
source = os.environ.get('AGENTEAM_TASK_UNBLOCK_SOURCE')
assert source is None or re.fullmatch(r'[0-9a-f]{8,40}', source)
selector = '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$'
tops = {
 'work': ['TestTaskUnblockFrozenResolutionsAndLegacyPlans', 'TestTaskUnblockResolutionReadsRejectPartialOrStaleSets', 'TestTaskUnblockResolverWritesAndPostimage', 'TestTaskTransitionPlanBindsOriginalIntentAndPostimage', 'TestTaskLaunchFailureHistoryAndEventRemainSeparate'],
 'work/contract': ['TestTaskTechnicalBlockerResolvedReadKeepsSchedulerProvenance', 'TestTaskUnblockHistoryKeepsStandaloneBoundary', 'TestTaskTechnicalBlockerReadDoesNotAuthorizeHumanCreate', 'TestTaskBlockerIdentityAndType', 'TestTaskBlockerRawCapsAndAtomicDecode', 'TestTaskTransitionHistoryTypedFacts'],
 'work/http': ['TestWorkHTTPTechnicalBlockerReadOnly', 'TestWorkHTTPTechnicalBlockerStandardSchema'],
 'scheduler': ['TestSchedulerLaunchRetryPolicyRequiresExplicitValidParameters', 'TestSchedulerLaunchRetryPolicyCountsInitialAttemptAndCapsSafely', 'TestSchedulerLaunchRetryPolicyIdentityIsStableAndImmutable'],
}
def saved(ref, name):
 return subprocess.check_output(['git', 'show', ref + ':.agent-state/agent-system-integration/' + name], text=True)
def replace(text, old, new):
 assert old in text, old
 return text.replace(old, new)
def put(name, text):
 ast.parse(text)
 path = root / 'output/ai/agent-system-integration' / name
 path.parent.mkdir(parents=True, exist_ok=True)
 path.write_text(text); path.chmod(0o700)
pure = saved('148640b8', 'task-launch-failure-pure-checks.py')
node = next(n for n in ast.parse(pure).body if isinstance(n, ast.Assign) and getattr(n.targets[0], 'id', '') == 'TOPS')
lines = pure.splitlines(True)
lines[node.lineno-1:node.end_lineno] = ['TOPS = ' + pprint.pformat(tops, sort_dicts=False) + '\n']
pure = ''.join(lines)
pure = replace(pure, "ROOT = Path('/workspace/agenteam-agent-system-integration')", 'ROOT = Path(' + repr(str(root)) + ')')
pure = replace(pure, '"./internal/central/scheduler", "./internal/central/work", "./internal/central/work/contract", "./internal/central/project", "./internal/central/work/http"', ', '.join(repr('./internal/central/' + p) for p in tops))
pure = replace(pure, 'output/ai/task-launch-failure/combined-pure-01', 'output/ai/task-unblock/combined-pure-01')
pure = replace(pure, 'exact_26_top_pass', 'exact_16_top_pass')
put('task-unblock-pure-checks.py', pure)
build = saved('73387883', 'scheduler-failure-compile-02-launcher.py')
build = replace(build, "SOURCE = '70d383c5'", 'SOURCE = ' + repr(source))
build = replace(build, "ROOT = Path('/workspace/agenteam-task-flow-delivery')", 'ROOT = Path(' + repr(str(root)) + ')')
build = replace(build, 'scheduler-failure-compile-02', 'task-unblock-compile-01')
build = replace(build, 'scheduler-failure-race-02.test', 'task-unblock-race-01.test')
build = replace(build, "(OUT / 'list.log').read_text().splitlines() == ['TestSchedulerLaunchFinalFailure']", "len((OUT / 'list.log').read_text().splitlines()) == 2 and set((OUT / 'list.log').read_text().splitlines()) == {'TestTaskTechnicalResolutionAtomic', 'TestTaskTechnicalResolutionHTTP'}")
build = replace(build, '^TestSchedulerLaunchFinalFailure$', selector)
build = replace(build, 'exact_one_top', 'exact_two_tops')
put('task-unblock-compile-01-launcher.py', build)
native = saved('73387883', 'scheduler-failure-launcher-01.py')
native = replace(native, "SOURCE = '70d383c5'", 'SOURCE = ' + repr(source))
native = replace(native, 'Fixed Scheduler failure', 'Fixed Task unblock')
native = replace(native, 'scheduler-failure-01-inputs.json', 'task-unblock-01-inputs.json')
put('task-unblock-launcher-01.py', native)
print('three original-method copies prepared; no Go or resources')
PY
```

原入口依次为：

```sh
python3 -B output/ai/agent-system-integration/task-unblock-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/task-unblock-compile-01-launcher.py
# 编译通过后先运行下段冻结一次实际输入，再启动 native：
python3 -B output/ai/agent-system-integration/task-unblock-launcher-01.py
```

实际输入生成仅读取候选和源码，不启动资源。native01 固定新 `/tmp/tub01`，输出已存在即停止；新轮必须另定 namespace，不复用 FAIL 目录。原 Go 6m、driver 540s、TERM 60s/KILL 3s、TCP 75s 和七资源/所有原尾不变。

本轮 compile01 原 wholeFAIL 保留：compile/list 均 Wait0、输入及所有实际尾不变，仅预期 top 顺序错误。原 list 恰为 HTTP、Atomic 两项；以下以数量加精确集合离线确认并复用原候选，不改旧 runner/result，不再执行 Go/list。

```sh
python3 -B - <<'PY'
from pathlib import Path
import ast, hashlib, importlib.util, json, os, shutil
root = Path.cwd().resolve(); base = root / 'output/ai/agent-system-integration'
ns = {}
for n in ast.parse((base/'task-unblock-launcher-01.py').read_text()).body:
 if isinstance(n, ast.Assign) and getattr(n.targets[0], 'id', '') == 'SOURCE': ns['source'] = ast.literal_eval(n.value)
assert ns['source'] is not None
spec = importlib.util.spec_from_file_location('unblock_driver', root/'.agent-state/work-owner-http/root_chain_driver.py')
d = importlib.util.module_from_spec(spec); spec.loader.exec_module(d)
compiled = base/'task-unblock-compile-01'
r = json.loads((compiled/'result.json').read_text()); assert r['inputs_unchanged'] and r['method_inputs_unchanged']
listed = (compiled/'list.log').read_text().splitlines()
assert len(listed) == 2 and set(listed) == {'TestTaskTechnicalResolutionAtomic', 'TestTaskTechnicalResolutionHTTP'}
assert [c['name'] for c in r['commands']] == ['compile', 'list']
assert all(c['actual_wait'] == 0 and not c.get('timeout', False) and c['group_absent_first'] and c['group_absent_second'] and not c['descendants_first'] and not c['descendants_second'] and not c['descendants_final'] and not c['survivors_before_cleanup'] and not c['adopted_waits'] and c['runtime_samples'] == [[], []] for c in r['commands'])
assert r['result'] == 'PASS' or (r['result'] == 'FAIL' and r['source'] == 'eca3336f' and r['commands'][1]['exact_two_tops'] is False)
selector = '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$'; binary = base/'task-unblock-race-01.test'
inputs = {str(p): d.sha(p) for p in d.metadata_inputs(binary, selector)}
before = json.loads((compiled/'inputs-before.json').read_text())
assert before == json.loads((compiled/'inputs-after.json').read_text()) and all(inputs.get(k) == v for k,v in before.items())
private = base/'task-unblock-01-toolconfig'; control = base/'task-unblock-01-control'; output = Path('/tmp/tub01')
assert all(not p.exists() and not p.is_symlink() for p in (private, control, output))
env = {'CGO_ENABLED':'1', 'GOCACHE':'/workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build', 'GOFLAGS':'-mod=readonly -p=2', 'GOMAXPROCS':'2', 'GOMODCACHE':'/workspace/shared/agenteam-deps/go-mod', 'GOPROXY':'off', 'GOSUMDB':'off', 'GOTELEMETRY':'off', 'GOTOOLCHAIN':'local', 'LANG':'C.UTF-8', 'LC_ALL':'C.UTF-8', 'PATH':'/workspace/toolchains/go1.27.1/bin:/usr/local/bin:/usr/bin:/bin', 'PYTHONDONTWRITEBYTECODE':'1', 'TZ':'UTC', 'XDG_CONFIG_HOME':str(private/'go-config'), 'DOCKER_CONFIG':str(private/'docker')}
python = '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12'
command = [python, '-B', str(root/'.agent-state/task-planning-recovery/pg_only_supervisor.py'), '--driver', str(root/'.agent-state/work-owner-http/root_chain_driver.py'), '--binary', str(binary), '--run', selector, '--output', str(output), '--root-chain']
assert shutil.which('docker', path=env['PATH']) == '/usr/local/bin/docker'
plan = {'source_ref':ns['source'], 'selector':selector, 'command':command, 'environment':env,
 'removed_environment':['TEST_TELEMETRY_DIR','GO_TELEMETRY_CHILD','GO_TELEMETRY_CHILD_UPLOAD','AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD','AGENTEAM_PG_FIXTURE','AGENTEAM_PG_UNSUPPORTED_FIXTURE','AGENTEAM_OBJECT_FIXTURE','AGENTEAM_OUTBOUND_FIXTURE'],
 'planned_private':str(private), 'planned_control':str(control), 'planned_output':str(output),
 'candidate_bytes':r['candidate']['bytes'], 'candidate_sha256':r['candidate']['sha256'], 'inputs':inputs,
 'input_hash':hashlib.sha256(json.dumps(inputs,sort_keys=True,separators=(',',':')).encode()).hexdigest(),
 'docker_preflight':{'resolved_path':'/usr/local/bin/docker','bytes':43556144,'sha256':'27f239f97492c434e091a70b41b8796c698f0aa2b6052c6f9c28da4ee7ae888b'}}
assert inputs[str(binary)] == r['candidate']['sha256']
fd = os.open(base/'task-unblock-01-inputs.json', os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
with os.fdopen(fd,'w') as out: json.dump(plan,out,indent=2,sort_keys=True); out.write('\n')
print('native inputs frozen',len(inputs),'compile inputs preserved',len(before))
PY
```

## Scheduler retry policy 绑定与真实临时拒绝

SOURCE `6d0a6a5c`，本批 pure01 的 15 top race＋5 pkg vet、compile01/list、native01 均 wholePASS。真实 PG `TestSchedulerRetryBinding` 恰 1 top/2 sub（41.10s）：显式配置→Claim-time policy bytes/digest，同 key 换配置重放仍保原 policy；真实 AgentSH holder 使 final AgentEX 产生 PostgreSQL 55P03，原物理 NotCommitted 后返回绑定原请求的 temporary proof，保持 pending/known_not_created、零 Execution/slot；旧 NULL policy 不回填。未接 RetryDue、next_retry writer、自动重发或耗尽处置。

native01 于 2026-10-10 20:05:04–20:07:11 UTC 结束；Go 115332、driver 113656、supervisor 113655、outer 113589 均原 Wait0，七资源 14 次 absent、private/runtime/desc/HOST_TCP 及外层全部双尾关闭，adopted=[]。1,478 输入首尾一致（含全部 768 compile 输入），hash `1e05cbc3b5f75723c19c12bdaaa1a7d92d2b12c72c00df541ec8267bbc477430`。窗口已归还，无在途 Go/资源；原旧 FAIL 不改。

原件：`output/ai/scheduler-retry-binding/combined-pure-01/result.json`、`output/ai/agent-system-integration/scheduler-retry-binding-compile-01/result.json`、`output/ai/agent-system-integration/scheduler-retry-binding-01-control/result.json`；原日志 `/tmp/srb01/pg-b76f134d09ff4a95a3c56b35ea31a9a3.log`。候选 `scheduler-retry-binding-race-01.test` 为 59,518,544 B / SHA256 `a4d40d71ca2d23f2e174102d531d742a1aa112d5caf18a8ecbe7cea23124378b`，原输入与三个 ignored runner 同目录保留。

跨设备恢复沿上一节已保存 recipe（可用 `git show 6d0a6a5c:.agent-state/agent-system-integration/README.md` 取不可变版本），仍从 `148640b8` pure 与 `73387883` compile/native 原源重建，仅代入以下本批常量；不覆盖任何已有运行目录：

- SOURCE=`6d0a6a5c`；namespace `task-unblock`→`scheduler-retry-binding`，纯结果目录 `output/ai/scheduler-retry-binding/combined-pure-01`，候选 `scheduler-retry-binding-race-01.test`，私有 root `/tmp/srb01`。
- selector=`^TestSchedulerRetryBinding$`；两个 sub 为 `config-bound-claim-and-real-lock-timeout`、`legacy-null-policy-stays-unbound`。compile list 用 `len(listed)==1` 且精确集合 `{'TestSchedulerRetryBinding'}`，保原 `exact_one_top` 名；native input 冻结要求本批 compile `result=='PASS'`，不使用上一批顺序 FAIL 的特例。
- pure `TOPS` 用下列精确 15 名，`exact_26_top_pass` 改为 `exact_15_top_pass`；vet 仅 `config`、`execution`、`execution/contract`、`execution/internal/launchtemporary`、`scheduler` 五包。原正常环境、5GiB 同进程门、预算、Wait、所有资源双尾及 input 首尾一致门不变。

```python
TOPS = {
 'config': ['TestSchedulerRetryConfigurationAbsentOrExplicit', 'TestSchedulerRetryConfigurationRejectsPartialAndInvalid', 'TestSchedulerRetryConfigurationSnapshotAndValidation', 'TestDefaultsAndImmutableConfig', 'TestLoadNeverReadsIgnoredValues'],
 'execution': ['TestLaunchTemporaryRejectionBindingAndSafeProjection', 'TestLaunchTemporaryRejectionRequiresOriginalPhysicalProof', 'TestLaunchTemporaryRejectionDoesNotClassifyOtherPhases', 'TestExecutionLaunchOriginalIdentityReplayAndSlot', 'TestExecutionUnknownLookupDoesNotRepeatLaunch'],
 'scheduler': ['TestSchedulerRetryBindingStrictStoredIdentity', 'TestSchedulerRetryBindingInsertPreservesExplicitAndLegacyPair', 'TestSchedulerRetryBindingConstructorAndStoredReplay', 'TestSchedulerRetryBindingUnknownRetainsOriginalObservedPolicy', 'TestSchedulerLaunchRetryPolicyIdentityIsStableAndImmutable'],
}
```

仅在分配的资源窗口内顺序执行，compile/list 全部原尾通过后，先按上一节原步骤冻结一次本批 `scheduler-retry-binding-01-inputs.json`，再启动 native：

```sh
python3 -B output/ai/agent-system-integration/scheduler-retry-binding-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/scheduler-retry-binding-compile-01-launcher.py
python3 -B output/ai/agent-system-integration/scheduler-retry-binding-launcher-01.py
```

## Scheduler 有界 retry 与真实耗尽

SOURCE `08428005`：pure01 精确 11 top race＋3 pkg vet、compile01/list、native01 均 wholePASS。`TestSchedulerBoundedRetry` 恰 1 top/2 sub（34.35s）：真实 AgentSH→final AgentEX PostgreSQL 55P03/物理 NotCommitted→原 attempt 临时拒绝与持久 due；释放锁后，同 Handoff 到期以原完整请求/key 执行 attempt2，生成唯一 created Execution 并关联。另一场景持续持锁至两次真实 Launch 返回，固定 max2 耗尽后经原 Finalize 写 Work technical blocker、Task blocked、history/Outbox 与 Dispatch.failed；原 Lookup/replay 无重复事实。未运行后台 retry Loop、preparing/执行循环或新增旧 NULL 矩阵；旧 binding due 断言已同步产品语义，但其旧 top 本轮未重跑。

native01 于 2026-10-10 20:24:38–20:26:45 UTC 完成，Go 132562、driver 130860、supervisor 130859、outer 130795 全部原 Wait0；七资源 14 次 absent，private/runtime/desc/HOST_TCP 及 outer 双尾全闭，adopted=[]。1,485 输入首尾一致（含全部 772 compile 输入），hash `7b8d91e268d6c67087d81a33afbe435f7f3af8cf68aaad5954a5d89479752ea3`。窗口已归还，旧 FAIL/候选/输入全保留。

原件：`output/ai/scheduler-bounded-retry/combined-pure-01/result.json`、`output/ai/agent-system-integration/scheduler-bounded-retry-compile-01/result.json`、`output/ai/agent-system-integration/scheduler-bounded-retry-01-control/result.json`；原日志 `/tmp/sbr01/pg-f14ed156a05f4254bccbced271e92460.log`。候选 `scheduler-bounded-retry-race-01.test` 为 59,685,430 B，SHA256 `6d8500b528f9b0b88168dcd48098dea1748aa392a11547b930d406dd9420dd00`。

恢复复用上节 recipe（不可变来源 `git show 08428005:.agent-state/agent-system-integration/README.md`），仍从 `148640b8` pure、`73387883` compile/native 原源重建，仅替换本批 SOURCE、namespace `scheduler-bounded-retry`、私有 root `/tmp/sbr01`、selector `^TestSchedulerBoundedRetry$`。sub 固定 `temporary-due-original-key-created`、`temporary-exhaustion-technical-blocker`；compile list 为 len1＋精确集合，input 冻结必须要求本批 compile PASS，不采用旧顺序 FAIL 特例。pure 用以下精确 11 名、`exact_11_top_pass`，vet 仅 `work`、`work/contract`、`scheduler`；原环境、同进程 5GiB 门、预算/Wait/所有资源尾不变。

```python
TOPS = {
 'work': ['TestTaskRetryExhaustionReusesCurrentFailureFacts'],
 'work/contract': ['TestTaskLaunchFailureExhaustionReasonKeepsProofBoundary'],
 'scheduler': ['TestSchedulerRetryDeadlineAndUnprovenRejection', 'TestSchedulerRetryDueRequiresCurrentEligibilityAndCommit', 'TestSchedulerRetryUnknownAndStopKeepOriginalOwner', 'TestSchedulerRetryAttemptAndProjectionBoundaries', 'TestSchedulerLaunchMarkerMustCommitBeforeHandoff', 'TestSchedulerLaunchAssociationAndOriginalKeyRecovery', 'TestSchedulerLaunchKnownRejectionAndUncertainTransport', 'TestSchedulerLaunchFailureCodecKeepsLegacyAndStrictAttempt', 'TestSchedulerPendingVisitSelectsOnlySupportedOriginalPath'],
}
```

在分配的唯一窗口内依次运行以下入口；compile/list 全尾 PASS 后按上述 recipe 冻结 `scheduler-bounded-retry-01-inputs.json` 再启动 native，已有目录不得覆盖：

```sh
python3 -B output/ai/agent-system-integration/scheduler-bounded-retry-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/scheduler-bounded-retry-compile-01-launcher.py
python3 -B output/ai/agent-system-integration/scheduler-bounded-retry-launcher-01.py
```

## Project 串行遍历与 todo 启动

SOURCE `d2be346a`：pure01 精确 9 top race＋3 pkg vet、compile01/list、native01 均 wholePASS。`TestSchedulerProjectRunner` 恰 1 top/2 sub（21.96s）：真实固定排序 todo 由 runner 串行 Claim→Launch，容量拒绝不改变其余 Task；真实 Execution 已提交、association 原事务回滚形成 pending，正式 pause 时零恢复写入，resume 后原 key Lookup 关联且不重发，并验证 Stop/Drain 等待原调用退出。其他三组保持 Deferred；无 CurrentSprint/旧 Sprint 的 pending 完整性由定向纯控覆盖，本轮未以 SQL 制造该业务状态。未交付跨进程 leader、relaunch/cooldown、自动处理 blocked 或实际执行 Loop。

native01 于 2026-10-10 20:40:15–20:42:02 UTC 完成，Go 147433、driver 145826、supervisor 145822、outer 145758 原 Wait0；七资源 14 次 absent，private/runtime/desc/HOST_TCP 及 outer 双尾全闭，adopted=[]。1,492 输入首尾相同，包含全部 777 compile 输入。窗口已归还；旧 FAIL、候选与输入保留。原件：`output/ai/scheduler-project-runner/combined-pure-01/result.json`、`output/ai/agent-system-integration/scheduler-project-runner-compile-01/result.json`、`output/ai/agent-system-integration/scheduler-project-runner-01-control/result.json`；原日志 `/tmp/spr01/pg-6f63c91f3d0e45ba9e89ef76bd15451a.log`。

恢复沿上述 recipe（不可变来源 `git show d2be346a:.agent-state/agent-system-integration/README.md`），从 `148640b8` pure、`73387883` compile/native 原源重建；仅代入本批 SOURCE、namespace `scheduler-project-runner`、私有 root `/tmp/spr01`、selector `^TestSchedulerProjectRunner$`。sub 固定 `ordered-todo-and-serial-launch`、`paused-pending-recovery-and-join`；compile list 用 len1＋精确集合，input 冻结必须要求 compile PASS，不套旧顺序 FAIL 特例。pure 为以下 9 名及 `exact_9_top_pass`，vet 仅 `work`、`work/contract`、`scheduler`；原环境、同进程 5GiB、预算/Wait/全部资源尾不变。

```python
TOPS = {
 'work': ['TestSchedulerTaskReaderScopeAndEmptySprint', 'TestSchedulerTaskSnapshotOrderAndCompleteTail', 'TestSchedulerTaskReaderCurrentFacts'],
 'scheduler': ['TestSchedulerProjectRunnerConsumesFixedSnapshotOrder', 'TestSchedulerProjectRunnerPrioritizesHistoricalPending', 'TestSchedulerProjectRunnerRechecksScopeAndPacesSkips', 'TestSchedulerProjectRunnerSerialAdmissionAndActualJoin', 'TestSchedulerPendingVisitSelectsOnlySupportedOriginalPath', 'TestSchedulerPendingVisitRechecksPauseInOriginalWriteTransaction'],
}
```

仅在分配的唯一窗口内顺序执行；compile/list 原全尾 PASS 后先按原 recipe 冻结一次 `scheduler-project-runner-01-inputs.json`，再启动 native，已有结果不得覆盖：

```sh
python3 -B output/ai/agent-system-integration/scheduler-project-runner-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/scheduler-project-runner-compile-01-launcher.py
python3 -B output/ai/agent-system-integration/scheduler-project-runner-launcher-01.py
```

## Execution preparing 中的真实 Skill/Tool capture providers

SOURCE `6c3200ed`。pure01 原 wholeFAIL 保留：11 top PASS，4 个新 Skill top 因测试 helper 的非法 JobCause ID 失败；仅修该测试一行后，pure02 的这 4 top＋尚未执行的 5 pkg vet 均 wholePASS，原 11 PASS 复用。compile01/list 与 native01 wholePASS。`TestExecutionCaptureProviders/real-providers-roll-back-with-unbound-snapshot` 恰 1 top/1 sub（16.81s）：真实 P2/Agent/Task Launch 后，PreparationDriver 的私有 preparing authority 经原 Task/Agent capture 调用真实 Skill/Tool providers；同一原事务中观察固定 Skill revision/assignment 与实际 install builtin metadata、两域 head/ref 各一条。完整 Snapshot 提供方尚未齐，原 DependencyUnbound 导致物理 NotCommitted、四表引用全回滚；Execution 仍 preparing 且持 slot，attempt terminal 仅表示原调用已退。原调用外两 provider 均 Forbidden；不宣称完整 Snapshot、running 或 Tool 执行。

native01 于 2026-10-10 21:52:26–21:54:18 UTC 完成，Go 201513、driver 199767、supervisor 199766、outer 199701 原 Wait0；七资源 14 次 absent，private/runtime/desc/HOST_TCP 与 outer 双尾全闭，adopted=[]。1,506 运行输入首尾相同，含全部 788 compile 输入。窗口已归还，所有旧 FAIL/候选/输入保留。原件：`output/ai/execution-capture-providers/combined-pure-{01,02}/result.json`、`output/ai/agent-system-integration/execution-capture-providers-compile-01/result.json`、`output/ai/agent-system-integration/execution-capture-providers-01-control/result.json`；原日志 `/tmp/ecp01/pg-d09d2d10a2394423aac5b4b94516d523.log`。候选 `execution-capture-providers-race-01.test` 为 60,615,294 B，SHA256 `d89e14dc17e4c69cb8cc83b5748f0b93fb668040182af7c6b15f6c29a6434e6f`。

恢复复用本文件原 recipe（不可变来源 `git show 6c3200ed:.agent-state/agent-system-integration/README.md`），从 `148640b8` 的 `.agent-state/agent-system-integration/task-launch-failure-pure-checks.py`、`73387883` 的同目录 `scheduler-failure-compile-02-launcher.py` 和 `scheduler-failure-launcher-01.py` 重建 ignored 入口。仅代入 ROOT 为当前 delivery、SOURCE `6c3200ed`、namespace `execution-capture-providers`、私有 root `/tmp/ecp01`、selector `^TestExecutionCaptureProviders$`；compile list 为 len1＋精确集合，输入冻结要求 compile PASS 并覆盖原 compile 输入，排除旧顺序 FAIL 特例。pure 完整范围为以下 15 名及 `exact_15_top_pass`，vet 仅 `execution`、`tool/registry`、`tool/contract`、`skill`、`skill/contract`。历史补集 `pure-checks-02.py` 仅保留其中 Skill 新 4 名（不含旧 Cleanup top），count 改 4、输出 `combined-pure-02`，5 vet 不变；不覆盖原 01。原正常环境、同进程 5GiB、预算/Wait/全部尾保持。

```python
TOPS = {
 'execution': ['TestExecutionPreparationResourceCaptureOriginalTransaction', 'TestExecutionPreparationResourcePlansRejectChangedSourceAndClaim', 'TestExecutionPreparationResourceDiscoveryStopJoinsOriginalCall', 'TestExecutionPreparationCapturesInOriginalTransactionAndRollsBackPartialInput', 'TestExecutionPreparationUnknownOwnsOriginalAttemptUntilObserved'],
 'tool/registry': ['TestToolExecutionCaptureFixesRealMetadataAndReferences', 'TestToolExecutionCaptureRejectsUnprovenOrChangedFacts', 'TestToolExecutionCaptureEmptySetStillRequiresOwner', 'TestToolExecutionCaptureJoinsOriginalCallAndKeepsUnknown'],
 'skill': ['TestExecutionSkillBindingsFixedRevisionAndEmptyHead', 'TestExecutionSkillBindingsRejectSourceAndAttemptDrift', 'TestExecutionSkillBindingsKeepOriginalOutcomeAndRollback', 'TestExecutionSkillBindingsProtectCleanup', 'TestSkillCleanupBlocksUnretiredAgentInitializationBeforeRelease'],
 'skill/contract': ['TestInitialSkillBindingsKeepFixedCatalogAndClone'],
}
```

在已分配的唯一窗口内依次执行；compile/list 全尾 PASS 后按原 recipe 冻结 `execution-capture-providers-01-inputs.json` 再启动 native，已有输出不得覆盖：

```sh
python3 -B output/ai/agent-system-integration/execution-capture-providers-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/execution-capture-providers-compile-01-launcher.py
python3 -B output/ai/agent-system-integration/execution-capture-providers-launcher-01.py
```

## Model、环境与完整 preparation input

SOURCE `67804902`。限定 pure 共 23 top：原 pure01 中 20 PASS 复用，Mount 三项原素材缺 `sha256:` 前缀；仅两行测试修复后，pure03 的原三项＋11 pkg vet 全 PASS。pure01 素材 wholeFAIL、pure02 容量门 FAIL（0 Go）、native01 容量门 FAIL（0 资源）原件保持，不补写成成功。compile01/list wholePASS；native02 复用该候选和原冻结输入，wholePASS，`TestExecutionModelEnvironmentCapture` 恰 1 top/2 sub（18.67s）：

- `complete-input-unknown-recovery`：正式 Owner 创建 Model 凭据、普通/Secret 变量及 Agent allowlist，显式关闭 AGENTS 注入；真实 Claim/Launch 后，同 preparing 原事务捕获 Model、Skill、Tool、环境专用租约及实际空 Mount head，写完整 typed input。测试仅在真实 COMMIT 成功后丢一次回执，原 owner 观察 exact input/claim 恢复、已知重放不重调 provider；不是 PostgreSQL 物理 CommitUnknown。
- `missing-provider-rolls-back`：缺 Mount 提供方时，原事务 NotCommitted，input、引用及两类租约全部回滚；Model prepared 意图单独保留。两条均仍是 preparing，未 sealed Snapshot、Running、Invoke 或生产 Loop。

native02 于 2026-10-10 22:17:29–22:19:11 UTC 收尾，Go 225958、driver 224346、supervisor 224345、outer 224298 原 Wait0；七资源 14 次 absent、private/runtime/desc/HOST_TCP 及 outer 全部双尾关闭，adopted=[]。1,536 输入首尾相同，完整包含 810 compile 输入；input hash `21692875e9a5f9eebda920c63f1fb6779738e17b99d013f0505928ce1e440ac3`。候选 `output/ai/agent-system-integration/execution-model-environment-race-01.test` 为 61,660,895 B，SHA256 `57ed294d7dc4d81f7258aa775a65a899137993199daedf88fb3aaef508f12115`。窗口已归还，无追加测试。

原结果保留于 `output/ai/execution-model-environment/combined-pure-{01,02,03}/result.json`、`output/ai/agent-system-integration/execution-model-environment-compile-01/result.json`、同目录 `execution-model-environment-{01,02}-control/result.json`；成功 PG 日志 `/tmp/eme02/pg-e40bb2ce120a43dcbb7522f054f46792.log`，supervisor 日志在 `02-control/supervisor.log`。三个旧成功候选（capture-providers、project-runner、bounded-retry）共 180,326,314 B 已按授权退休，仅派生二进制移除，原源码/recipe/输入/PASS/FAIL/日志保留。

恢复沿已保存 recipe（`git show 67804902:.agent-state/agent-system-integration/README.md`），从 `148640b8` pure 和 `73387883` compile02/native01 原源重建，仅代入 SOURCE `67804902`、namespace `execution-model-environment`、selector `^TestExecutionModelEnvironmentCapture$`、compile len1＋精确集合、下列 TOPS 与 `exact_23_top_pass`。vet 精确为 execution、execution/contract、execution/prompt、model、model/contract、mount、mount/contract、projectvariable、projectvariable/contract、secret、secret/contract。补集 pure03 仅保下列 Mount 三名、`exact_3_top_pass` 与独立 `combined-pure-03`，11 vet 不变；旧01/02不可覆盖。

```python
TOPS = {
 'execution': ['TestExecutionPreparationCompleteInputAndConsumerAuthority', 'TestExecutionPreparationInputUnknownAndAtomicRollback', 'TestExecutionPreparationModelUnknownRecoveryUsesOriginalScope', 'TestExecutionPreparationResourceCaptureOriginalTransaction', 'TestExecutionPreparationUnknownOwnsOriginalAttemptUntilObserved'],
 'execution/contract': ['TestPreparationInputCanonicalRoundTrip', 'TestPreparationInputRejectsPartialOrCrossCapture', 'TestPreparationInputClosedEncodingAndSafeDefaults'],
 'execution/prompt': ['TestPlatformPromptVersionedContent'],
 'model': ['TestExecutionModelCaptureCurrentSourceAndClosedProfile', 'TestExecutionModelCaptureRejectsStaleOriginalTransaction', 'TestExecutionModelCaptureDiscoveryUnknownObservation', 'TestExecutionModelCapturePreparedObservationIsExactReadOnly'],
 'model/contract': ['TestExecutionModelCaptureContractCopiesAndSafeFacts'],
 'mount': ['TestExecutionMountCaptureRequiresRealEmptyHead', 'TestExecutionMountCaptureRejectsStaleAndForeignPlans', 'TestExecutionMountCaptureCancellationAndPhysicalUnknown'],
 'projectvariable': ['TestExecutionEnvironmentCapturesValuesAndStableSecretReferences', 'TestExecutionEnvironmentRejectsUnprovenAndChangedFacts', 'TestExecutionEnvironmentEmptyStillRequiresRealOwner', 'TestExecutionEnvironmentOriginalCallJoinsAndUnknownKeepsNoPlan'],
 'secret': ['TestProjectVariableEnvironmentLeaseUsesDedicatedCurrentMetadata', 'TestProjectVariableEnvironmentLeaseRejectsUnprovenAndJoinsOriginalCall'],
}
```

本轮实际入口为 `execution-model-environment-pure-checks-03.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build`、`execution-model-environment-compile-01-launcher.py`、`execution-model-environment-launcher-02.py`，均位于 `output/ai/agent-system-integration/`，用 `python3 -B` 执行且必须先获唯一资源窗口。native02 launcher 仅把 plan 名改 `execution-model-environment-02-inputs.json`；该 plan 复用原 candidate/SOURCE/1,536 inputs/hash，仅将 private/control 及 XDG/DOCKER_CONFIG 改02、output/command 改 `/tmp/eme02`。新轮须另定 fresh namespace；compile PASS 后按原 recipe 冻结实际输入，不采用旧顺序 FAIL 特例。原 5GiB 同进程门、固定 Go/正常环境、预算、真实 Wait、全部资源尾不变。

## 固定 input 的 Execution Context

SOURCE `346003e6720a313cd3d6da6d493e3828dd7d842b`：新增 4 top race 与 3 包 vet、compile/list、native01 均 wholePASS。真实 `TestExecutionTaskContext/frozen-input-after-owner-updates` 恰 1 top / 1 sub，11.34s：正式完整 preparation input 正常提交后构造 Context，再由 Owner 正式更新当前 Task 标题与 Agent 指令，旧 input 与再次 Build 的 canonical bytes/digest、typed Task 和版本化组件保持固定。默认显示和文本组件不含 Secret 明文、credential/lease ID；内部 canonical 保留明确访问的 typed refs，不能整包作为 model messages。Execution 仍 preparing；不宣称 sealed Snapshot、Running 或 Loop assembly。没有重跑上一批 Unknown/缺 provider 矩阵。

原 pure/compile/native 结果依次为 `output/ai/execution-context/combined-pure-01/result.json`、`output/ai/agent-system-integration/execution-context-compile-01/result.json`、同目录 `execution-context-01-control/result.json`；原 supervisor 日志在该 control 目录，PG 日志 `/tmp/ctx01/pg-da2e98d247f541c89e40325ad0850277.log`。pure race/vet Wait0（16.923s/3.743s），compile/list Wait0（13.851s/1.068s）。native 于 2026-10-10 22:32:10–22:33:41 UTC 结束，Go 239739、driver 238199、supervisor 238198、outer 238153 均实际 Wait0；七资源 14 次 absent、private/runtime/desc/HOST_TCP 及 outer 全部双尾关闭，adopted=[]。1,542 native 输入首尾相同且包含全部 814 compile 输入，窗口已归还。旧 FAIL、输入和所有原结果保留。

候选 `output/ai/agent-system-integration/execution-context-race-01.test` 为 61,811,635 B，SHA256 `c3ef046129292fb0b158b03838b12c4b8a25fd63185b0accaaef5b6af49174ae`。恢复沿本文件原 recipe（可从 `346003e6` 读取），原模板仍为 `148640b8` 的 `task-launch-failure-pure-checks.py` 与 `73387883` 的 `scheduler-failure-compile-02-launcher.py` / `scheduler-failure-launcher-01.py`，均在 `.agent-state/agent-system-integration/`。仅代入上述完整 SOURCE、delivery ROOT、namespace `execution-context`、selector `^TestExecutionTaskContext$`、compile `len(listed)==1` 加精确集合、native `/tmp/ctx01` 与 `execution-context-01-inputs.json`；新轮使用 fresh namespace，不覆盖原件。pure TOPS 如下，`exact_4_top_pass`；vet 仅 execution、execution/contract、work。

```python
TOPS = {
 'execution': ['TestExecutionContextBuildKeepsCapturedComponents', 'TestExecutionContextBuildRejectsMissingMismatchedAndCanceledProvider'],
 'work': ['TestTaskContextBuildUsesOnlyFixedTypedSource', 'TestTaskContextBuildRejectsReboundInputAndUnknownComponent'],
}
```

实际三个 ignored 入口均在 `output/ai/agent-system-integration/`：`execution-context-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build` → `execution-context-compile-01-launcher.py` → `execution-context-launcher-01.py`，用固定 Python `-B` 执行。必须先取得唯一资源窗口；每段 wholePASS 后才下一段，compile PASS 后按原输入冻结 recipe 一次生成实际 plan（严格 PASS，不采用旧顺序 FAIL 特例）。原固定 Go/正常环境/RO 模块、同进程 5GiB、预算、实际 Wait、七资源与全部双尾门不变。

## Model 域 AgentRetry 与 ExecutionOwner 租约

SOURCE `870b370bcfa3d7e7c13a565415200fa182add602`。`TestModelAgentRetryRuntime` 恰 1 top/3 sub：`retry-success-and-execution-lease-reuse`、`cancel-prevents-next-attempt`、`nonretryable-single-failure`，真实 PG/TLS 20.38s 全通过。测试明确用 Model 域私有 Consumer authority，正式 Resolver/Runtime/Usage/Secret/ProcessGuard 与新源码构建的 D04 server 全实；503→200 的不同 Invocation/连续 ordinal/逐次 Usage 与材料退出、同 ExecutionOwner 租约跨 call 保留、retry_wait 取消后零后继且 Drain 实退、401 一发失败均成立。不代表生产 Execution 调用授权、54→Loop、首 Round 或 Snapshot/start；Loop 本片仅纯请求组装。

原 pure01 wholeFAIL 保留：`TestDirectTextRequestRejectsUnsupportedProfiles` 的两处小数素材在 canonical input 构造时被拒，其余 8 top PASS。修复仅测试值改合法整数；pure02 只补该 1 top（3.011s）及原 5 包 vet（12.059s），全部 Wait0/双尾闭合，未重跑其余 8 项。原件分别在 `output/ai/model-agent-runtime/combined-pure-{01,02}/{result.json,race.jsonl}`。compile01/list wholePASS（25.367s/1.068s，912 输入同），native01 wholePASS；结果在 `output/ai/agent-system-integration/model-agent-runtime-compile-01/result.json`、`model-agent-runtime-01-control/result.json`，后者同目录保留 supervisor 日志。原 PG 日志 `/tmp/mar01/pg-6da194c254cb4d8ca9f2bcaeb20c1e35.log`。

native 原窗口 2026-10-10 22:51:55–22:53:41 UTC，Go 258426、driver 256965、supervisor 256944、outer 256899 全部实际 Wait0；七资源 14 次 absent、private/runtime/desc/HOST_TCP 和 outer 全部双尾关闭，adopted=[]。1,591 native 输入首尾相同且包含全部 912 compile 输入；窗口已归还。候选 `output/ai/agent-system-integration/model-agent-runtime-race-01.test` 为 69,254,712 B，SHA256 `687baf0f5aae48fc423ad0af912562309870b2dc1d2e965cbdc851ef8160f962`。旧 FAIL、输入、源码和原结果不改。

恢复复用上述 immutable 模板 `148640b8` pure 与 `73387883` compile-02/native-01，只有本批 profile 常量差：ROOT 为 delivery、namespace `model-agent-runtime`、完整 SOURCE 如上；compile 的同包输入枚举与 `go test -c` 目标都改 `tests/model`，list 用 `len==1` 加精确集合 `{'TestModelAgentRetryRuntime'}`。native selector `^TestModelAgentRetryRuntime$`、plan `model-agent-runtime-01-inputs.json`、原输出 `/tmp/mar01`；实际输入按原 recipe 冻结一次且只接受 compile wholePASS。shared metadata family 的本 key 显式 TARGETS=`tests/model`，四 fixture 源均 mandatory；D04 沿原 fixture 构建新 server，不采用旧缓存 server 代替响应序列。新轮使用 fresh namespace，不覆盖原件。

pure01 的 9 top 集合如下；pure02 仅其一个失败项，vet 固定 `./internal/central/agentloop`、`./internal/central/model`、`./internal/central/model/contract`、`./tests/testsupport/outbound`、`./tests/testsupport/outbound/cmd/server`，不跑旧整库矩阵：

```python
TOPS = {
 'agentloop': ['TestDirectTextRequestProjectsCapturedSources', 'TestDirectTextRequestRejectsUnsupportedProfiles', 'TestDirectTextRequestKeepsIsolationAndRejectsIdentityAndCancellation'],
 'model': ['TestRuntimeAgentTimingGrowthSaturates', 'TestRuntimeAgentAndBoundedConsumerPoliciesStaySeparate', 'TestRuntimeAgentRetryNeedsWireFailureAndAuthorizedCategory', 'TestRuntimeAgentCanceledBackoffCreatesNoNextAttempt', 'TestRuntimeStreamFramesAndSafeNestedOutput'],
 'model/contract': ['TestAgentRetryTimingBoundsAndOwnedFields'],
}
```

实际三阶段命令沿原固定 Python `-B`：`output/ai/agent-system-integration/model-agent-runtime-pure-checks-02.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build` → 同目录 `model-agent-runtime-compile-01-launcher.py` → 冻结 plan 后 `model-agent-runtime-launcher-01.py`。pure01 入口仍原位保留，需从零重验新 namespace 时用上述完整 9 top；本次补集不冒完整重跑。固定 Go、正常环境、RO 模块、private XDG、同进程 5GiB、预算、原 Wait 和全部资源尾均保持。

## 真实 Execution 首轮

最终运行 SOURCE `f524b1feebd3e63c4360fccfc0a38f81b45ba0f2`，`TestExecutionFirstRound` 恰 1 top/3 sub：`completed-one-turn`、`start-receipt-loss-recovery`、`cancel-joins-current-call`。native03 全通过（39.05s），真实 preparation input→Snapshot/首 Round/Started→Loop→Model JSON/Usage/Secret→terminal 与双 lease 退休；使用正式 Execution consumer，无测试替代授权。正常 Execution succeeded 不把 Task 改为 done；启动事务实际提交后丢回执，原 owner 观察恢复且不重复 Model；held wire 取消待原调用/Loop 实退后提交 cancelled。回执丢失不冒数据库物理 Unknown，范围仍是显式调用的一轮 direct text，无 tools/stream 或 App 自动运行。

原 pure01 wholeFAIL 保留（Execution 两处 Process ID 类型错误），其余 15 top PASS 复用；修后 pure02 补 Execution 5 top＋13 包 vet 全通过。真实 native01 前两 sub PASS、取消失败；生产窄修仅以实际 Joined 区分业务取消与未退出，再由 pure03 补原第三 Execution top＋execution vet 通过。compile02/list wholePASS；native02 因 slash 子选择器不能用于原 fixture 顶层枚举而失败，业务未启动、七资源记录未形成，不能记资源验收通过。已撤销该新 profile，postgres fixture 源未改；native03 恢复成熟三子项入口，复用 compile02 候选，未再 pure/compile/list。所有原 FAIL、两个候选、输入和日志保留。

结果原件：`output/ai/execution-first-round/combined-pure-{01,02,03}/result.json`；`output/ai/agent-system-integration/execution-first-round-compile-{01,02}/result.json`、`execution-first-round-01-control/result.json`、`execution-first-round-cancel-02-control/result.json`、`execution-first-round-03-control/result.json`。各 control 保留原 supervisor.log；成功 PG 日志 `/tmp/efr03/pg-1f8c63f722e7417d8e6448c603657e8a.log`。native03 在 2026-10-10 23:41:04–23:43:10 UTC 完成，Go 302670、driver 301108、supervisor 301105、outer 301041 原 Wait0；七资源 14 次 absent、private/runtime/desc/HOST_TCP 与 outer 全部双尾关闭，adopted=[]，1,588 输入首尾相同。窗口已归还。

复用候选 `output/ai/agent-system-integration/execution-first-round-race-02.test`：64,558,702 B，SHA256 `db0d41b06180d232b43d455bd2dcb994758530585bda68a2e127e450d8f3dfaa`，编译 SOURCE 为 `30a0b946dde81ee9c1d4002dd0c727f3dbcc3fe2`。最终 f524 仅撤回方法 profile，854 编译输入逐项与原 compile02 相同，候选身份按原门确认；新 native03 plan 独立冻结全部运行输入。此复用不改旧运行的 source/result。

本批容量恢复仅按 owner 确认逐文件退休上述 output 目录中五个旧成功派生产物：`model-agent-runtime-race-01.test`、`execution-context-race-01.test`、`execution-model-environment-race-01.test`（合 192,727,242 B），以及 `scheduler-failure-race-02.test`、`scheduler-retry-binding-race-01.test`（合 118,652,064 B）；对应 source refs/recipe/inputs/PASS 原件都在。root 另对已停用 donor/root 树实施 normal sparse 可逆停放，保留根规则、恢复目录、refs 和全部 ignored/output/FAIL；delivery 源及 ProjectVariable sole hotcache 未停放。接续不要因旧 binary 缺失重建失败记录或自动展开所有 donor；必要构建仍用原恢复 recipe 和资源窗口，原 MinIO/shared mod/hotcache 不变。

恢复继续用本文件既有 recipe（可从 `f524b1fe` 读取）及 immutable 模板：`148640b8` 的 `task-launch-failure-pure-checks.py`、`73387883` 的 `scheduler-failure-compile-02-launcher.py` / `scheduler-failure-launcher-01.py`，均在 `.agent-state/agent-system-integration/`。仅代入 delivery ROOT、本批完整 SOURCE、namespace `execution-first-round`、compile 目标 `tests/projectvariable`、list `len==1` 加精确集合 `{'TestExecutionFirstRound'}`；native 必须用父选择器 `^TestExecutionFirstRound$`，不要恢复 slash profile。原计划 `execution-first-round-03-inputs.json` 和 `/tmp/efr03` 已使用，新运行须新 namespace；输入冻结沿原严格 compile PASS＋完整编译输入子集匹配步骤。完整 pure 使用以下 20 top、`exact_20_top_pass`，vet 为 agentloop 与 skill/model/secret/execution/projectvariable/project 各本包及 contract，共 13 包，不跑旧整库矩阵。

```python
TOPS = {
 'agentloop': ['TestDirectTextControllerAcceptsAndCompletesOwnedTurn', 'TestDirectTextControllerKeepsUnknownHandleWithoutRedispatch', 'TestDirectTextControllerCancellationWaitsForActualModelReturn', 'TestDirectTextTurnRejectsUnsafeOrIncompleteResponses'],
 'skill': ['TestInitialRoundSkillBindingsFixedAndEmpty', 'TestInitialRoundSkillBindingsRejectDriftAndMissingFacts', 'TestInitialRoundSkillBindingsCallerBoundaryAndOutcome'],
 'model': ['TestRuntimeJSONCallRejectsBeforeOwnership', 'TestRuntimeJSONCallCloseJoinsOnlyItsOriginalOwner', 'TestRuntimeJSONCallUnknownObservationDoesNotRedispatch', 'TestExecutionModelRetirementRequiresCurrentConsumerAndOwnPlan', 'TestExecutionModelRetirementWitnessIsScopedAndNoMaterial'],
 'secret': ['TestModelExecutionLeaseObservationUsesCurrentProofAndOriginalRead', 'TestProjectVariableLeaseRetirementIsExactAndCannotBeReacquired'],
 'execution': ['TestExecutionRuntimeModelMatchesExactRoundAndRetirement', 'TestExecutionRuntimeModelDoesNotGrantFromPublicCandidates', 'TestExecutionDirectTextCodecsKeepExactIdentityAndSafeOutput', 'TestExecutionDirectTextLifecycleRequiresClosedTypedEvents', 'TestExecutionDirectTextRejectsUnboundAndForeignOwners'],
 'projectvariable': ['TestEnvironmentRetirementPreservesHistoryAndRequiresOriginalProof'],
}
```

实际 ignored 入口都在 `output/ai/agent-system-integration/`，固定 Python `-B`：`execution-first-round-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build`（原 20 top）；`execution-first-round-pure-checks-02.py`（Execution 5/13 vet）；`execution-first-round-pure-checks-03.py`（第三 top/1 vet）；`execution-first-round-compile-02-launcher.py`；`execution-first-round-launcher-03.py`。这些目录均已执行，不直接覆盖重跑；后续需先获唯一窗口，使用新 namespace，按所改范围选择必要补集。原 Go 1.27.1、只读模块、sole hotcache、private XDG、每进程 fresh≥5GiB、Go 6m/driver 540s/TERM 60s/KILL 3s/TCP 75s、实际 Wait 和全部资源尾保持。

## Scheduler 可靠关联到异步 Execution

SOURCE `573d427567071cea55c2b9778c263472a656d26e`。pure01 精确 10 top race＋3 包 vet、compile01/list、native01 均 wholePASS。`TestSchedulerExecution` 恰 1 top/2 sub（37.80s）：`historical-association-terminal-and-dedup` 用真实旧 Runner 形成已关联未投递的 Execution，pageSize1 补投后由真实 providers/Loop/Model 完成，重复 Runner 遍历保持同 E、单次调用与 Task in_progress；`asynchronous-todo-and-cancel-join` 在真实 held wire 期间遍历返回并继续后项，短 traversal context 不取消独立 executor，Stop/Drain 等原调用退出后提交 cancelled 与双 lease 退休。分页跨终态历史的完整推进由定向纯控覆盖；本片不包含 App 自动装配、多轮或 Task done。

native01 于 2026-10-11 00:05:40–00:07:51 UTC 全尾结束，Go 323988、driver 322304、supervisor 322303、outer 322258 原 Wait0；七资源 14 次 absent，private/runtime/desc/HOST_TCP 与 outer 全部双尾关闭，adopted=[]。1,596 native 输入首尾相同，包含全部 860 compile 输入。候选 `output/ai/agent-system-integration/scheduler-execution-race-01.test` 为 64,844,964 B，SHA256 `b8616a3038576de0c8bbd620ecdc9a485bdc5e4a9cd9d4f22f37cf0af94770a7`。窗口已归还，未追加验证。

原件：`output/ai/scheduler-execution/combined-pure-01/result.json`、`output/ai/agent-system-integration/scheduler-execution-compile-01/result.json`、同目录 `scheduler-execution-01-control/result.json` 与 `supervisor.log`；PG 日志 `/tmp/sxe01/pg-3c07d5cc1b1c4fc5bacffa7ba079cc08.log`。compile 全尾后因已知容量不足暂停，未尝试 native、未产生容量 FAIL；root 对旧 ProjectVariableLifecycle HEAD `0654efe` 的九个 tracked 源目录作 normal sparse 可逆停放，全部 output/sole hotcache、恢复目录、根规则、refs、旧 FAIL 保留。native 原同进程 fresh 为 5,495,574,528 B；本批未删除 candidate 或 cache。

恢复沿既有 recipe（可从 `573d4275:.agent-state/agent-system-integration/README.md` 读取），模板仍为 `148640b8` 的 `task-launch-failure-pure-checks.py`、`73387883` 的 `scheduler-failure-compile-02-launcher.py` / `scheduler-failure-launcher-01.py`，均在 `.agent-state/agent-system-integration/`。仅代入 delivery ROOT、上述完整 SOURCE、namespace `scheduler-execution`、compile `tests/projectvariable` 与 len1＋精确集合 `{'TestSchedulerExecution'}`；native 父 selector `^TestSchedulerExecution$`、plan `scheduler-execution-01-inputs.json`、输出 `/tmp/sxe01`。不使用 slash 子选择器；新轮需 fresh namespace，compile wholePASS 后按原 recipe 冻结实际输入。pure 为下列 10 top、`exact_10_top_pass`，vet 仅 execution、execution/contract、scheduler；原固定环境、5GiB、预算、Wait 与全部尾门不变。

```python
TOPS = {
 'execution': ['TestAssociatedExecutorAdmissionOwnsExactTupleAndCapacity', 'TestAssociatedExecutorRunLifetimeStopsOnlyOwnedCalls', 'TestAssociatedExecutorUnknownRecoveryIsPacedAndUsesOriginalOwner', 'TestAssociatedExecutorCurrentFactsDeferUnsafeTakeover', 'TestExecutionPreparationStopWaitsForOriginalSourceAndCheckpoint'],
 'scheduler': ['TestSchedulerProjectRunnerExecutionPagesReachAndWrapHistory', 'TestSchedulerProjectRunnerExecutionFailuresPreserveCursor', 'TestSchedulerProjectRunnerExecutionPauseAndBorrowedLifetime', 'TestSchedulerProjectRunnerConsumesFixedSnapshotOrder', 'TestSchedulerProjectRunnerRechecksScopeAndPacesSkips'],
}
```

实际三个 ignored 入口均在 `output/ai/agent-system-integration/`，沿固定 Python `-B` 顺序执行：`scheduler-execution-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build` → `scheduler-execution-compile-01-launcher.py` → 冻结 plan 后 `scheduler-execution-launcher-01.py`。须先取得唯一资源窗口，已有结果不覆盖，所有历史 FAIL/输入/日志继续保留。
