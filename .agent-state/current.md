# Project Variables 独立风险验收恢复点

- 独立树 `/workspace/agenteam-project-variables-independent`，分支 `ai/project-variables-independent`，输入基线 `fc6ffd9e5ac3d77850cf723a23d5b8aa8df25877`。本人未参与Variables产品实现，曾有限独审其HTTP/root，不把该静审或作者动态当成本轮独验。
- 唯一新增写域：tests/projectvariable/independent_*、internal/central/app/project_variables_independent_*、tests/process/project_variables_independent_*、必要.agent-state/project-variables-independent/新源码与本文；产品/作者测试/原工具只读。Git及全局真实资源由root；当前仅离线，禁止未经freshgrant启动PG/socket/root。
- 依据正式D10 Project Variables卡§8。独立风险：真实Account拒绝先于body解码；同User新Session与当前撤权；原create/update/delete intent在后续改删及同名新UUID之后仍绑定原receipt；真实完整COMMIT丢回应且确认阶段撤权仍Unknown，自己读原持久事实；默认root真正确认期间Stop/Force/实际Tx及callback退出，真实进程原intent/退出补集。
- 首个源码 `tests/projectvariable/independent_acceptance_test.go` 使用现真实fixture/完整帧proxy作基础设施，独立oracle和直接SQL自己书写。不以作者success字段判断，不把httptest-only称为真实root。
- 暂无本轮动态PASS，源码正离线编译准备。独占output/cache位于output/ai/project-variables-independent；GOMODCACHE共享只读，Go1.27.1/p1/GOMAXPROCS2/GOPROXYoff/GOSUMDBoff。真实root精确selector待实现后报root最小接线，复用既有监督器及原预算，不能另造资源退出框架。
- Skills树停在新恢复3源+card/current可构建freeze，已向root报全部实际race/vet及未验Object/PG/lifecycle门槛；本阶段不写Skills。

- 首独立PG/HTTP源码已可构建freeze：`tests/projectvariable/independent_acceptance_test.go`。72046 `go test -race -mod=readonly -p=1 -tags=integration -c` actualexit0，独立binary `output/ai/project-variables-independent/independent-pg.test`；实际精确list唯一 `TestIndependentProjectVariableHTTPIntentAndCommit`，0行为执行。源码两风险子项：当前Account优先与历史intent/直接SQL；完整COMMIT帧真实提交后在确认拿锁前撤权，原Unknown不冒成功，再新Session原Lookup/显式重放直查一份事实。当前尚未获真实窗口，不能记PASS。
- 下一真实root确认退出改用精确实际cmd+完整帧代理+PG advisory屏障定位writer/confirmation，原生命周期过程由进程真实Wait证明；避免复制一套root或生产装配。需要新独立process top闭合selector后由root增既有root adapter。独立本域两路径freeze交root，继续新process/proxy源码。
