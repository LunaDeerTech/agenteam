# knowledge-title-search-ui 原验收源码差额

此目录只保留删除已交任务分支后仍需远端恢复的文本差额，不是当前产品变更。恢复基准为 main 历史提交 `1d41132a7cd3d3b29e6efd30ac4e95067d1ce182`；不要向最新工作树直接应用。

由协调者准备该历史提交的隔离 checkout，再在其根目录执行：

```sh
patch --batch --forward --fuzz=0 -p1 < /path/to/retired-inputs.patch
```

补丁涉及以下原相对路径：

- `.agent-state/task-planning-recovery/pg_only_supervisor.py`
- `.agent-state/work-owner-http/root_chain_driver.py`
- `web/src/api/client.ts`
- `web/src/composables/useProjectTasks.ts`
- `web/src/composables/useSession.ts`
- `web/src/views/projects/ProjectTasksView.vue`

用途是恢复原 `knowledge-title-search-ui/native01/input-hashes.json` 中的受控输入；对应旧树前缀为 `/workspace/agenteam-knowledge-title-search-ui`。其余所需源码可从上述 main 历史恢复。原本地原件、FAIL、日志、binary/dist 与私有依赖仍在 main `output/ai/knowledge-title-search-ui/` 及其保全说明指定的位置；补丁不携带这些可再生或本机原件，也不改写原绝对路径记录。

已在仅含必要基准文件的私有临时目录实际运行上述 `patch`（exit 0、fuzz 0、无 offset），应用后逐字等已保全源码，并逐项核对原验收 manifest 的 SHA。临时目录已退出并移除；只验证文本恢复，没有重跑产品测试或补造旧验收。构建/运行仍须按原调用材料恢复外部工具和依赖，历史通过不自动适用于新环境。

## 本地保全副本的压缩恢复

2026-10-12，`output/ai/knowledge-title-search-ui/retained-inputs` 的 8,199 个条目已完整归档为同级 `retained-inputs-archive01/retained-inputs.tar.gz`（SHA-256 `b5966ee7f164b19c5c30e00c744318e354081b47483c269ea8a7ca535c868fcb`）。逐成员内容、链接目标、权限、属主及纳秒 mtime 与归档前后清单核合后，按独立授权仅释放该原副本；`release01/` 保存逐项删除及实际退出记录。原 `PRESERVATION.md`、其他 FAIL/PASS、日志、binary/dist 与上述文本恢复补丁保持。

恢复须按本机 [RECOVER.md](../../output/ai/knowledge-title-search-ui/retained-inputs-archive01/RECOVER.md) 写入新的排他目录并逐项核验；原 dev/inode 保存在清单中，恢复会产生新 inode。归档位于 Git 忽略目录，跨设备需另行传送并核对上述 SHA；这次压缩没有重跑验收，也不证明 Projection 的原 ENOSPC 已解决。
