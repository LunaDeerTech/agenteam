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

## Work phase relaunch 与持久 cooldown

最终 SOURCE `146594e6a993eaaafb8be7aa427d20b7a1e41426`，compile04/list 与 native04 wholePASS。`TestSchedulerRelaunch` 恰 1 top/2 sub（25.59s）：`cooldown-restart-and-new-execution` 从真实旧 Execution succeeded 开始，skipCount2 跨新 owner 持久减为1、0，下一次访问才建新 Dispatch/key/Execution，保持原 Task in_progress、版本/history 与唯一 todo Claim；`relaunch-failure-atomic-block-and-replay` 使用真实 typed 永久拒绝，在最终事务完整写入 schema5、双来源47/58关系、Task/blocker/history/Dispatch 后物理回滚，再由原请求正常结算与幂等重放。cooldown 是访问次数，不是墙钟期限；本片未接 App 生产装配、review/revise 或系统 blocked 自动解除。

pure04 补直接受影响 6 top＋4 包 vet 全通过，与未变 12 top PASS 合计18；原 pure01/02 的类型错误和 pure03 的4项失败均保留，修复及补集不冒完整重跑。native01 在迁移失败且没有底层诊断时停止；native02 新增安全 SQLSTATE 后复现22P02，修58三处 JSON 提取/减法括号；native03 迁移已过但新 origin 写入失败，后修58两处 Version 的 canonical JSON 字符串类型，并纠正 fixture 的 admission helper 位置及原始错误诊断。58 属 Go embed 输入，每次修后都重新编译。三个 native wholeFAIL、四个候选及全部输入/日志原件保留，不回填旧结果。

native04 于 2026-10-11 00:51:48–00:53:42 UTC 完成，Go 367928、driver 366286、supervisor 366285、outer 366220 原 Wait0；七资源14次 absent、private/runtime/desc/HOST_TCP 与 outer 全部双尾关闭，adopted=[]。1,614 native 输入首尾一致，含全部875 compile 输入。候选 `output/ai/agent-system-integration/scheduler-relaunch-race-04.test` 为65,603,644 B，SHA256 `ec3167a242bf6870826e6c3e941e89ea80c1c6ff94627cae42cf7ff5470b5ef7`。窗口已归还，不追加测试。

原件为 `output/ai/scheduler-relaunch/combined-pure-{01,02,03,04}/result.json`、`output/ai/agent-system-integration/scheduler-relaunch-compile-{01,02,03,04}/result.json`、同目录 `scheduler-relaunch-{01,02,03,04}-control/result.json` 与各 `supervisor.log`；成功 PG 日志 `/tmp/srl04/pg-ef24c95e204046f4ad4c40957741a724.log`。pure04 SOURCE 为 `a84edefd59ac227d0b88b6151351dc05715a90ff`；后续差额仅运行时 fixture 诊断/断言与嵌入迁移，纯测闭合证据复用。

恢复沿本文件既有 recipe（原版可读 `573d4275:.agent-state/agent-system-integration/README.md`），immutable 模板仍为 `.agent-state/agent-system-integration/` 下 `148640b8` 的 `task-launch-failure-pure-checks.py`、`73387883` 的 `scheduler-failure-compile-02-launcher.py` / `scheduler-failure-launcher-01.py`。仅代入 delivery ROOT、上述最终 SOURCE、namespace `scheduler-relaunch`、compile `tests/projectvariable`，list 数量1＋精确集合 `{'TestSchedulerRelaunch'}`；native 必须为父 selector `^TestSchedulerRelaunch$`。compile wholePASS 后按原严格输入冻结步骤生成 plan，保留875编译输入子集检查。已用 plan 为 `scheduler-relaunch-04-inputs.json`、输出 `/tmp/srl04`；重新运行必须换 fresh namespace，不覆盖任何结果。

从零恢复 pure 使用以下18 top、`exact_18_top_pass`，vet 仅 `work`、`work/contract`、`scheduler`、`project`；本次最后补集为 Work 的 Origin/Records/Schema/CurrentClaim 四项与 Scheduler 的 Cooldown/Unknown 两项。

```python
TOPS = {
 'work': ['TestTaskRelaunchOriginIsDistinctFromTodoClaim', 'TestTaskRelaunchRecordsOnlyOriginAndRechecksCurrentFacts', 'TestTaskRelaunchRejectsForeignProofAndPreservesOriginalOutcome', 'TestTaskRelaunchFailureOriginsKeepLegacyEncoding', 'TestTaskRelaunchFailureSchemaDoesNotRewriteClaimHistory', 'TestTaskLaunchCurrentClaimAndFrozenSource', 'TestTaskLaunchFailureHistoryAndEventRemainSeparate'],
 'scheduler': ['TestSchedulerRelaunchCooldownPersistsVisits', 'TestSchedulerRelaunchUnknownKeepsOriginalVisit', 'TestSchedulerRelaunchBusyKeepsTaskAndCooldown', 'TestSchedulerLaunchAssociationAndOriginalKeyRecovery', 'TestSchedulerBusySettlementRequiresWorkProofInOriginalTransaction', 'TestSchedulerLaunchFailureSettlementChecksCurrentProofAndCommit', 'TestSchedulerRetryBindingInsertPreservesExplicitAndLegacyPair', 'TestSchedulerProjectRunnerPrioritizesHistoricalPending'],
 'project': ['TestSchedulerClaimProjectEventExactGate', 'TestTaskBusyCompensationProjectGatePausedAndExactSchema', 'TestTaskLaunchFailureProjectGatePausedAndExactSchema'],
}
```

实际 ignored 入口保留于 `output/ai/agent-system-integration/`，原调用命令如下；已有 namespace 仅供复原核对，不能原位重跑。固定 Go1.27.1、只读模块、sole hotcache、private XDG、每进程 fresh≥5GiB、原 Go6m/driver540s/TERM60s/KILL3s/TCP75s、实际 Wait 与所有资源尾不变。

```sh
python3 -B output/ai/agent-system-integration/scheduler-relaunch-pure-checks-04.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/scheduler-relaunch-compile-04-launcher.py
# compile PASS 后，先按原 recipe 冻结实际输入再启动 native：
python3 -B output/ai/agent-system-integration/scheduler-relaunch-launcher-04.py
```

本批容量恢复只退休前批全PASS派生产物 `scheduler-execution-race-01.test`（64,844,964 B），其 source/recipe/inputs/results 保留；root 对已停用 TaskHumanHTTP、TaskTransition、Source、旧 WorkUI 与 independent UI 树作 normal sparse 可逆停放，保留 output/ignored/FAIL、根规则、恢复目录和 refs。delivery、sole hotcache/shared mod/MinIO 未停放，所有本批 FAIL candidates 保留；后继不要自动展开全部 donor 或删除失败证据。

## Human review 主流程

SOURCE `8cfbe24d21938947787fcfeb7633b64a72866e94`。12 top race 全PASS，补验4包 vet、compile01/list 与 native02 均 wholePASS。`TestTaskHumanReview` 恰1 top/2 sub（34.54s）：`completed-work-review-and-done` 在真实 work Execution 终态后，经 Owner HTTPS 交给真实当前 reviewer、转 in_review，再接受为 done并保留省略的 reviewer，验证权限、Get/Lookup/原 key replay；`review-rework-late-transaction-rollback-and-replay` 用另一真实 Task 从 review 返工到 todo，在原最终事务完整写入后物理回滚，再以原请求正常提交和重放，验证当前组追加位置、history/receipt/Outbox/activity 原子性。两条均保留原 Execution/Dispatch lineage，不由模型终态自动完成 Task，也未增加生产 Scheduler/App 装配。

