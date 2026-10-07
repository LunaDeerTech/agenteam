# 独立 A/B 真实窗口提案（未授权、未运行）

固定 candidate06，复用作者原完整 fixture/dynamic/CGO0/Python+20 官方 schema 数据闭包；独立 overlay 仅映射 18 冻结技术快照及两个私有 integration 源。实际独立 race 图 398 packages /2543 files，extra0，selected model 无 init/TestMain。probes01 首编译仅三处 root.request 缺 nil body，actual1/wait/双空原件保留；probes02 只修这三处，imports/fileset 不变，格式、race compile/vet、exact2 list 均实际0。没有 integration test body 执行。

唯一两个 selector：

- A：`^TestModelProjectOwnerUpdateIndependentAuthorityAndTerminal$`。正式 Bootstrap/Invitation/Redeem/Login 身份与持久 Skills 前置沿作者固定 fixture。独立真实 Audit/Outbox/Project service 构造使用同 Authority、真实 catalog/plan；capture wrapper 原样委派，无返回装饰。实际业务 Event/AppendPlan 的完整锁对照、缺 User/Project/outbox-registration 锁、旧 plan 错配另一个实际 Event；随后真实 Logout 已提交先于原 receipt/plan 访问。pending-final 复用作者固定按 backend PID 的原 COMMIT proxy/helper，独立选择原 writer rollback：parent+Stop 后 Drain 未完成，放行关闭原 backend，HTTP/Service 实际退役，再由原 command 锁串行 lookup 与 unchanged canonical/receipt/Audit/Event/Activity 证明 final 未完成、原 durable plan 留存；不把 proxy socket 关闭本身当 rollback。最后同原 target/key/body/version 重试与重放。
- B：`^TestModelProjectOwnerUpdateIndependentRootAndHistory$`。默认 app.Run 不注入 Store/handler，真实 8192-byte UTF-8 description、当前 v3/历史 v2、仅 update namespace 不泄露 Create receipt、名称 resolve 保留；保留真实 PATCH/lookup 同一响应原字节及安全 metadata 后实际 root.stop/drained。作者新增正常/100ms forced 在途 root 场景按同冻源实轮另行组合复用，本 B 不重复同一 helper，也不把普通 stop 说成在途 forced 覆盖。

执行必须 root 独占窗口授权，且作者全部已授权资源轮实际退役并明确释放后才开始。A PASS、actual wait、精确七资源双清和输入一致后才能执行 B；任何首红先实际 cleanup，停止后继，不自动重跑。两轮分别重新取得当前环境 PID/starttime、daemon 和精确资源 baseline，绝不沿用旧环境 PID 集合。每轮启动前 fresh ≥5GiB，每个精确 top 从 RUN 到 PASS/FAIL（包括 Cleanup）120s，包沿原6m。

driver-v01 保留原 `scripts/test-objects.sh` 七资源链及其实际监控/标签/完整 Mount/TERM/retire_owned/subreaper wait/双清函数。只有 source_input 支持父闭包+有效 overlay，小环境差量固定 GOFLAGS overlay（不重设 HOME），groups 改独立两组、watchdog 对这两组均启用。Go overlay 不改变 os.ReadFile：实际 repo project-owner.json 另外绑定，不能只核 snapshot。

受控 `--check-input-only` 已实际0，3213输入无缺失/漂移，未调用 Docker/启动资源；该检查不替代窗口开始时新鲜空间和 live baseline。原 daemon/PID1 zombie 仅只读记录，任何新集合差量单列，不能声称 task-owned wait 或全机清零。

建议命令先 A，再条件满足后 B：

```
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3 /workspace/scratch/project-owner-update-http-verification/probes02/driver-v01/driver.py --name independent-a01 --group independent-a --frozen /workspace/scratch/project-owner-update-http-verification/probes02/driver-v01/frozen.json --execute-authorized
/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3 /workspace/scratch/project-owner-update-http-verification/probes02/driver-v01/driver.py --name independent-b01 --group independent-b --frozen /workspace/scratch/project-owner-update-http-verification/probes02/driver-v01/frozen.json --execute-authorized
```

B 完成后只对其 `safe-http-evidence/{patch,lookup}.json` 原 bytes 与 source metadata 运行 `validate_body.py`，标准 Draft202012+FormatChecker、本地 refs、禁网络 retrieval；绑定同 candidate/schema/target/run/status/type/length。实际 body 未产生前保持 pending，不代入合成值。报告仅代表此有界产品，不完成整个 D08/D09 或旧停工项。
