# B02 当前 main 装配独立复核

Skills 未参与 Knowledge 产品或本次共享源整合，只读 `/workspace/agenteam-knowledge-delivery`。输入为 main `e94077eb` 加 root 精确导入的 44 路径、Work 冻结的 Project `audit_facts.go`、`audit_facts_test.go`、`events.go`、`initialization_audit_test.go`。44 路径中两独立测试来自 `924d5627`，其余来自作者 `aaa408c8`；`run.py` 直接比对正式 Git 来源，不生成闭包清单或源码副本。实际 `7bf180` 及本轮 `33965` 均核等。

结论：**有限 Service 装配接受，无本范围 mustfix，未发现需要新增 PG 轮次的生产闭包差异。** 这是既有固定真实证据与新 main 共享分派的组合结论，不是本树重新运行完整业务矩阵。HTTP/UI、D13、Agent destructive、完整 Project 生命周期、生产 root 和停止中的 Object Runtime join 均不在接受范围。

## 本次共享边界

- 逆去 Knowledge/Object allowlist 与两精确委托后，Audit 原分支逐字 main；Knowledge checker 逐字已验作者源。事件只新增 Knowledge Discover/Validate，Model、Work、ProjectVariable 与 Project 原分支逐字保留。Main Audit、初始化 wrapper 和相邻权限实现没有修改；旧业务闭包中的 Foundation/Identity/Postgres/Account/Object/Outbox 与作者固定版本无生产差异。新 main 的 Variable/Secret Audit 形状仅增量，不替代写入权威。
- 初始化测试只修 Human/AgentRun 两个普通 Object 委托子例。原 EX-only fixture 在新普通 Object 的 SH/current gate 提前 Fatal，故作者首 `837326` 整体 FAIL 保留；没有绕过新 gate 或改变 Service 初始化 EX。修后作者 Project 整包 `64574/046714` race 通过，Knowledge 首轮包 PASS 复用，事件 Valid `9900/4a4ff9`、三包 vet `93419/b9c6d5` 和修后 Project vet `23472e` 均实际通过。作者相邻 Secret 公开形状拒绝测试在全部四 provider 配置时仍要求零 provider 调用。
- 新独立控制不使用作者修正后的委托测试作断言。实际 `33965`、终态 `3102cb` exit 0，race **1.032s，4 top/11 sub**：四 Audit provider 同时存在且精确分派，原 context/Tx/entry/key/CommitUnknown 与 cause 保留；错 producer、未初始化/Archived 在 provider 前拒绝。Model/Work/Variable/Knowledge 的双阶段事件计划不能跨域复用，单改 producer 不能伪造原 typed triple。真实 Knowledge/Object fact checker 在缺私有 witness 时拒绝合法公开 entry。普通 Human/AgentRun Object 先 SH/current gate；Service 初始化仍走原 EX/creation/provider 且不能借普通四 provider。
- 测试只用受控 Store/Session，实际执行 Project 分派、Knowledge/Object 私有凭证检查与 Foundation 类型，不把受控权限行称为 PG、Login/Create 或生产组合。控制前后共享四源逐字不变；原 Service 初始化与其余委托测试的字节保留另行核验。

## 复用的完整有限业务证据

既有 15 生产源/00025/Project 权威风险审查及 P1/P2 修复独验见 [原独审记录](../knowledge-b02-review/README.md)。P1/P2 `15866` 的 4 top/7 sub 已接受；原失败不回填。当前作者固定 10 top/35 direct sub（含 Runtime、Cleanup、Unknown、Concurrency）和单独 Process `50756` 原生 SIGKILL/Wait/Guard/recovery 证据闭合各自范围。

本次实际读取当前 Runtime `57974` 日志 `pg-0a7b9115f922479997dc166ee227af3b.log`（3 sub/7.82s、terminal 0/104.995s）、组合 `91700` 日志 `pg-04554d791ca64ac680bc6498b1ccc816.log` 中四通过组/14 sub 与完整失败终态、修后 Process `50756` 日志 `pg-d776f88b697049b1853a3f24616ca380.log`（2.40s、terminal 0/92.535s）。它们均在作者 `output/ai/knowledge/pg/`。`91700` 整体 FAIL 仍是 FAIL；其 Process 原失败由新固定测试 `50756` 另证，不能反向改写原轮。

独立真实补集为 `89530` 未变的五子 + `82746` 修复末子：DOCX/原 length 与 SHA、send 后 final 当前权限撤销、缺 Audit 私有凭证及原 command Event 拒绝、正文更新不覆盖并发 Move、preview 成员交换旧 scope 失效，以及旧公开持久 receipt 身份/旧 attachment 的真实阳性与撤销拒绝。`82746` exact 一父一子、Go/driver actual Wait 0、outer `c5f341` exit 0、七资源双退役/private/runtime/desc/TCP/input 全尾齐；结果保存于独立树 `458b943a`。`89530` 原整体 FAIL、`62452` 发现阶段 FAIL 和当时未到断言保持，不把公开构造的持久 identity 称为 API 曾返回的 receipt。

以上依赖与技术输入未变，可复用原实际结果。无需为纯分派合并重复作者 11 top 或独立六组；若 root 后续改变生产闭包，应仅重验具体受影响门。

## 复现本次离线控制

```sh
# cwd: /workspace/agenteam-skills
python3 .agent-state/knowledge-delivery-review/run.py
```

脚本固定 Go `/workspace/toolchains/go1.27.1/bin/go`，继承 PATH 前置 Go；`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOFLAGS=-mod=readonly`，`GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，独占 `GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`，`GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp`。临时 overlay 仅增加本目录 `shared_test.go`，执行 `go test -race -count=1 -p=1 -timeout=30s -run=^TestIndependentKnowledgeDelivery ./internal/central/project`。没有 integration、PG、Docker、socket、网络、产品写入或新大缓存。