原 pure01 wholeFAIL 仅 vet 启动门容量不足：12 top 已PASS，0 vet；随后 `combined-vet-02` 只补原4包，不重race。compile01 一次通过；native01 仅同进程容量门失败，0资源/0业务，未到输入核验的 `inputs_unchanged=false` 不表示发现源变更。native02 只换新 namespace，复用原 candidate/876编译输入与1616运行输入。两容量FAIL及全部原件保持，不改判。

native02 于 2026-10-11 01:12:35–01:14:39 UTC 完成，Go386695、driver385072、supervisor385071、outer385026 原 Wait0；七资源14次 absent，private/runtime/desc/HOST_TCP 与 outer 全部双尾关闭、adopted=[]，1616输入首尾同。候选 `output/ai/agent-system-integration/task-human-review-race-01.test` 为65,726,522 B，SHA256 `15c388ade46e62e83bce414610565c4bdc9313eeb7bd3d29ae130cb092e9d318`。窗口已归还。

原件：`output/ai/task-human-review/combined-pure-01/result.json`、`combined-vet-02/result.json`；`output/ai/agent-system-integration/task-human-review-compile-01/result.json`、`task-human-review-{01,02}-control/result.json` 与02的 `supervisor.log`；成功 PG `/tmp/thr02/pg-fe195712631b47858e3746c295ebb0b7.log`。root 精确退休旧第七批全PASS `scheduler-relaunch-race-04.test`（65,603,644 B），保留对应源码refs/recipe/inputs/results与全部07失败候选；另普通 sparse 停放旧 feature-integration、ToolOperation 的九个 tracked 源目录，output/FAIL/state/config/refs保留。未清理本批候选、hotcache、模块或 MinIO。

恢复使用上述成熟 recipe 与 immutable 模板：`148640b8:.agent-state/agent-system-integration/task-launch-failure-pure-checks.py`，以及 `73387883` 同目录 `scheduler-failure-compile-02-launcher.py` / `scheduler-failure-launcher-01.py`。仅代入 delivery ROOT、上述完整SOURCE、namespace `task-human-review`、compile包 `tests/projectvariable`、list数量1＋精确集合 `{'TestTaskHumanReview'}`；native只用父 selector `^TestTaskHumanReview$`，不加 slash profile。compile PASS后沿原步骤冻结实际输入并验证全部编译输入子集。已用 plan `task-human-review-02-inputs.json`、输出 `/tmp/thr02`，后继必须新 namespace，不覆盖任何旧结果。

完整pure为下列12 top、`exact_12_top_pass`；vet恰同4包，固定 `AGENTEAM_WORK_HTTP_SCHEMA_PYTHON` 沿原入口。原Go1.27.1、只读模块、sole hotcache、private XDG、每进程fresh≥5GiB、全部预算/实际Wait/资源尾不变。

```python
TOPS = {
 'work/http': ['TestWorkHTTPReviewTransitionsAndOriginalLookup', 'TestWorkHTTPTaskTransitionStandardSchema'],
 'httpapi': ['TestWorkProblemsPreservePublicFailureFacts', 'TestSharedSchemaMatchesImplementedBoundary'],
 'foundation': ['TestTaskFaultCodesPreserveSafeProjectionAndCause'],
 'work': ['TestTaskHumanReviewPresenceAndActiveCapability', 'TestTaskHumanReviewCanonicalHistoryAndRetainedReviewer', 'TestTaskHumanReviewBlockersUseTargetInvariant', 'TestTaskTransitionPlanBindsOriginalIntentAndPostimage', 'TestTaskUnblockFrozenResolutionsAndLegacyPlans', 'TestTaskUnblockResolutionReadsRejectPartialOrStaleSets', 'TestTaskUnblockResolverWritesAndPostimage'],
}
```

实际 ignored 调用命令如下，原 pure入口仍保留；补验入口只移除已通过race、改输出目录，不能把它记成12 top重跑。重建后须另定fresh namespace并取得唯一执行窗口。

```sh
python3 -B output/ai/agent-system-integration/task-human-review-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
# 本次只补未执行的vet：
python3 -B output/ai/agent-system-integration/task-human-review-vet-checks-02.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/task-human-review-compile-01-launcher.py
# compile PASS并冻结新plan后：
python3 -B output/ai/agent-system-integration/task-human-review-launcher-02.py
```

## Scheduler 自动 review dispatch

最终 SOURCE `a3b72c72b4d8ba26610e7096a99113cf115f36a8`。pure01 的14 top race＋3包vet全PASS；后两次仅改新native fixture，原pure输入不变并复用。compile03/list及native02 wholePASS，`TestSchedulerReviewDispatch`整top两sub共49.09s：`phase-isolated-review-execution`从真实work终态与Human交review开始，由新reviewer的正式Model执行验证task/review输入、Prompt/HTTPS请求、双phase pointer及跨owner持久skip，Task仍in_review，再由Human明确done；`review-failure-atomic-block-and-replay`用真实typed永久拒绝，在原最终事务全部事实写入后物理rollback，再按原请求结算与重放，保留另一phase事实。未增加App自动装配、active Human交接、多轮/tools/Stream或Task自动done。

compile01因fixture三处把TextPart当string而FAIL，修为Text.Text后compile02通过；native01首sub终态后验错误引用不存在的`e.purpose`，原SQLSTATE42703/Position1721，第二subPASS，整轮仍FAIL。唯一谓词改读真实`launch_request->>'purpose'`后编译新candidate03，并沿成熟whole-top入口重验两sub。原compile01/native01结果、candidate02及全部旧FAIL/输入/日志不变，不回填原轮。

native02于2026-10-11 01:44:33–01:46:53 UTC结束；Go414081、driver412543、supervisor412542、outer412497原Wait均0，七ID14次absence，private/runtime/desc与TCP全部双尾关闭、delta0/adopted[]，1620输入首尾同（含878编译输入）。候选`output/ai/agent-system-integration/scheduler-review-dispatch-race-03.test`为65,919,007 B，SHA256 `cd3bc9cfc5bbbe965a9937f7f39806e9a03fc7fb97473950c8a651e75b3657a1`；窗口已归还，无新增验证待跑。

原件：`output/ai/scheduler-review-dispatch/combined-pure-01/result.json`（SOURCE `389c40a3`）；`output/ai/agent-system-integration/scheduler-review-dispatch-compile-{01,02,03}/result.json`、`scheduler-review-dispatch-{01,02}-control/result.json`及各`supervisor.log`；成功PG `/tmp/srd02/pg-86823bd8d4a247038220ccd926abe888.log`。本轮启动前root普通sparse停放D13/D18/D16九tracked目录，protected output/state/config/rootfiles、refs和全部FAIL保留；未清理第八candidate01、两容量FAIL或任何共享cache/MinIO。

恢复仍用本文件原recipe和immutable模板：`148640b8:.agent-state/agent-system-integration/task-launch-failure-pure-checks.py`，以及`73387883`同目录`scheduler-failure-compile-02-launcher.py`/`scheduler-failure-launcher-01.py`。只代入delivery ROOT、上述完整SOURCE、namespace `scheduler-review-dispatch`、compile `tests/projectvariable`及list数量1＋精确集合`{'TestSchedulerReviewDispatch'}`；native父selector `^TestSchedulerReviewDispatch$`，不加slash。已用compile03/candidate03、native02 plan `scheduler-review-dispatch-02-inputs.json`和`/tmp/srd02`；新运行须fresh namespace。compile wholePASS后沿原步骤冻结实际输入，包含全部编译输入子集。原固定Go1.27.1/唯一hotcache/RO模块/private环境、每实际调用same-process fresh≥5GiB、预算/Wait/资源尾均不变。

