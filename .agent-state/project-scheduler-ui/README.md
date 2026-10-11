# project-scheduler-ui 已交付材料恢复

## 已交付工作树材料保全

正式主线基准为 `6aaa7763`，原作者末端 `dda4538348f06f380a4ad9d31533afddb944ef6c`；原树 `/workspace/agenteam-project-scheduler-ui`。此记录只保全现存材料，不重判原 FAIL 或扩大交付范围。Git 与最终 worktree/ref 清理由主线程负责。

全部原任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/project-scheduler-ui](../../output/ai/project-scheduler-ui)，迁移瞬间 inode、权限、uid/gid、mtime 相同，无覆盖。原输出中的日志、FAIL、binary、dist、输入清单、截图与原状态均保持；旧绝对路径不改写，按此映射访问。

原 `/tmp/psw01` 保留；小目录副本为输出 `original-tmp/psw01/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。
原 `/tmp/psw02` 保留；小目录副本为输出 `original-tmp/psw02/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。

### 最小源码恢复

[retired-inputs.patch](retired-inputs.patch) 对应下表第一行。每个版本都独立以命名主线基准开始，不能叠加应用不同版本补丁；只在私有历史恢复树中使用，不覆盖当前产品。所有存在独有差额的历史补丁均在本目录随 Git 保存；实际应用目录和原 apply 日志仍保存在输出 `source-recovery/`。空差额版本直接由命名 main 基准恢复，不生成额外补丁。

| 历史输入 | 恢复补丁 | 技术差额文件数 |
| --- | --- | --- |
| `bc4787c5`（原最终通过两场景输入） | [retired-inputs.patch](retired-inputs.patch) | 16 |
| `85052f268e5c71c4321d06553e292937609d5f96`（原导航 fixture FAIL 输入） | [retired-inputs-85052f26.patch](retired-inputs-85052f26.patch) | 17 |
| `7c223d08f2393580c676187f692328edf5e0820c`（原 race binary 编译输入） | [retired-inputs-7c223d08.patch](retired-inputs-7c223d08.patch) | 17 |

所有非空补丁均实际运行 `patch --batch --forward --fuzz=0 -p1`，原进程退出 0、无 fuzz/offset；每个差额文件逐字等于指定历史 Git blob（原版本缺席的文件恢复为缺席）。存在原输入 map 的差额文件亦逐项核对 SHA。没有对整个旧树进行大哈希；未改路径由 Git blob/tree 差额界定，未据此声称对所有运行环境重新校验。详细路径/摘要/映射见 [preservation01.json](../../output/ai/project-scheduler-ui/preservation01.json)。

main 6aaa7763 保留同期 Milestone 与 Scheduler 的最终组合行为；原候选的共享 Session、Task/Milestone 源码及原 profile 与主线有历史差额。因此补丁只用于原 fixture 输入恢复，不能整文件覆盖当前产品。各版本在原清单中出现的差额文件均逐字核对其 2438/2439 输入清单，主线组合 guard 的原 merge-controls 也完整保留。

原 native01 导航 fixture wholeFAIL 不改；native02 是既有有限 wholePASS：normal 两 PATCH 后当前 GET，lost 一次真实截断 PATCH、同 key 历史 Lookup 再独立 GET，无重 PATCH。normal 原 GET ERR_ABORTED 与 lost CONTENT_LENGTH_MISMATCH 都保持；两业务场景不代 Runtime 或生产 initializer。

compile→native01 清理一行、native01→02 专属导航/安全诊断差额均通过独立版本补丁恢复；生产 dist-01/binary 原样保存，未重新构建。

原 owner 已停写且无自有后台；当前 Usage、AgentRunner、Move 无这棵树的源码/binary/dist/cache/node_modules 运行依赖。可再生 `node_modules`/`__pycache__` 未额外复制；后继依赖恢复必须沿固定锁及工具版本。保护的热 Go cache、RO modules 和活动工作树未触及。未运行产品测试、Go、服务或网络，也未删除旧树或 Git 引用；目录名义体积不是实际释放容量。

额外历史补丁复制自上述已经实际零 fuzz 应用并核对的原补丁，新增 tracked 文件逐字相同；本次只核保存字节，没有重复恢复验证或运行产品检查。
