# Registry 配置前置当前恢复点

- 树：`/workspace/agenteam-tool-registry`，分支 `ai/tool-registry`，基线 `728cd45a`。本树唯一实现者 cleanup，所有 Git 操作归 root。
- 首四路径已保存 `1d60e34f`，目录/Agent 引用及九项必要纯控已保存 `51d4b299`。当前窄修独审发现的 jsonb 数值规范化与孤立 surrogate 替换两问题：canonical bytea 持久化/完整回读、严格转义检查和对应控制；原静态问题事实保留。
- 范围与工程契约见 [D18 卡 §10](../docs/development/work-items/d18-tool-name-projection.md#10-registry-配置前置实施中)。Registry 仅 Builtin metadata/current/配置目录及同 Tx Agent refs；无安装 Backend 时 production install-skill 未绑定。NameTable、执行器、既有 tools STOP 不变。
- Agent owner content 的 consumer-owned Tool contract 四文件已由 root 按 `34755aa5` 同步；Registry 未修改借用源。00032 归 Agent，00033 唯一归本树；真实迁移必须连续组合。
- 本轮仅读源码、落实现、gofmt/diff 检查；未 Go 编译/测试、未 PG/socket/实际资源。纯测试替身不证明真实 Backend、授权、迁移或 F1 完成。
- 后继：窄修冻结给 skills_http 复核；root 保存并授权后只九项新 pure race 一次和两包 vet，再按新窗口组织真实 PG/Agent 组合。当前没有在途自有进程或资源。