pure设`exact_14_top_pass`，vet仅work、work/contract、scheduler；精确TOPS如下，不复跑旧整库：

```python
TOPS = {
 'work': ['TestTaskReviewDispatchUsesCurrentReviewerOrigin', 'TestTaskReviewDispatchRejectsCrossPhaseAndBindsFailureMarker', 'TestTaskReviewFailurePreservesOtherPhaseAndKeepsSchemaFour', 'TestTaskRelaunchOriginIsDistinctFromTodoClaim', 'TestTaskRelaunchRecordsOnlyOriginAndRechecksCurrentFacts', 'TestTaskLaunchCurrentClaimAndFrozenSource', 'TestTaskRelaunchFailureSchemaDoesNotRewriteClaimHistory', 'TestTaskLaunchFailureHistoryAndEventRemainSeparate'],
 'scheduler': ['TestSchedulerReviewEligibilityAndRunnerSelection', 'TestSchedulerReviewCooldownAndUnknownKeepOriginalPhase', 'TestSchedulerReviewBusyAndFinalFailureKeepOrigin', 'TestSchedulerRelaunchCooldownPersistsVisits', 'TestSchedulerRelaunchUnknownKeepsOriginalVisit', 'TestSchedulerLaunchAssociationAndOriginalKeyRecovery'],
}
```

```sh
python3 -B output/ai/agent-system-integration/scheduler-review-dispatch-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3 -B output/ai/agent-system-integration/scheduler-review-dispatch-compile-03-launcher.py
# compile PASS并按原recipe冻结新plan后：
python3 -B output/ai/agent-system-integration/scheduler-review-dispatch-launcher-02.py
```

## Project 多实例管理与 Agent 安全目录

SOURCE `2c59cf0d7eee18f968a688b4c7cc003211863ac2`；pure01 精确17 top race＋7包vet、一次compile01/list两top、两组native01均wholePASS。A `TestProjectRunnerManager` 的 `candidate-pages-and-project-facts`、`cross-project-capacity-and-borrowed-executor-join` 共28.40s，验证真实Project有限发现、多Project容量与借用executor的独立退出；B `TestAgentDirectoryHTTP` 的 `normal-directory-pagination`、`current-owner-boundary` 共26.95s，验证真实Owner HTTPS分页与当前权限。范围不包含完整App Runtime装配、ready或页面。

两组各七资源14次absence、全部private/runtime/desc/TCP双尾关闭、delta0/adopted=[]，各1640输入首尾同，包含889编译输入。A原Wait0为Go435911/driver434328/supervisor434327/outer434262（02:08:55–02:10:55 UTC）；B为Go439127/driver437668/supervisor437667/outer437601（02:11:14–02:13:06 UTC）。共享候选 `output/ai/agent-system-integration/project-runtime-support-race-01.test` 为66,400,625 B，SHA256 `5f3270ca110b758276a77ec274fddbdbfaa23805ecbe35ba3b9b07c862df07c9`。本批无FAIL，窗口已归还。

原件：`output/ai/project-runtime-support/combined-pure-01/result.json`；`output/ai/agent-system-integration/project-runtime-support-compile-01/result.json`；同目录 `project-runner-manager-01-control/`、`agent-directory-http-01-control/` 各 `result.json` 与 `supervisor.log`。PG分别 `/tmp/prm01/pg-9101d6ac178744d2aae8aa6a4ff1b016.log`、`/tmp/adh01/pg-820c5a50ea934f4385814a0bfbd19eba.log`。

恢复沿前述immutable模板与原输入冻结recipe，只替换ROOT/SOURCE/namespace及下列精确清单。compile同包 `tests/projectvariable` 一次，list selector `^(TestProjectRunnerManager|TestAgentDirectoryHTTP)$` 用len2＋精确集合，不能依列表顺序；两native各用独立whole-top selector与plan `project-runner-manager-01-inputs.json` / `agent-directory-http-01-inputs.json`，输出 `/tmp/prm01` / `/tmp/adh01`。已有namespace不覆盖；重建后在唯一窗口用新namespace、冻结实际输入并核编译输入子集。原Go1.27.1/唯一hotcache/RO模块/private环境、每实际调用fresh≥5GiB、全部预算/Wait/资源尾保持。新增固定 `AGENTEAM_AGENT_DIRECTORY_SCHEMA_PYTHON=/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3`；pure设 `exact_17_top_pass`，vet恰下列7包。

```python
TOPS = {
 'project/contract': ['TestSchedulerProjectPagesBoundOneDiscoveryCycle'],
 'project': ['TestSchedulerProjectDiscoveryTransactionBoundaries', 'TestSchedulerProjectDiscoveryCompleteRowsAndClose'],
 'scheduler': ['TestSchedulerProjectRunnersBoundedCyclesAndCapacity', 'TestSchedulerProjectRunnersWaitForActualCallsAndBorrowExecutor', 'TestSchedulerProjectRunnersKeepOriginalUnknownBeforeRestart'],
 'agent/contract': ['TestAgentDirectoryEntrySafeProjectionAndStrictCodec'],
 'agent': ['TestAgentDirectoryPublicationAndScopedCursor', 'TestAgentDirectoryCurrentOwnerAndFailurePublishNothing', 'TestAgentDirectoryCancellationWaitsForOriginalRead'],
 'agent/http': ['TestAgentDirectoryHTTPReadBoundaryAndProjection', 'TestAgentDirectoryHTTPOriginalCallAndIOJoin', 'TestAgentDirectoryHTTPStandardSchema'],
 'app': ['TestAgentDirectoryRootConstructionAndRoutes', 'TestAgentDirectoryRootWaitsForOriginalRead', 'TestWorkPlanningRootRouteOwnership', 'TestSkillManagementRootRoutesKeepReadCompatibility'],
}
```

实际入口位于 `output/ai/agent-system-integration/`，以下 `python3.12` 为固定 `/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12`：

```sh
python3.12 -B output/ai/agent-system-integration/project-runtime-support-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3.12 -B output/ai/agent-system-integration/project-runtime-support-compile-01-launcher.py
# compile wholePASS并按原recipe冻结两plan后，共用同一candidate顺序运行：
python3.12 -B output/ai/agent-system-integration/project-runner-manager-launcher-01.py
python3.12 -B output/ai/agent-system-integration/agent-directory-http-launcher-01.py
```

资源准备仅root普通sparse停放旧ToolRegistry九tracked目录（4e3323，后验8c7646）；compile全尾后精准退休旧第九全PASS可再生bin `scheduler-review-dispatch-race-03.test`（05777c，65,919,007 B/allocated65,925,120 B）。原源码refs/recipe/results/inputs及FAIL candidate02、全部ignored/output/state/config继续保留；当前候选与shared cache/MinIO未清理。

## Execution RuntimeAccess 拒绝审计

第十一批B SOURCE `64bdbb6f15531d55f56cd8e0b891b18a5b8c5056`：Project五top race＋单包vet、compile01/list、native01全部wholePASS。`TestExecutionRuntimeAccessAudit`整top两sub22.41s：`policy-denial-requires-live-runtime-handoff`沿真实Execution/Model/D04拒绝与单原ProjectAuthority wrapper核审计；`default-factory-completed-one-turn`核旧默认factory正常首轮。没有更换原Owner/授权来源，不外推第十一批A浏览器或完整App Runtime装配。

