# milestone-edit-ui 已交付材料恢复

## 已交付工作树材料保全

正式主线基准为 `953fc793`，原作者末端 `0405ee3a171783c264d72fa8ceb9896896c45f84`；原树 `/workspace/agenteam-milestone-edit-ui`。此记录只保全现存材料，不重判原 FAIL 或扩大交付范围。Git 与最终 worktree/ref 清理由主线程负责。

全部原任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/milestone-edit-ui](../../output/ai/milestone-edit-ui)，迁移瞬间 inode、权限、uid/gid、mtime 相同，无覆盖。原输出中的日志、FAIL、binary、dist、输入清单、截图与原状态均保持；旧绝对路径不改写，按此映射访问。 原 `web/dist` 同样迁为该输出的 `original-web-dist/`。

原 `/tmp/mew01` 保留；小目录副本为输出 `original-tmp/mew01/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。
原 `/tmp/mew02` 保留；小目录副本为输出 `original-tmp/mew02/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。

### 最小源码恢复

[retired-inputs.patch](retired-inputs.patch) 对应下表第一行。每个版本都独立以命名主线基准开始，不能叠加应用不同版本补丁；只在私有历史恢复树中使用，不覆盖当前产品。其余有界补丁、实际应用目录和原 apply 日志保存在输出 `source-recovery/`。

| 历史输入 | 恢复补丁 | 技术差额文件数 |
| --- | --- | --- |
| `267d31607915909a189df192554853ed1f1e4ff8`（原 compile 与 native01 FAIL） | `compile-native01-267d.patch` | 2 |
| `e34d643fd0e29559cff04bd12ea43b3f3956d300`（原 native02 通过方法） | `native02-e34.patch` | 0 |

所有非空补丁均实际运行 `patch --batch --forward --fuzz=0 -p1`，原进程退出 0、无 fuzz/offset；每个差额文件逐字等于指定历史 Git blob（原版本缺席的文件恢复为缺席）。存在原输入 map 的差额文件亦逐项核对 SHA。没有对整个旧树进行大哈希；未改路径由 Git blob/tree 差额界定，未据此声称对所有运行环境重新校验。详细路径/摘要/映射见 [preservation01.json](../../output/ai/milestone-edit-ui/preservation01.json)。

原作者末端与 main 953fc793 的完整 Git tree 相同。原 native01 FAIL、修后 native02 有限 wholePASS、编译输入/产物、资源原件及两模式图片均保留。

compile-milestone01 与 native-milestone01/02 原 before/after 清单完整保留；本次只核有界源差额与对应 map，不重新运行浏览器或资源门。最终技术从 main 953fc793 恢复；原失败方法仅在历史私有树中应用。

原 owner 已停写且无自有后台；当前 Usage、AgentRunner、Move 无这棵树的源码/binary/dist/cache/node_modules 运行依赖。可再生 `node_modules`/`__pycache__` 未额外复制；后继依赖恢复必须沿固定锁及工具版本。保护的热 Go cache、RO modules 和活动工作树未触及。未运行产品测试、Go、服务或网络，也未删除旧树或 Git 引用；目录名义体积不是实际释放容量。
