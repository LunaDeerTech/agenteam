# Project Variables 独立风险验收恢复点

- 独立树 `/workspace/agenteam-project-variables-independent`，分支 `ai/project-variables-independent`，输入基线 `fc6ffd9e5ac3d77850cf723a23d5b8aa8df25877`。本人未参与Variables产品实现，曾有限独审其HTTP/root，不把该静审或作者动态当成本轮独验。
- 唯一新增写域：tests/projectvariable/independent_*、internal/central/app/project_variables_independent_*、tests/process/project_variables_independent_*、必要.agent-state/project-variables-independent/新源码与本文；产品/作者测试只读。root另授权仅本树既有root_chain_driver.py增加独立process的一个精确selector映射，其余工具不改。Git及全局真实资源由root；当前仅离线，禁止未经freshgrant启动PG/socket/root。
- 依据正式D10 Project Variables卡§8。独立风险：真实Account拒绝先于body解码；同User新Session与当前撤权；原create/update/delete intent在后续改删及同名新UUID之后仍绑定原receipt；真实完整COMMIT丢回应且确认阶段撤权仍Unknown，自己读原持久事实；默认root真正确认期间Stop/Force/实际Tx及callback退出，真实进程原intent/退出补集。
- 首个源码 `tests/projectvariable/independent_acceptance_test.go` 使用现真实fixture/完整帧proxy作基础设施，独立oracle和直接SQL自己书写。不以作者success字段判断，不把httptest-only称为真实root。
- 暂无本轮动态PASS。独占output/cache位于output/ai/project-variables-independent；GOMODCACHE共享只读，Go1.27.1/p1/GOMAXPROCS2/GOPROXYoff/GOSUMDBoff。真实root精确selector复用既有监督器及原预算，不能另造资源退出框架。
- Skills新恢复3源+card/current已由root实际保存推送cfb82d9d，全部恢复片段可恢复，未验Object/PG/lifecycle及原Object停止项保留；本阶段不写Skills。

- 首独立PG/HTTP源码已可构建freeze：`tests/projectvariable/independent_acceptance_test.go`。72046 `go test -race -mod=readonly -p=1 -tags=integration -c` actualexit0，独立binary `output/ai/project-variables-independent/independent-pg.test`；实际精确list唯一 `TestIndependentProjectVariableHTTPIntentAndCommit`，0行为执行。源码两风险子项：当前Account优先与历史intent/直接SQL；完整COMMIT帧真实提交后在确认拿锁前撤权，原Unknown不冒成功，再新Session原Lookup/显式重放直查一份事实。当前尚未获真实窗口，不能记PASS。
- 首独立PG实际执行cwd为本树tests/projectvariable。复用既有pg_only_supervisor.py、私有pg-only-driver与independent-pg.test，精确selector `^TestIndependentProjectVariableHTTPIntentAndCommit$`；仍原105+15、123+3、75s TCP、5GiB与两资源预算。driver63851实际build0；未启动真实资源。
- 第二片段为tests/process/project_variables_independent_confirmation_test.go与.agent-state/project-variables-independent/commitproxy/{proxy.go,proxy_test.go}。用实际cmd+完整Q COMMIT帧代理+真实PG advisory屏障独立定位writer及确认PID，SIGTERM后实际Wait，代理之后才放行原上游提交，重启同库核原Lookup/显式重放及直接SQL一份事实。披露Project前置复用持久fixture Skills，非生产Skills/root创建发布；本top覆盖确认期间真实进程退出，不冒Force回调已验。
- process首次39687编译因独立测试误用不存在Cause.Canonical而setupFAIL；修本probe刺激key后39350 race-c实际0，binary为output/ai/project-variables-independent/independent-process.test。5578实际TestMain离线重建两cmd并精确list0，唯一TestIndependentProjectVariablesProcessConfirmationExit，无body执行。代理内存帧/短写/实际join控制10664 race实际0（三top/五child），只net.Pipe不启kernel socket，不当真实COMMIT证据。
- root_chain_driver.py仅增 `^TestIndependentProjectVariablesProcessConfirmationExit$` → tests/process。实际AST删除新映射后同0aa71901；6闭集正例、5非法selector拒绝、7resources/6m和固定MinIO SHA控制实际0，原root540+60+3+75及资源流程不变。该新映射待根安排未参与者窄审。
- 作者Concurrency实际发现跨Project全局ID竞争loser被D03 poisoned transaction归为INTERNAL_ERROR。作者最小Create主键ON CONFLICT DO NOTHING/0row NOT_FOUND修复已获另一实例有限独审，当前本树尚未同步，不修改其原红证据。root同步精确产品源后必须重编本独验PG/process输入，旧fc6二进制不作为修后最终验收。
