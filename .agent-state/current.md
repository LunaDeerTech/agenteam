# Owner 功能组合候选

- 分支：`ai/owner-feature-integration`；本地树提示 `/workspace/agenteam-feature-integration`，正式基线 `origin/main 280a6431`。这是隔离组合候选，尚未交付 main；不继承基线 current 中的旧进程、代理或任务状态。
- root 独占全部 Git；coordination 独占本 current、已接受领域的组装及共享 harness；content 独占默认根装配、config、两个新 Object resolver 文件和对应部署文档。真实 PG/native/browser/socket/hostTCP 窗口由 root 唯一调度，离线编译不得启动 native 测试或 Go telemetry。
- 已保存配置前置 `d0e6edfc`：`internal/central/config/{config.go,config_test.go,knowledge.go,knowledge_test.go}` 与 `internal/central/object/maintenance_owner_resolver{,_test}.go`。由 content 负责验证和后续必需配置兼容；根 app 首片段与必要配置 fixture 已保存 `760c4888`，尚未完成权限/生命周期最小真实验；本 current 的领域组合结论不替代 content 的根接线验收。

## 已组装的 Skills HTTP

- 来源 `1e260d55`：HTTP 产品/测试 10 路径、`tests/skills/owner_http*` 4 路径、OpenAPI/工作卡 2 路径和 `.agent-state/skills-owner-http/` 4 个入口/独验/诊断源，共 20 非共享路径，逐字比较一致。coordination 仅并入 `.agent-state/task-planning-recovery/{pg_only_driver.go,pg_only_supervisor.py}` 与 `.agent-state/work-owner-http/native_driver.go` 的 Skills 精确分支；保留旧 selector、预算、Wait、资源与 TCP 门。
- 可复用的正式有限证据：原 native 3 top/6 sub 完整 PASS、修后作者 PG 4 top/12 sub 完整 PASS，以及独立当前 Session 撤权 GET/HEAD 1 top/2 sub 的 recovery02 完整 PASS。仅接受 Skills Owner 元数据 HTTP adapter；不证明默认 root、真实 Project.Create 初始化链路或 UI。
- 独验 recovery02：冻结 Go candidate `86fd92431a3c928946445873c3b574b05bc55734aecef7d59e8fd7f3abb8a05f`、driver `0e6cf0bd6fbfd28b016066f6d0bec1eb06405d913c883163f6309d63a9cdbf3c` 和 `d4cdbbc0` 同步诊断；outer session 68413 实际 exit 0，Go/driver 实际 Wait 0，两 PG 资源双退役、private/desc/TCP 双空和输入一致齐，supervisor 75.271s。诊断只观察原采样，未改变 75s/双空门。
- 原作者 fixture 未初始化 Account 的 FAIL、独验 recovery01 因 TCP 尾未闭的 whole FAIL 均保留在来源工作卡/必要失败原件。01 没有 row 身份，02 诊断不能回填其原因或升级原结果。

## 本候选最低离线验证

