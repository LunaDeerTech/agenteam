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
