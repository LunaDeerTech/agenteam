# 当前有限组合

- 写域：coordination 仅 `tests/projectvariable/agent_configuration_schema_test.go` 和本目录两份记录；content 写独立构造 helper，root 负责所有 Git。组合基线 main `7a693cb6` 加四组精确源，生产/迁移不由此测试修改。
- 新 `TestAgentConfigurationSchema` 已形成 1 top/3 sub 源码，覆盖真实连续迁移/重跑、00031 旧事实升级、Audit CHECK 回滚探针、refs/head 拒绝与固定真实构造中的 metadata 同 Tx 正向；Registry 配置及组合 NewService 缺实际 install Backend 均须明确 unbound，不冒完整 Create。
- 测试与本目录记录已停写：gofmt、diff whitespace 和静态 1 top/3直接sub 形状检查通过。尚未 Go 编译/list、PG 或任何资源运行；既有 root-chain 的新 selector/input 接线尚未准备。格式检查不证明 SQL 或业务。
- 下一步冻结测试及构造 helper，root 保存后安排必要候选编译/方法审与原入口接线，再另授首次真实窗口；00032–00035 成功不外推 00036、完整 F1、生产 initializer、participant/Runtime 或 E01。