native01于2026-10-11 02:30:03–02:31:54 UTC结束，Go456047/driver454419/supervisor454398/outer454352原Wait0；七ID14次absence、所有private/runtime/desc/TCP双尾闭合、delta0/adopted=[]、1643输入首尾同（含891编译输入）。候选 `output/ai/agent-system-integration/execution-runtime-access-audit-race-01.test` 为66,467,736 B，SHA256 `ae1352b233159c62924e1557d664a3c7ac85fe7bc1142a27f562e395f0e3f289`。本片无新增FAIL，窗口已归还。

原件：`output/ai/execution-runtime-access-audit/combined-pure-01/result.json`（pure SOURCE `a3eb45b1`）；`output/ai/agent-system-integration/execution-runtime-access-audit-compile-01/result.json`、`execution-runtime-access-audit-01-control/{result.json,supervisor.log}`；PG `/tmp/eraa01/pg-2e4f25ddb30a4bc88641bed5ecbe1e90.log`。后续只新增fixture/methods，pure输入不变并复用。

恢复沿上述immutable模板/输入冻结recipe；pure `exact_5_top_pass`，仅project包五top：`TestRuntimeAccessAuditAuthorityConstructionAndDelegation`、`TestRuntimeAccessAuditAuthorityOriginalFacts`、`TestModelAccessAuditExactTxAndOriginalDenyFault`、`TestModelAccessAuditCurrentGateAndExactActor`、`TestModelAccessAuditRegistrationDoesNotGrantOtherProducer`；vet仅该包。compile包 `tests/projectvariable`，list数量1＋精确集合 `{'TestExecutionRuntimeAccessAudit'}`；native whole-top `^TestExecutionRuntimeAccessAudit$`，plan `execution-runtime-access-audit-01-inputs.json`，输出 `/tmp/eraa01`。原完整Go/embed/support闭包不缩减；新轮须fresh namespace与唯一窗口，固定Go/Python/sole hotcache/RO模块/private环境及每进程fresh≥5GiB、预算/实际Wait/全部尾保持。

```sh
python3.12 -B output/ai/agent-system-integration/execution-runtime-access-audit-pure-checks.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build
python3.12 -B output/ai/agent-system-integration/execution-runtime-access-audit-compile-01-launcher.py
# compile wholePASS后按原recipe冻结新plan：
python3.12 -B output/ai/agent-system-integration/execution-runtime-access-audit-launcher-01.py
```

本批准备仅root普通park指定旧ExecutionIdentity/ModelTextRuntime九tracked目录（`038ac9`/`1f6f79`），protected与原output存在/absent后验`9822e2`保持；精确退休第十全PASS candidate01可再生bin（`86e15b`，66,400,625 B/allocated66,408,448 B）。原source/recipe/results/inputs、全部FAIL与sharedcache/MinIO保留；Web/PW仅复用既有同锁安装，B结果不冒A已执行。

## Work 评审 UI（真实链已通过）

最终SOURCE `53bd25cb80b2b890edb4df14452fede05edeb5e1`。`TestTaskReviewWeb`整top两sub为`review-complete`与`lost-confirmation-lookup-and-session-revocation`：真实完成work后交review/done，另一Task返工回执截断后保留Unknown、查询原操作并验证Logout/Session撤销。native06于2026-10-11 03:36:21–03:39:36 UTC wholePASS，1top2sub86.97s（43.42/43.55）。Go516688/driver514996/supervisor514974/outer514909四原Wait0，Node517027/517677在实际Wait后success=true；七ID14次absence、private/runtime/desc及SUP/outer TCP全双尾闭、delta0/adopted[]、2404输入首尾同。当前仅本片验收，不冒完整app runtime/initializer/ready；完整七列与深色视觉不由详情截图外推。

前端有效结果按实际输入复用：frontend06的31定向控验证JSON/204/avatar原body生命周期；frontend07的Session20＋WorkReviewSession4及type/build通过；最终frontend08仅补成功204统一empty reader的body9＋精确旧5、两path格式、type/build，全部PASS/各原Wait0与双尾空，legacy120未选不计通过。旧auth62/state3等未受影响结果保留、不重整矩阵。上述目录均在`output/ai/work-review-web/`，各`result.json`保存精确argv、范围和退出尾；frontend06/08另有`core-unit.json`、`legacy-unit.json`，frontend07为`unit.json`。最终独有生产产物为`frontend-08/dist`，旧dist全部保留。

compile03/list在SOURCE `64701249`通过，精确父top1、898 Go/embed输入首尾同，原件`output/ai/agent-system-integration/task-review-web-compile-03/{result.json,inputs-after.json}`。候选`task-review-web-race-03.test`为69,629,851 B，SHA256 `df3ce2d5e30bf67310345b0bd56f7c6247fc99daf5c327fc0e53295b3d384aed`（同上输出根）。后继仅method/runtime JS/client/前端测试变化，原898记录不变，native04–06复用候选、不重复compile。native06 plan为`task-review-web-06-inputs.json`；相05只换70项dist07→dist08，共同记录仅client变化，2404闭集hash `6337eb6045e826e8cc7eea168259f8d7f6f446372f0188a2da5d8718bd6e6efc`。

最终原件为`output/ai/agent-system-integration/task-review-web-06-control/{result.json,supervisor.log}`、PG `/tmp/trw06/ui-008f2459cc554212.log`和`output/ai/work-review-web/native-06-evidence/`。原native01/02/03初始读取observer FAIL、native04 typed投影/Unknown文案fixture FAIL、native05首case PASS而最后Logout FAIL均原样保留，未回填wholePASS；compile01输入枚举0Go FAIL、frontend01缓存尾/02类型及page加载/03按钮helper FAIL也保留。当前candidate03、旧candidate02、所有输入/诊断/截图/结果与旧dist不删除。

恢复沿原三methods的`^TestTaskReviewWeb$`profile：mandatory六项为`task_review_web_test.go`、`task_review_test.go`、`task-review.config.js`、`task-review.spec.ts`、`task-review.helpers.ts`与只读`knowledge-owner-read.native.ts`，完整Go/embed/browser及批准依赖闭包不缩减。GET200/exact ERR_ABORTED仅按同request的Go长度/SHA、原reader完整EOF与退役、公开typed消费fulfilled/current、PW唯一终态全部联证；mutation判据不豁免。固定Node v24.19.0、同锁PW1.56.1、批准Chromium、`NODE_DISABLE_COMPILE_CACHE=1`及原private环境；Go1.27.1/唯一hotcache/RO模块、每实际进程fresh≥5GiB、原60s Node/45s Playwright预算、实际Wait和七资源全尾保持。以下为实际入口，已有namespace不可重用；恢复须独立namespace和明确资源窗口，不自动重跑已通过输入。

```sh
# 已执行的唯一新Go候选编译/list；后继runtime JS/前端变更不重编：
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/task-review-web-compile-03-launcher.py
# 已执行的最终native06：
AGENTEAM_TASK_REVIEW_WEB_DIST=/workspace/agenteam-task-flow-delivery/output/ai/work-review-web/frontend-08/dist \
AGENTEAM_TASK_REVIEW_WEB_EVIDENCE=/workspace/agenteam-task-flow-delivery/output/ai/work-review-web/native-06-evidence \
NODE_DISABLE_COMPILE_CACHE=1 \
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/task-review-web-launcher-06.py
```

## Task intake与Agent配置创建有限组合

第十二批A与第十四批真实验收共用SOURCE `75e23e9ddfc72e5d72b83af5c66cc695baf1a6c6`及candidate06，各自独立运行并留存输入与资源尾：