- 107 个原入口控制（含整个旧 supervisor AST / 两 driver 逆投影）及 9 个 TCP 诊断纯控制实际 exit 0；只用明确 OS/child/TCP doubles，不开资源。
- 固定 Go 1.27.1，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`、共享只读 module cache、自有 `output/ai/skills-http-integration/go-build` 与 task-private `config/go/telemetry/mode=off`。Schema Python 为当前实际 Python 3.12 + jsonschema 4.26.0；`AGENTEAM_SKILL_HTTP_NATIVE` 明确移除。每条 Go 阶段同 process 新鲜磁盘均大于 5GiB。
- 精确 8 个 pure/Schema top race PASS：`^TestSkillOwnerHTTP(ReadRoutesAndHEAD|StrictRequestBoundary|BoundInstancesAndAuthentication|FaultAndHEADProblem|BudgetPropagation|ProjectionRejectsMalformedResults|RepresentationBoundAndCancellation|StandardSchema)$`，session 71821 / Go 123252 实际 Wait 0，总 4.927s。未选择 native 或 lifetime top。
- PG driver 与 native driver 仅 build，实际 exit 0；`go test -race -tags=integration -c ./tests/skills` 实际 exit 0/14.386s。新 binary 的作者 exact selector 恰 4 top，独验 exact selector 恰 1 top，均仅 list，原 shell/所有子命令实际 Wait 0（session 89230）。没有在候选重跑 PG/native 场景。
- 保留一次选择范围偏离：此前 session 41659 的命令缺少 `-run`，整 HTTP 包实际 Wait 0/121.795s（包 1.178s），不能充作授权 exact 检查。原 Go 的 native gate 缺省且源码先 require 再 Listen，lifetime 没有服务器；未证其产生 socket，不推断污染。Go 在拟中止前自然退出，未发信号。实际命令和结果仍在忽略的 `pure-01.json/log`；正确范围在 `pure-exact-02.json/log`。
- 可再生编译/list/日志仅在忽略的 `output/ai/skills-http-integration/`，必要 source 在上述正式路径。源码格式与差异检查通过。

## 下一步和仍未闭合范围

- root 保存本批 Skills HTTP、D05/00028、Skills Cleanup 与此 current；以后增量只能并集共享分支和逆投影控制，不能用某领域旧文件覆盖其他消费者。
- D05 `52a42627` 的 16 产品 + 5 unit + 20 integration + 00028/领域卡/5 恢复源共 48 非共享路径已导入；Skills Cleanup 另从 `92cfb069` 导入 11 个领域源（含 2 pure）、5 个 PG 源及入口控制。两个既有 Skill 文件仅各增加 6 行精确 Cleanup 分派，其原 P2 主体不变。保留 main P2 的 `contract/authority.go`、`contract/skill_initialization_test.go`、`skill_initialization_test.go`、`transfer_upload.go`，不覆盖 content 新 resolver。成本限定组合和 D05 history 的 whole PASS 可复用；Skills 历史消费者在 `8cef9252` 入口窄修后的 recovery02 已完整 whole PASS（42162→2a973b outer 0/142.715s，Go/driver Wait 0，1 top/2 sub，七资源/三 private/runtime/desc/TCP 双尾和输入一致齐，无 STOP），00028 消费者门已关闭。原 recovery01 whole FAIL 及 96693 身份未知保留，不代表默认 root/全 participant 已完成。
- Secret 00029/00030 及对应领域增量尚未导入；必须按 00028→00029→00030 连续前缀组合，原有效领域证据不等于 main 装配完成。
- Knowledge 正文 HTTP PG01 原 whole FAIL：两 GET fixture 正文可被正式短正文预读提前释放 lease，与测试 live-lease 假设冲突；content 仅修两处正文大于 64KiB，保原门与原 FAIL，修后 `1e5833bc` PG02 原 4 top/14 sub 已完整 whole PASS，Go/driver/outer 实际 Wait0、七资源/private/runtime/desc/TCP双尾和输入一致齐；记录 `9b9d1e7c`，正文 HTTP adapter 有限接受，其原有效 native 证据复用。
- Work UI recovery11 原 whole FAIL 已完整退出，作者正在定位 observerError；新的独立 Recovery/Authority 两 top 候选就绪但未验，旧 FAIL 不升级。默认根装配/Project 初始化链路由 content 在本树继续，root 再安排最小真实联调。

## D05 / Skills Cleanup 本候选组合检查

- 四 shared 文件按 Skills HTTP、D05 与消费者三个限定增量并集；`.agent-state/owner-feature-integration/entry_union.py` 对当前真实文件的整个来源并集逐字校验，未知改动 fail，再允许各域逆投影。新 `entry-controls.py` 的 199 控实际 exit 0，覆盖六 D05 exact selector、非 exact 拒绝、原七资源双观察、实际输入冻结 AST 的原 if adapter 分支，以及未知预算改动拒绝。组合后的 Skills HTTP 原 107 控、Skills Cleanup 原 6 methods 亦实际 exit 0；9 个 TCP 诊断控制输入未变，复用已通过结果。三份原 D05 cost 控制保留原历史 BASE，未声称它们直接在当前并集运行通过。
- 初次并集离线控制实际 FAIL：两个增量曾因上下文重复分别插入 Skills HTTP 分支及 later-cost helper，导致 D05 / 消费者输入闭包缺项。仅把原 hunk 移回原 if adapter 和 input_paths，原控制随后通过；没有真实资源测试发生，原失败事实不回填。
- 仅选 5 个 Object 测试文件和 2 个 Skill Cleanup pure 文件中的 22 top，三包 race 全部 PASS（session74461，Go137548实际 Wait0，50.563s），无 socket/native server。新对象/Skills integration candidate 均只 race-c（20.784s / 29.511s）；六个 D05 exact 入口各 list0，Cleanup 原两 top+历史一 top 恰3、HTTP作者四+独验一恰5，所有原进程实际 Wait0，outer0。未重跑任何已闭 PG/MinIO/native 组合，也未声称候选重新验过 P2 PG。
- 正式 P2 的两独验源、Object 四关键源和 Project lifecycle 授权继续保留 main 字节。D05/Skills Cleanup 工作卡只增量标有限接受，Skills 设计 §§1–15 保 main，新增已实现的 §16；默认根/完整 participant/Runtime STOP 不升级。
- 本批94路径已由root保存推送 `44b9b311`；该批终态无 Go/compiler 或资源在途。可重建日志仍在忽略的 output；必要 shared、两个新的离线恢复源、两入口控制及领域文档全部普通文件。候选未交付 main。

## Knowledge 正文 HTTP 第四域

- 正文 HTTP 来源 `9b9d1e7c` 的 23 非共享路径已导入：contenthttp 10 Go、Knowledge source/测试 2、PG 5、Schema/卡 2、恢复源 4。22 个非控制输入逐字等于来源；`selector-controls.py` 仅适配组合基线与已共用的输入一致性分支逆投影。原 PG01 whole FAIL 原件仍保留，不回填。
- 共享仅 `pg_only_supervisor.py`、`root_chain_driver.py`、`native_driver.go` 的正文精确增量；`e3145974` 三源基线与 main280 相同。`entry_union.py` 明确增加第四域，只对 native selector/gate 与输入检查两个已知交叠作可审计组合，当前源须逐字符合并集，未知 hunk 失败。Skills 原输入一致性分支保留，正文检查进入其 else；所有原预算、selector、Wait、资源、private/desc/TCP 门保持。
- 本候选正文 157 控、Skills HTTP 107 控、Cleanup 6 methods、D05 并集 200 控全部实际 exit0。一次 Cleanup 字节逆投影因 CONTENT 映射行顺序不一致 FAIL，仅固定在原 observe_root_chain.expected 开头后复核通过；这属于离线组装控制，没有真实业务执行。
- `^TestContent(ProjectionStrictUnionAndByteIdentity|HTTPGETAndHEADSameConsumption|HTTPBoundariesAndOriginalFaults|HTTPActualSchema|QueryCanonicalBounds)$` 明确 5 pure/Schema top race PASS，不选 native/io；native 四域 driver 仅 build0；`tests/knowledge` integration race-c0/9.770s、正文 exact4 list0。session65082 与每个原子命令实际 Wait0，native gates 显式移除，沿自有 offline/cache/telemetry off/新鲜大于5GiB预飞；没有 socket/PG 场景。
- 默认根 content route、必需 Knowledge keyring 影响的外域 fixture 及根最小测试由 content 唯一写；本结果不接受其尚未闭合的真实初始化/权限/生命周期组合。Secret 00029/30 尚未导入，保后续连续前缀责任。
- 本批 23 非共享 + 3 shared + 两组合控制源 + current 共29路径准备保存；当前所有本写域停写，原命令终态齐，无资源在途。
