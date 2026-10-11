# knowledge-text-ui 已交付材料恢复

## 已交付工作树材料保全

正式主线基准为 `bd1c8b5f`，原作者末端 `c43ea957712506ba4d2206d97eb83624c0f641f9`；原树 `/workspace/agenteam-knowledge-text-ui`。此记录只保全现存材料，不重判原 FAIL 或扩大交付范围。Git 与最终 worktree/ref 清理由主线程负责。

全部原任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/knowledge-text-ui](../../output/ai/knowledge-text-ui)，迁移瞬间 inode、权限、uid/gid、mtime 相同，无覆盖。原输出中的日志、FAIL、binary、dist、输入清单、截图与原状态均保持；旧绝对路径不改写，按此映射访问。

原 `/tmp/ktw01` 保留；小目录副本为输出 `original-tmp/ktw01/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。
原 `/tmp/ktw02` 保留；小目录副本为输出 `original-tmp/ktw02/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。
原 `/tmp/ktw03` 保留；小目录副本为输出 `original-tmp/ktw03/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。

### 最小源码恢复

[retired-inputs.patch](retired-inputs.patch) 对应下表第一行。每个版本都独立以命名主线基准开始，不能叠加应用不同版本补丁；只在私有历史恢复树中使用，不覆盖当前产品。其余有界补丁、实际应用目录和原 apply 日志保存在输出 `source-recovery/`。

| 历史输入 | 恢复补丁 | 技术差额文件数 |
| --- | --- | --- |
| `34135408`（oracle 修复后早期方法） | `native01-method-341.patch` | 2 |
| `9382b447`（原产品 checkpoint；含原 oracle 修复前方法） | `product-938.patch` | 3 |
| `bbcfb296`（窄屏 Drawer 修复前原 FAIL 方法） | `native02-method-bbcf.patch` | 1 |
| `ef1c8b47`（最终通过方法） | `native03-method-ef1.patch` | 0 |

所有非空补丁均实际运行 `patch --batch --forward --fuzz=0 -p1`，原进程退出 0、无 fuzz/offset；每个差额文件逐字等于指定历史 Git blob（原版本缺席的文件恢复为缺席）。存在原输入 map 的差额文件亦逐项核对 SHA。没有对整个旧树进行大哈希；未改路径由 Git blob/tree 差额界定，未据此声称对所有运行环境重新校验。详细路径/摘要/映射见 [preservation01.json](../../output/ai/knowledge-text-ui/preservation01.json)。

原 native01 双 Session 资产与 native02 窄屏 fixture FAIL、早期控制失败完整保留；native03 是既有有限 wholePASS（28 请求中 23 finished、5 原 GET aborted，4 写、browser Lookup 0），本次不重验。

原 native01/02 invocation 未保存完整 source map，不能回溯证明从未记录的输入，也不声称完整重验可复现。现存 build 的 283 源文件/74 dist SHA、聚合 hash、原命令、binary 与全部资产原样保留；上列 341/938/bbcf/ef1 已知独有技术版本均已恢复，未补造清单。最终 runtime 技术由 main bd1c8b5f 取得。

原命令引用的旧 KnowledgeHTTP 冷 cache 已在先前独立容量恢复中移除；它不是现有运行依赖。本次保全不重建或修改任何共享 cache。

原 owner 已停写且无自有后台；当前 Usage、AgentRunner、Move 无这棵树的源码/binary/dist/cache/node_modules 运行依赖。可再生 `node_modules`/`__pycache__` 未额外复制；后继依赖恢复必须沿固定锁及工具版本。保护的热 Go cache、RO modules 和活动工作树未触及。未运行产品测试、Go、服务或网络，也未删除旧树或 Git 引用；目录名义体积不是实际释放容量。