| 入口 | 实际范围与结果 | 原件（delivery的output/ai/agent-system-integration下） |
| --- | --- | --- |
| `TestTaskIntakeWeb` | native06两sub37.63s：`create-and-ready`18.85s、`lost-create-confirmation-and-ready`18.78s；四Wait0、两Node实际成功、七ID14次absence、全部双尾/TCP0、2426输入同；业务及15PNG有限视觉最终接受 | `task-intake-web-06-control/result.json` |
| `TestAgentConfigurationApp` | native01两sub18.42s：`default-create-get-lookup-directory-and-assign`9.39s、`lost-create-receipt-and-current-owner`9.03s；runtimeoff公共App TLS、两次joined、每例仅一次Create；四Wait0、七ID14次absence、全部双尾/TCP0、1663输入同，最终独审接受 | `agent-configuration-app-01-control/result.json` |

A复用25个不同前端用例及其有效format/type/build输入；14与16组合的十九race top和后继两包vet按原输入分段接受，不回填pure01容量wholeFAIL。14创建链不代验16 Update PG，也不证明15/17页面已通过浏览器。13前端24项、15前端20项、17前端20项分别按原输入保留，不能累加当作同一次全平台验证。

A普通POST若出现精确ERR_ABORTED，仍保原failed事件；仅在同一原请求的服务端完整长度/SHA及真实提交后验、原body完成/退役、公开typed消费者原Promise fulfilled且身份current三方严格一致时计完整完成。预声明的创建回执截断仍必须进入Unknown，仅原key/fullrequest Lookup；不得以已显示Task或后补GET替代提交证明。真实范围是Current Sprint创建backlog并独立选择真实Agent转todo，不自动StartSprint、启用Scheduler或把todo认作执行完成。

实际入口继续复用原immutable plan/runner和唯一资源窗口，以下是已执行文件，重现须新namespace并重新冻结输入，不能覆盖原轮：

```sh
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/task-intake-runtime-compile-06-launcher.py
AGENTEAM_TASK_INTAKE_WEB_DIST=/workspace/agenteam-task-flow-delivery/output/ai/task-intake-web/frontend-03/dist \
AGENTEAM_TASK_INTAKE_WEB_EVIDENCE=/workspace/agenteam-task-flow-delivery/output/ai/task-intake-web/native-06-evidence \
NODE_DISABLE_COMPILE_CACHE=1 \
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/task-intake-web-launcher-06.py
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/agent-configuration-app-launcher-01.py
```

