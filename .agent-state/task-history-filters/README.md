# task-history-filters 原验收源码差额

此目录只保留删除已交任务分支后仍需远端恢复的文本差额，不是当前产品变更。恢复基准为 main 历史提交 `4cb67e596505af63f74bc2c360532daa8c2700a4`；不要向最新工作树直接应用。

由协调者准备该历史提交的隔离 checkout，再在其根目录执行：

```sh
patch --batch --forward --fuzz=0 -p1 < /path/to/retired-inputs.patch
```

补丁涉及以下原相对路径：

- `tests/projectvariable/agent_edit_web_test.go`
- `web/src/api/agent-configuration.ts`
- `web/src/components/agents/AgentEditForm.vue`
- `web/src/composables/useProjectAgents.ts`

用途是恢复原 `task-timeline-web/native01/input-hashes.json` 中的受控输入；对应旧树前缀为 `/workspace/agenteam-task-history-filters`。其余所需源码可从上述 main 历史恢复。原本地原件、FAIL、日志、binary/dist 与私有依赖仍在 main `output/ai/task-history-filters/` 及其保全说明指定的位置；补丁不携带这些可再生或本机原件，也不改写原绝对路径记录。

已在仅含必要基准文件的私有临时目录实际运行上述 `patch`（exit 0、fuzz 0、无 offset），应用后逐字等已保全源码，并逐项核对原验收 manifest 的 SHA。临时目录已退出并移除；只验证文本恢复，没有重跑产品测试或补造旧验收。构建/运行仍须按原调用材料恢复外部工具和依赖，历史通过不自动适用于新环境。
