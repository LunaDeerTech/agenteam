# Owner 功能组合候选

- 分支：`ai/owner-feature-integration`；本地树提示 `/workspace/agenteam-feature-integration`，正式基线 `origin/main 280a6431`。这是隔离组合候选，尚未交付 main；不继承基线 current 中的旧进程、代理或任务状态。
- root 独占全部 Git；coordination 独占本 current、已接受领域的组装及共享 harness；content 独占默认根装配、config、两个新 Object resolver 文件和对应部署文档。真实 PG/native/browser/socket/hostTCP 窗口由 root 唯一调度，离线编译不得启动 native 测试或 Go telemetry。
- 已保存配置前置 `d0e6edfc`：`internal/central/config/{config.go,config_test.go,knowledge.go,knowledge_test.go}` 与 `internal/central/object/maintenance_owner_resolver{,_test}.go`。由 content 负责验证和后续必需配置兼容；根 app 业务构造尚未完成。

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

- root 保存本批 23 个 Skills 路径与此 current；以后增量只能并集共享分支和逆投影控制，不能用某领域旧文件覆盖其他消费者。
- D05 `52a42627` 的 16 产品 + 5 unit + 20 integration + 00028/领域卡/5 恢复源共 48 非共享路径已只读核齐，尚未导入。保留 main P2 的 `contract/authority.go`、`contract/skill_initialization_test.go`、`skill_initialization_test.go`、`transfer_upload.go`，不覆盖 content 新 resolver。成本限定组合和 D05 history 的 whole PASS 可复用；Skills 历史消费者在 `8cef9252` 入口窄修后的 recovery02 已完整 whole PASS（42162→2a973b outer 0/142.715s，Go/driver Wait 0，1 top/2 sub，七资源/三 private/runtime/desc/TCP 双尾和输入一致齐，无 STOP），00028 消费者门已关闭。原 recovery01 whole FAIL 及 96693 身份未知保留，不代表默认 root/全 participant 已完成。
- Secret 00029/00030 及对应领域增量尚未导入；必须按 00028→00029→00030 连续前缀组合，原有效领域证据不等于 main 装配完成。
- Knowledge 正文 HTTP PG01 原 whole FAIL：两 GET fixture 正文可被正式短正文预读提前释放 lease，与测试 live-lease 假设冲突；content 仅修两处正文大于 64KiB，保原门与原 FAIL，等待修后候选和真实窗口。未接受该 HTTP；其原有效 native 证据保留。
- Work UI recovery11 和新的独立 Recovery/Authority 探针仍未通过；旧 FAIL 不升级。默认根装配/Project 初始化链路由 content 在本树继续，root 再安排最小真实联调。