原A01–05 FAIL及compile05等结果不回填；B01虽原文件为PASS但业务接受已撤回，B02–04 FAIL及复验按独立入口保存。B的最终范围见[显式Runtime装配](../../docs/development/work-items/recovery-project-domain-bindings.md#有限后继显式配置的-central-execution-runtime-装配)，不由上述A/14验收代替。生产Project initializer、完整Loop/工具调用、全局ready及既有STOP保持。

## 显式 App Runtime有限组合

SOURCE `deec8a506b98a77b120446e7e4360740557ac861`的compile07/candidate07与B05已实际wholePASS。`TestAppExecutionRuntime`两sub25.84s：`discovers-existing-project-and-completes`12.34s，`stop-joins-held-model-and-leases`13.50s；实际从原app.Run发现已有initialized Project，经Scheduler/Execution/真实Model到终态，另在held-wire时Stop并等待原调用、双lease与App退出。两个原SystemResolver A/AAAA前验均通过，private DNS run PID与原Go实际Wait身份一致。四Wait0、七ID14次absence、全部private/runtime/desc/TCP双尾空、1665输入首尾同；secret独立最终原件有限验收已接受。

原件为`output/ai/agent-system-integration/app-execution-runtime-05-control/{result.json,supervisor.log}`及PG `/tmp/aer05/pg-2af5cff46f01429d98c3f7a240a4813f.log`。此前B01目标回调未执行而撤回的业务接受不恢复；B02–04及compile05原FAIL继续保留。真实DNS属于本轮有界fixture接线，不改生产Resolver，也不以测试初始化图证明生产Project创建已绑定。

```sh
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/task-intake-runtime-compile-07-launcher.py
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/app-execution-runtime-launcher-05.py
```

上述为已执行入口，不直接复用旧namespace。重现继续按原plan/输入闭包、固定Go/Python与唯一hotcache、fresh容量及七资源实际退出门执行；依赖已初始化Project、真实provider图和显式`AGENTEAM_CENTRAL_EXECUTION_RUNTIME`中的`direct-text-v1`完整配置，不能只传模式字符串替代闭集JSON。准确字段见[Runtime装配](../../docs/development/work-items/recovery-project-domain-bindings.md#有限后继显式配置的-central-execution-runtime-装配)。完整Loop、Tool、Project initializer及ready仍未完成。

## Task编辑与Agent创建页面有限组合

第十三/十五批真实页面验收闭合，已随main `4270a670`交付。共享candidate09来自compile SOURCE `8417da7a0d6de6e7813092b106797ba3491e66df`；13 native02沿同SOURCE运行，15 native03沿runtime SOURCE `7a0224a98c724ade5bab956d3f5e2a1d57948353`运行并复用原Agent dist01。后继只修夹具原请求/进度绑定，不重建历史请求或改变页面产品；具体输入闭包与编译字节/SHA以原compile09记录为准。

| 入口 | 实际有限结果 | 原件（delivery的output/ai/agent-system-integration下） |
| --- | --- | --- |
| `TestTaskEditWeb` | native02两sub81.74s wholePASS；真实changed/clear/No-op及独立Ready，丢编辑回执后仅原完整patch/key Lookup；业务/资源与8PNG有限视觉接受 | `task-edit-web-02-control/result.json` |
| `TestAgentConfigurationWeb` | native03两sub41.82s wholePASS：`default-create-config-and-directory`21.37s、`lost-create-confirmation-only-lookup`20.45s；默认Create1及目录9/config20，另一例Create1真截断后仅原Lookup1；业务/资源与7PNG有限视觉接受 | `agent-configuration-web-03-control/result.json` |

两轮均为四原Wait0、两Node实际成功、七ID14次absence、全部private/runtime/desc/TCP双尾闭合、outer无遗留、2436输入一致。原请求完成仍依同request的服务端长度/SHA与提交后验、原body EOF/退役、公开typed消费者原Promise fulfilled且身份current联合证明；预声明截断保留failed，不能借成功Lookup伪写原mutation成功。独立前端13的24项、15的20项及各自format/type/build按原输入复用，不累加成一次全平台验证。

视觉与交互仅接受上述实际截图及同轮键盘、取消回焦、主题、reduced-motion/overflow范围；13截图下半部和Unknown提示有DOM断言支持，15二十字段未同入一图，不外推全面视觉或无障碍审计。13 native01的Ready联证FAIL、15 native01容量FAIL与native02 Lookup绑定FAIL及其全部原件保留；后继通过不回填。重现沿既有immutable输入/方法和独立namespace，仍须固定依赖、fresh容量、原实际Wait和七资源退出门。

当前只交付[Task内容编辑](../../docs/development/work-items/d11-work-owner-planning-ui.md#13-未指派-backlog-task-内容编辑批-13)与[Agent创建页面](../../docs/development/work-items/d10-agent-configuration.md)。16真实Update PG、17编辑页面、Timeline真实PG/浏览器、Project initializer、完整Loop与ready均不由本轮通过替代；旧STOP保持。

## Task Timeline只读组合

本片已完成限定验收，随本次提交交付main；产品与Web事实锚为SOURCE `b3e6f0bb9981cca668087f0686c5138cd8df6237`。后端14 top/3包vet、UI `b1fdc7d5`的新7加原page9共16项、format10/type/build按原有效输入复用，不将不同轮次累加为一次全平台检查。

| 入口 | 绑定与实际范围 | 原件（metadata树output/ai/agent-system-integration下） |
| --- | --- | --- |
| `TestTaskTimelinePersistenceAndPaging`、`TestTaskTimelineAuthorityAndCancellation`、`TestTaskTimelineIntegrityAndCompatibility` | SOURCE `d407f82a`，三父top/十三sub真实PG全PASS | `task-timeline-pg-01-control/result.json` |
| `TestTaskTimelineWeb` | SOURCE `b3e6f0bb`、compile02/candidate02，native03唯一`recent-older-and-refresh`30.90s wholePASS；正式服务写入51事件，读取最近50、较早1、刷新50，完整评论与只读DB后验通过 | `task-timeline-web-03-control/result.json` |

Web四原Wait0、Node实际成功、七ID14次absence、全部private/runtime/desc/TCP双尾空、outer无遗留、2444输入一致，secret最终业务/资源有限接受。原PW失败事件仍保留，严格同request联合完成证明成立后才计读取完成；不以DOM或后补GET替代原typed消费者与服务端证明。cleanup实际2PNG与原键盘/overflow有限接受，不外推完整无障碍审计或所有事件类型的视觉覆盖。

Web成功candidate02原为`task-timeline-web-race-02.test`，78,241,136 B，SHA256 `10ab714511caab1b69c8da3df36a9cfc86ddf603bf0f9a7ccecf25bbaed5de32`，绑定同目录`task-timeline-web-compile-02/result.json`；compile02/native03全PASS且零consumer后已精确退休raw（actual `8441f4`退出0），source、recipe、plan及results保留，可按原源码配方重建，未保存为gzip；原PG日志`/tmp/tlw03/ui-0e4ab8434d6d4175.log`。恢复继续沿原methods、immutable输入与独立namespace、固定依赖及唯一资源窗口，不直接重用旧namespace。旧candidate01已无损gzip保存，精确compile映射与独占恢复规程见[前置协调记录](../agent-prerequisites/coordination.md#当前批次与活动树)。

原combined-pure-01断言FAIL、02物理缺包零top FAIL、format01授权文件物理缺失FAIL、Web native01容量零业务FAIL及native02夹具比较FAIL均保留。native02的原HTTP200被fixture中JSONB RawMessage空白与compact字节比较误拒为502；后继比较双方原typed数组经正式wire codec序列化的完整内容，保留事件来源、顺序、页边界及错误门，不改产品。真实浏览器仅覆盖本轮51事件分页链，不宣称六类来源的全部真实生产写矩阵、评论编辑、Execution日志、完整Runtime或ready已完成；Agent编辑由后继独立组合验收，见下节。

## Agent配置编辑有限组合

本片已完成限定验收，随本次文档提交交付main。完整SOURCE `510dd29d90fd39a81fff3fabfe4a923d0770b114`的compile02/candidate02绑定987个Go/embed输入；原UI二十项及Timeline十六项结果按各自有效输入复用，新组合八项controls、类型检查与构建通过，使用本组合既有`frontend-01/dist`。本片仅开放当前选中Agent的name、description、instructions显式保存、空串清空与原Lookup，未扩展完整配置权限或运行管理。

| 入口 | 实际有限结果 | 原件（App树output/ai/agent-system-integration下） |
| --- | --- | --- |
| `TestAgentConfigurationUpdateApp` | App03两sub18.77s wholePASS：`changed-noop-and-original-version`9.16s，`current-owner-and-original-update-lookup`9.62s | `agent-update-app-03-control/result.json` |
| `TestAgentEditWeb` | Web01两sub31.39s wholePASS：`changed-noop-and-clear`16.95s，`lost-update-confirmation-only-lookup`14.44s | `agent-edit-web-01-control/result.json` |

App树为`/workspace/agenteam-skill-install-runtime-source`。两轮均四原Wait0、七ID14次absence、private/runtime/desc/TCP双尾空、outer无遗留，App1672/Web2447输入首尾同；Web两Node实际成功，secret最终业务/资源有限接受。App验证原版本changed/no-op与历史原回执Lookup，以及409 VersionConflict/IdempotencyKeyReused、404 Owner、403 CSRF、401 SessionRevoked的零写门。Web正常三次PATCH覆盖changed/no-op/清空，丢回执链一次PATCH后仅原key/version/fullpatch Lookup；原首PATCH的ERR_ABORTED事实保留，由同原请求的typed Promise/current身份与DB/TLS完整完成联合证明闭合，预声明截断仍记失败，不由Lookup改写。cleanup接受本轮六PNG及原键盘、取消回焦、主题、overflow有限范围，1440浅色与390深色不代表二十字段同时入图或全面无障碍审计。

原App01容量FAIL、App02创建目标403且Update未进入的17.09s wholeFAIL及其全部原件保留；后者是错误Host/cookie的fixture接线，原日志没有的具体Problem不补写。candidate01 FAIL与candidate02成功raw当前均保留，没有新退休或gzip。资源窗口另精确回收28个可再生旧Go cache对象548,720,640 B，原记录为`output/ai/agent-system-integration/agent-edit-cache-recovery-01/result.json`，未删除FAIL证据。重现须依原compile02结果、immutable输入/方法、独立namespace、固定依赖、fresh容量及原实际Wait/资源退出门，不覆盖既有结果；原PG分别为`/tmp/aupa03/pg-63f85a7297204f65a1711ce7bb9f955d.log`与`/tmp/aew01/ui-616f76af286c4ae4.log`。完整F1、Project initializer、运行管理、平台ready与既有STOP保持。

## 已有planned Sprint启动页面

本片真实native01已wholePASS，最终业务/资源有限独审接受，随本次文档提交交付main。沿原Work HTTP的Start与原请求Lookup，不新增后端规则或迁移；页面只在当前Human Owner、active Project、Current为空且显式选择已有planned Sprint时提供“启动 Sprint／确认启动 Sprint”。Session冻结原Project、Sprint、正整数字符串version与key；Unknown仅查询原操作，in_progress/not_observed不授权重发。确认回执保留为历史操作事实，页面仍须真实重读Project→同一Sprint→board；若当前指针已变化或重读失败，显示明确状态和只读恢复入口，不再Start。

启动不改变Scheduler启停状态，不自动创建、完成、rollover或删除Sprint，也不启用Scheduler或提升平台ready。公开Project DTO无scheduler_enabled；页面说明“已暂停的调度保持暂停”，真实paused保留已经本轮PG后验验证，不以页面文案代证。切Project/Sprint或身份退出后，原调用仍实际退出，迟到结果不能发布到新页面。

实际入口`^TestSprintStartWeb$`的native01恰两sub56.83s wholePASS：`paused-start-and-current-board`34.67s，`lost-start-confirmation-only-lookup`22.16s。前者经真实公共App启动后重读当前Project/Sprint与看板，后者真实提交后截断回执、保持Unknown且仅查询原操作；本片不以已有TestSprintStartHTTP通过替代浏览器事实。runtime SOURCE `5a8ccdd244d68319972a28834095608db710c0dc`，Go编译SOURCE `20bb7a253aa14fb6f575b0ddad317b57282df487`/candidate02，使用dist02；后继runtime只含JS格式差，Go摘要修复保留原后验门。四原Wait0、两Node实际成功、七ID14次absence、所有private/runtime/desc/TCP双尾闭合、2448输入一致、outer无遗留；SUP另对八个Chrome实际Wait0。正常链恰一次Start、零Lookup，丢回执链恰一次Start、一次cut与一次原Lookup；真实Project/Sprint各version+1、command1、Outbox1，Scheduler原paused配置、零Dispatch/Execution与原Snapshot保持。secret最终业务/资源有限验收接受，原cut失败事件保留，不由Lookup回填原响应。cleanup有限接受本轮五张PNG：1440×960浅色启动确认、390×844深色Unknown与当前看板，以及原Enter、取消回焦、reduced-motion和页面横溢≤1px断言；看板仅首列与邻列入图，不外推七列末端或完整无障碍。

frontend01格式通过，unit为15PASS/1FAIL，唯一page因fixture恢复contextValid后未等Vue解除DOM disabled而未发出Lookup；type另报TS18047，build未启动。两路径窄修仅补显式非null与nextTick/按钮可用/恰一次Lookup断言，frontend02补原失败case、类型和构建全部通过；原15PASS、format10、八项controls按有效输入复用，compile02通过。原frontend01FAIL不回填，compile01是旧输入的通过记录，不是native失败。

执行树为`/workspace/agenteam-skill-install-runtime-source`。原前端记录在`output/ai/sprint-start-web/frontend-{01,02}/`；native01结果为`output/ai/agent-system-integration/sprint-start-web-01-control/result.json`，PG为`/tmp/ssw01/ui-10a2f04e582244ca.log`。原编译及恢复入口为同树`output/ai/agent-system-integration/sprint-start-compile-{01,02}-launcher.py`与对应`result.json`，保留其原candidate路径、bytes/SHA、Go/embed输入和实际Wait。下列为原编译入口；重现须重新分配窗口和独立输出，不直接覆盖已存在结果：

```sh
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3.12 -B output/ai/agent-system-integration/sprint-start-compile-02-launcher.py
```

继续使用原固定依赖、独立namespace、实际输入闭包、fresh容量及七资源/原Wait双尾门；已有FAIL目录不覆盖，两个候选raw当前均保留、没有新压缩或退休；复用须按对应原编译记录核定。资源仅回收原授权25个可再生cache对象417,730,560 B，`sprint-start-cache-recovery-01/result.json`保留前置失败与零删除事实，02记录实际成功；未删除源码、日志、FAIL或候选。正式可见范围见[D11 Sprint启动切片](../../docs/development/work-items/d11-work-owner-planning-ui.md#15-已有planned-sprint的显式启动)。

## Agent模型策略与Sprint创建组合（限定范围）

本组合已完成限定验收，随本次提交交付main。执行树为`/workspace/agenteam-skill-install-runtime-source` / `ai/agent-sprint-configuration-integration`；Agent三产品/三测试、Sprint六产品/六测试及各八path fixture分别保持边界，不混入后继Skills或Milestone源码。Agent runtime为`a4c636f72ff72ddfd111d1fac49b36a76cb7c61e`；Sprint runtime为`38ebc39e3fb9f257dd301846e9a798019a5a59c5`，使用回焦修复后的Sprint `frontend-02/dist`，不将其冒作Agent重验输入。

共享Go编译SOURCE为`293f062b2faf1554056602768ed21f95f256b104`，990个Go/embed输入。原编译记录为执行树`output/ai/agent-system-integration/agent-sprint-configuration-compile-01/result.json`；同目录candidate为`agent-sprint-configuration-race-01.test`，79,094,753 B，SHA256 `8ec3eb35022a4d24284450caed7cce4dd0623c2c57aea1219ca46c153dec8677`。恢复须从该原记录读取输入清单、候选和绑定，不用旧Rename/Sprint Start候选替代。候选当前保留，没有新增压缩或退休；重新运行须分配独立namespace/output和资源窗口，不覆盖原result。

基础frontend01原42项为41PASS/1FAIL，另原Agent Session两项PASS；仅补原失败case后通过，累计44项接受，非一次44项全绿。类型/构建及八项controls通过。Sprint路由重挂载后的标题回焦修复另补15项（12 Sprint＋3 Work，含两项新增remount/Session身份控）、类型和构建；与前44项存在复用，不累加唯一数。Agent02有效输入保持，无重复业务矩阵。

- `^TestAgentModelPolicyWeb$`：native02正常链22.88s wholePASS，两次PATCH使版本1→2→3、零浏览器Lookup；真实配置引用、receipt/event及最终服务端历史原Lookup/SQL/domainGuard到达。原结果为`output/ai/agent-system-integration/agent-model-policy-web-02-control/result.json`。11请求中9finished，另两GET failed原事实保留且严格完成联证通过。
- `^TestSprintCreateWeb$`：native03正常链17.41s wholePASS，一次Create、零浏览器Lookup/transition；新planned sibling、最终历史原Lookup/PG plan-Outbox，以及旧Current/paused/Project/Milestone/Task保持、零Dispatch/Execution和App joined到达。原结果为`output/ai/agent-system-integration/sprint-create-web-03-control/result.json`，PG为`/tmp/scr03/ui-ed204c1faa154bfd.log`。32请求中29finished，另三GET failed保留且严格联证通过。

两轮各四原Wait0、Node成功、七ID14次absence、所有private/runtime/desc/TCP双尾空、outer无遗留，Agent2456/Sprint2455输入不变；SUP各自回收Chrome的Wait另记，不代替四层Wait。secret最终业务/资源有限接受，cleanup分别接受两张实际图及同场景键盘/主题/reduced-motion/overflow边界：Agent为1440浅色/390深色且显式聚焦后Enter；Sprint为1440浅色创建表单/390深色planned结果及新路由标题焦点，窄图仅前列。不宣自动保存回焦、七列全景或完整无障碍，也不以正常链代验真实Unknown native矩阵。

原frontend01 FAIL、Agent native01（30.96s，新增自动回焦断言不符原契约）、Sprint native01（25.97s，标题label错配、零Create/Lookup）及native02（24.79s，Create后路由卸载导致标题回焦丢失、最终联证/后验未到）全部保留。原finished/failed事件不改写。公开范围与原意图规则见[D10模型与审批配置](../../docs/development/work-items/d10-agent-configuration.md#9-已有agent的模型与审批配置编辑限定范围)和[D11 Sprint创建](../../docs/development/work-items/d11-work-owner-planning-ui.md#16-已有milestone下创建planned-sprint限定范围)。两片不新增运行权限，不启用Scheduler，不解除initializer、ready或旧STOP。

四个活跃物理树及第五个原Rename/回焦作者树继续HOLD：执行树保组合/原候选，UI保文档与旧材料，independent的Milestone UI `fa87a985`与metadata的Milestone fixture `f9620068`已保存推送且独审接受、动态未跑；唯一锁定PW、依赖和原FAIL不清理。Skills UI `2ee6e334`及fixture `ebcfb3b8`已保存静审、尚无动态结论。沿原固定依赖、fresh容量、独立namespace、输入绑定和真实Wait/资源双尾门恢复，不新建永久归档。

## Skills目录与首Milestone恢复交付（2026-10-11）

以上旧物理树/HOLD 是当时记录，不作为当前布局。此次从已保存 `fc0fb130` 在 `/workspace/agenteam-skills-milestone-resume` 恢复两个产品切片；产品源码未返修，恢复只修私有依赖路径和两处 Skills fixture 观察/资产问题。正式可见行为见 [Skills UI](../../docs/development/work-items/d10-skills-owner-read-http.md#当前-human-owner-技能目录与详情-ui限定范围)与[首 Milestone](../../docs/development/work-items/d11-work-owner-planning-ui.md#17-空规划结构中创建首个-milestone限定范围)。本结果不解除生产 initializer、ready 或旧 STOP。

基础输入为 `fc0fb130` 的前端产品：精确 13 文件 80/80 单元测试、vue-tsc 与 Vite build 通过。测试文件是 `web/src/tests/` 下 `skill-owner-{client,state,view}`、`milestone-create-{page,state}`、`work-milestone-planning-{client,session}`、`sprint-create-state`、`sprint-start-state`、`work-review-{state,session}`、`work-sprint-lifecycle-session`、`session`（均 `.spec.ts`）。锁定 Vitest Node API 使用 `watch:false, cache:false, maxWorkers:2, configLoader:'native'` 并实际 close；类型命令为 `node web/node_modules/vue-tsc/bin/vue-tsc.js --noEmit --project web/tsconfig.json`，Vite Node API 产物为私有 dist。原件 `output/ai/skills-milestone-resume/frontend/{unit-01,type-01,build-01}.log`；同一 dist 以普通复制提供给两个 profile，未重新构建或改写资产。

恢复版本为 Go 1.27.1、Node 24.19.0、锁定 Playwright 1.56.1、TypeScript 5.9.3、Chromium 151.0.7922.173。两个 npm 根私有 `npm ci`，锁文件不变；driver 已改为本树 `web/node_modules` 和 `tests/account-captcha-web/node_modules`，不再依赖旧物理树。PG17/PG16 精确 digest 与 MinIO 原 release/commit/SHA 沿原 driver 门验证；MinIO SHA 为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，环境恢复原件 `output/ai/skills-milestone-resume/platform/environment.json`。平台单独 `data:` 探针受企业策略拒绝的原 FAIL 保留，不能代作 loopback 产品失败；其四个旧 PID1 zombie 不属于本次 native 后代。

必要路径补差的原八 entry controls 为 7 PASS/1 ERROR；仅更新 inverse 所用旧路径源片段后定向 1 PASS，不合称同轮八项全绿。Skills 恢复 01 的四个 GET 虽 200/finished，仍因 Chromium/PW 未投影 Host/:authority 被原观察门拒绝，wholeFAIL 与完整失败尾保留。修复在真实 App handler 核原 `r.Host` 等于 owned origin，以原 canonical response requestID、method、URL 和 input 绑定实际返回证明；两个原 header Promise 仍 join，Cookie/ID 门不变，可见错误 Host/:authority 拒绝，不合成 header。四个 Host 正反控 race PASS、candidate02 重编/list 通过。独立补差审后恢复 02 业务/退出全通过，但两图品牌破图：私有 server 的 flat-only 门拒绝冻结 dist 内真实 `/assets/brand/` 文件。后继仅白名单四个原品牌 SVG，保其它嵌套/穿越拒绝，五正常/五拒绝控制 race PASS；截图前要求当前主题唯一可见品牌图片完成解码。candidate03 重编/list、两次补差的锁定 TS 语法检查与独立方法审均通过。

| 原轮 | 实际结果与边界 | 原件 |
| --- | --- | --- |
| Milestone 恢复 01 | Go 22.32s PASS；9 请求/8 finished，原 GET failed 经原严格联证。正常创建与取消 Sprint 入口、资源和两图独立有限接受。 | `output/ai/skills-milestone-resume/native-milestone-01/`；`/tmp/smr-m1/ui-1f53fd31968e4960.log`；`output/ai/milestone-create-web/native-resume-01/` |
| Skills 恢复 01 | Go 14.93s FAIL，原 Node/Go/driver/supervisor 与外层工具均退出 1；首个 header 观察门拒绝，未完成读取链。 | `output/ai/skills-milestone-resume/native-skills-01/`；`/tmp/smr-s1/ui-7a3209324dcc4a61.log`；`output/ai/skills-owner-ui/native-resume-01/TestSkillOwnerReadWeb/` |
| Skills 恢复 02 | Go 27.34s PASS；9 请求/9 finished，业务与退出尾通过；两图品牌破图保留，不宣全页视觉接受。 | `output/ai/skills-milestone-resume/native-skills-02/`；`/tmp/smr-s2/ui-2cd23d7b84d7405e.log`；`output/ai/skills-owner-ui/native-resume-02/TestSkillOwnerReadWeb/` |
| Skills 恢复 03 | Go 21.60s PASS；9 请求/8 finished，首次 Session 原 `net::ERR_ABORTED` 保留并由完整原字节/消费/同 requestID 证明联证；四个 Skills 读与两次 observer 退休齐，新浅深两图独立接受。 | `output/ai/skills-milestone-resume/native-skills-03/`；`/tmp/smr-s3/ui-9effa8ddb61841f7.log`；`output/ai/skills-owner-ui/native-resume-03/TestSkillOwnerReadWeb/` |

四轮均收齐原 Node/Go/driver/supervisor 终态和七资源 14 次 absence、private/runtime/后代/HOST_TCP 双尾，输入未变。成功轮外层工具实际退出分别为会话 60059/96099/2864 的 0，失败轮 6168 为 1；它们是执行者直接观察的工具终态，不从文件反推 wrapper PID。每轮 subreaper 都实际 Wait 了四个自己的 Chrome 后代。恢复 03 supervisor 为 112.085s、外层 113.624s；最终独立审查核业务、资源与截图，限定正常链。恢复 01 FAIL、恢复 02 视觉缺口、历史 Skills03/Planning06 等旧 FAIL 都不回填。真实多页、完整撤权和 Unknown 浏览器矩阵未由本次运行证明。

重现继续使用已提交的原 supervisor/driver，不从忽略目录要求恢复新 harness。先在当前树恢复锁定依赖、私有 Go cache，选择全新输出/短 runtime，取得实际资源窗口；同进程启动前检查至少 5 GiB 空闲。编译使用精确 Go、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 CGO_ENABLED=1 GOFLAGS='-mod=readonly -p=2'`，`GOCACHE/GOMODCACHE/GOTMPDIR/XDG_CONFIG_HOME/DOCKER_CONFIG` 指向自有目录：

```sh
/workspace/toolchains/go1.27.1/bin/go test -tags=integration -race -c -o "$OWNED_OUTPUT/milestone-race.test" ./tests/projectvariable
/workspace/toolchains/go1.27.1/bin/go test -tags=integration -race -c -o "$OWNED_OUTPUT/skills-race.test" ./internal/central/app
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py --driver .agent-state/work-owner-http/root_chain_driver.py --binary "$OWNED_OUTPUT/skills-race.test" --run '^TestSkillOwnerReadWeb$' --output "$OWNED_RUNTIME" --root-chain
```

`OWNED_OUTPUT/OWNED_RUNTIME` 由执行者分配，不复用上述历史路径。Skills 设置 `AGENTEAM_SKILL_OWNER_WEB_DIST/EVIDENCE`，Milestone 设置 `AGENTEAM_MILESTONE_CREATE_WEB_DIST/EVIDENCE` 并换对应 binary 与 `^TestMilestoneCreateWeb$`；dist/evidence 各位于原 profile 的 `output/ai/skills-owner-ui/` 或 `output/ai/milestone-create-web/`，evidence 必须未存在，runtime 满足原短路径门。按原入口精确 list 后运行，不放宽 540s 总预算、原 75s HOST_TCP 尾或依赖/input 门。本次具体 env/命令/fresh 容量在各 native 目录 `invocation.json`，编译与控制日志在 `compile-{01,02,03}/`。输入未变的 Milestone 和前端基础结果没有随 Skills fixture 两次补差重复执行。
