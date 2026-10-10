# Knowledge / Skills 默认 root 有限组合

2026-10-10：组合树 `649e6ad3` 的首次单 top 真实链 whole PASS。原 Go、driver、outer 均实际返回 0；资源和 TCP 初尾检查全部完成。安全投影见 [root-first-pass.json](root-first-pass.json)。本记录不替代各领域已有的完整验收，也不扩展 Object Runtime 全局 join 停止项。

## 构造与边界

- 唯一 Store、Account、Project Authority、Object Service 和 ProcessGuard；固定 Avatar / Knowledge / SkillRevision 分派。Maintenance 的无 Owner 请求由 Object 自己的 uploads 映射发现，原 Store/Tx 中重读后委托同一个请求与 expected；解析结果不是授权。缺行、坏 kind、未知/未绑定用途均安全拒绝，不尝试其他 provider。
- Project 创建服务在同一个 Skills 实例构造并登记生命周期所有者后构造，Initializer 直接绑定它。Skill 初始化 Audit 仅在 Audit 端使用原 Object facts 与 Project 私有 witness wrapper；Skills 的 ProjectPorts 仍为原 Authority。原 Secret/Variables/Account Audit 路由保留。
- Knowledge metadata、命令、正文与 Skills HTTP 使用同一个已装配领域实例及 AccountBoundary。Knowledge 新必填 `AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING` 使用正式 loader；其全部 current/retained 材料与 Cursor、Secret、Object download、Account 的全部材料独立。无临时随机默认值；部署范例与受影响的实际 Config 输入已跟随。
- 退役顺序保留调用者 Project → Skills → Knowledge → Account/Core。Skill adapter 直接读取 `Service.Joined()`；Knowledge 先 Stop，仅原实际 Drain 成功后记录 join。共享 Object guard 仍受原 producersJoined 门约束，失败/取消/只发 Stop 不构成 join。
- 未绑定 Lease、whole-Project cleanup、Transfer、Execution 和本轮未授权的 Skill cleanup 根入口仍未绑定。没有新增 Project Create HTTP、自动 RecoverCreations、SPA 发布或生产就绪结论；Secret Owner 库组装也不代表 Secret HTTP/F1/默认根已交付。

## 本次真实证明

入口为 `internal/central/app/knowledge_skills_process_test.go` 的唯一 `TestKnowledgeSkillsDefaultRootComposition`，1 top / 0 sub。只观察默认 `run` / `bindAccounts` 的原实例，不替换 authority，不直接写业务 SQL，不另构造领域服务。

1. 原 bootstrap 恢复文件仅在私有 fixture 中用于正式匿名上下文、Login、Authenticate 和 Session CSRF；原 Login response 实际 Close。
2. 原 Project.Service.CreateProject 返回 ready。SQL 只读核原 creation completed、Project initialized、同 creation 的 Skill published 及相同 protected Skill/revision。原流程实际经过 Inspect → Initialize → DiscoverConfirmation → 同 Tx Confirm 与私有 Audit witness。
3. 同 Skills 服务读取唯一 protected 目录和真实对象包，核完整字节、长度和 digest，等待原 PackageReader.Close 与 Joined。
4. 同 Knowledge 服务正式 CreateDocument；默认根实际 HTTP GET/HEAD 返回当前有界正文及相同 representation length。实际 Skills HTTP 路由也返回成功。
5. 同根 Avatar 实际 PUT 安全重编码、GET 完整 JPEG/metadata 对照、DELETE 及后继 404；覆盖原 Object writer/reader maintenance 与 cleanup 路由。实时 reader lease 和 Skill work 零仅作为补充事实，不代替原 Close。
6. 活跃根仍持有原 ProcessGuard 文件锁；原 SIGTERM 后等待 run 实际返回、Project/Skill/Knowledge/Account/producer Joined，并核文件锁可获得和原 process claim stopped。

这些行为在真实 PG17、D05 MinIO 与原七资源 harness 上运行。Go 用例 6.63s；Go PID 179551、driver PID 177729、outer PID 177706 均实际 Wait 0；outer 105.502s。七资源各两轮 absent，私有目录两轮 absent，runtime 两轮 empty，后代两轮 `[]`，host TCP 两轮 delta empty，源码/候选初尾一致。原 log 在 ignored `output/ai/owner-root/root-01/pg-99b75c64414e493c94b63590e299f1b4.log`；安全投影为跟踪材料，恢复所需 test/harness 源均已跟踪。

## 已执行离线检查与独审

- Config 全包 race PASS（session 58758，1.314s），含严格 required、错误不泄漏、单次读取 Account raw 及 Confirmation 自身 retained key 与其他用途冲突负控。既有根相邻 10 top race PASS（1.209s）。
- 连续 00001..00030 组装后，实际 fixture 环境配置负正控、真实领域调用 held Store 退役、调用者/提供者退出顺序、限定路由 4 top / 12 sub race PASS（session 66747，1.446s）；同 session 的 integration race 编译与 exact list 均 0，列表仅目标 1 top。
- 固定 dispatcher 1 top race PASS（session 89138，1.130s）。真实 Knowledge/Skill Authority 在受控 Store 上拒绝，核当前映射重读、原 Tx/context/object/expected locks 以及缺失/未知/未绑定不 fallback。这是拒绝与方法控制，不冒真实授权通过。该新测试首编因 Query 返回值误写 `Rows` 而非 `*Rows` actual 1；只修测试签名后通过。原真人候选的产品与真人源未变，按 root 授权复用，没有为此重编或重跑真实链。
- Skills 作者独立只读审配置/resolver、七项装配断言，以及本次实际组合方法和 dispatcher 控制，均有限接受无 must-fix。共享单 top 入口另经独审及 40 项离线正负控。独审不冒动态结果。

## 可恢复入口

正式共享入口由 coordination 单写：`.agent-state/work-owner-http/root_chain_driver.py` 与 `.agent-state/task-planning-recovery/pg_only_supervisor.py`。精确命令：

```text
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --root-chain \
  --driver <tree>/.agent-state/work-owner-http/root_chain_driver.py \
  --binary <tree>/output/ai/owner-root/root-composition-race-01.test \
  --run '^TestKnowledgeSkillsDefaultRootComposition$' \
  --output <tree>/output/ai/owner-root/<fresh-run>
```

候选在固定 Go 1.27.1 上以 `go test -mod=readonly -p=1 -race -tags=integration -c ./internal/central/app` 编译。固定 MinIO 原 SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，本树自有副本位于 `output/ai/deps-minio/bin/minio`。共享 modules 只读，自有 GOCACHE；不假设 ignored 二进制在恢复后仍存在。

每轮须 root fresh 独占授权；不得自动重试。第一次任何 Go 动作前建立私有 XDG_CONFIG_HOME 的 `go/telemetry/mode=off`，移除 `TEST_TELEMETRY_DIR`、`GO_TELEMETRY_CHILD`、`GO_TELEMETRY_CHILD_UPLOAD`；只设置 `GOTELEMETRY=off` 不足以关闭 Go telemetry。与原运行同一 process 预飞可用空间至少 5 GiB、新空 Docker config，并保留原 Go 6m、root 540s + TERM 60s + KILL 3s、TCP 75s 双空、actual Wait、全部原资源与 inputs 双尾。业务 PASS 不能提前释放窗口。

Knowledge 原 PG01 whole FAIL 及其未打印的 lease count 保持在 [原材料](../knowledge-content-http/pg-first-failure.json)，未由这次根组合覆盖或回填。原正文/native/PG02 已有效证据不重复执行。
