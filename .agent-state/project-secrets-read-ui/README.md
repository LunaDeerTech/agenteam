# 已交付 Project Secrets 只读 UI 的恢复材料

正式交付为 main `7e0a123e`，候选末端 `61e8fdaba444b5ee03c96d4059262c5db4ae606f`；两者完整 Git tree 相同（`406f7c4954371c1f1755af09f184d1b02e10aca2`），不是依赖 squash 的祖先关系判断。交付范围及原失败见[专属卡](../../docs/development/work-items/d10-project-secrets-read-ui.md)。原工作树为 `/workspace/agenteam-project-secrets-read-ui`；Git、最终 worktree 与引用清理由 root 负责。

## 本机保全

全部任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/project-secrets-read-ui](../../output/ai/project-secrets-read-ui)，目录 inode、权限、uid/gid 与 mtime 保持；未覆盖现有目标。包含 checks、methods、compile-app01 原 binary/输入、dist-01/02、native01 焦点 FAIL、native02 有限 wholePASS 及容量检查原件。原 `web/dist` 同样无覆盖迁到该目录的 `original-web-dist/`，未重新构建或替换原资产。

`/tmp/psr01` 与 `/tmp/psr02` 原目录仍保留，完整小目录副本位于输出的 `original-tmp/psr01/`、`original-tmp/psr02/`。各5个文件逐字及 mode/mtime 核对相等，保留原日志、request、owned 与原目录结构。证据中的旧绝对路径没有改写；访问迁移材料时按此映射定位。操作与核对见 [preservation01.json](../../output/ai/project-secrets-read-ui/preservation01.json)。

两套 `node_modules` 未额外复制。Usage 已确认使用相同锁文件的物理私有依赖，无运行输入引用旧树；Secrets 作者确认停写且无后台，AgentRunner 与 Move 无旧树运行依赖。后继恢复依赖须沿固定锁文件与原工具版本，不将安装成功等同于重验；热 Go cache、共享只读 modules 和其他活动工作树未修改。表观目录大小不作为实际释放空间承诺。

## 历史失败输入

[retired-inputs.patch](retired-inputs.patch) **只用于恢复 native01 原焦点失败输入**，不应用到当前产品。以 main `7e0a123e` 的源码为基准，仅恢复以下两文件到原 `e74669730516684d906a11bffabbd3a80f734382`：

- `web/src/views/projects/ProjectSecretsView.vue`
- `web/src/tests/project-secrets-view.spec.ts`

补丁已在私有校验目录实际用 `patch --batch --forward --fuzz=0 -p1` 应用，原进程退出0、无 fuzz/offset；两文件逐字等于上述历史提交。校验文件及原输出在 [recovery-check01](../../output/ai/project-secrets-read-ui/recovery-check01/)，精确摘要见 preservation01。最终 native02/交付技术直接从 main `7e0a123e` 恢复，不需要该补丁；`30f4b8e1` 到候选末端仅五份交付文档变化。

原 native01、早期控制/build FAIL、原网络失败终态及有限接受边界均不改变。保全和补丁校验不是产品、浏览器或资源验证的重跑；没有启动服务、Go 编译或网络调用。恢复旧绝对路径需要由 root 在合适的私有工作树中安排，不能覆盖活动任务或把历史失败版本重新交付。
