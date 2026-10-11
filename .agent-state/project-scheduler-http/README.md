# project-scheduler-http 已交付材料恢复

## 已交付工作树材料保全

正式主线基准为 `6aaa7763`，原作者末端 `bca1906218c74aefba137ce9639be56dc767eda2`；原树 `/workspace/agenteam-project-scheduler-http`。此记录只保全现存材料，不重判原 FAIL 或扩大交付范围。Git 与最终 worktree/ref 清理由主线程负责。

全部原任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/project-scheduler-http](../../output/ai/project-scheduler-http)，迁移瞬间 inode、权限、uid/gid、mtime 相同，无覆盖。原输出中的日志、FAIL、binary、dist、输入清单、截图与原状态均保持；旧绝对路径不改写，按此映射访问。

原 `/tmp/psh01` 保留；小目录副本为输出 `original-tmp/psh01/`，4 个文件逐字及 mode/uid/gid/mtime 核对相等。

### 最小源码恢复

[retired-inputs.patch](retired-inputs.patch) 对应下表第一行。每个版本都独立以命名主线基准开始，不能叠加应用不同版本补丁；只在私有历史恢复树中使用，不覆盖当前产品。其余有界补丁、实际应用目录和原 apply 日志保存在输出 `source-recovery/`。

| 历史输入 | 恢复补丁 | 技术差额文件数 |
| --- | --- | --- |
| `1381eb773bd45e4a7a04f996d7488c0e397e13c1`（最终原 PG 方法与编译输入） | `native-compile-1381.patch` | 4 |
| `59f6bad5`（原 unit01/02 记录引用的已知源码基准） | `unit-initial-59f6.patch` | 9 |
| `ce6da768`（原 unit03/vet 引用的已知源码基准） | `unit-contract-ce6.patch` | 9 |
| `61d5bcf1`（原 compile 记录的已知基准（新 profile 见 1381）） | `compile-base-61d5.patch` | 4 |

所有非空补丁均实际运行 `patch --batch --forward --fuzz=0 -p1`，原进程退出 0、无 fuzz/offset；每个差额文件逐字等于指定历史 Git blob（原版本缺席的文件恢复为缺席）。存在原输入 map 的差额文件亦逐项核对 SHA。没有对整个旧树进行大哈希；未改路径由 Git blob/tree 差额界定，未据此声称对所有运行环境重新校验。详细路径/摘要/映射见 [preservation01.json](../../output/ai/project-scheduler-http/preservation01.json)。

已交付后端产品从 main 6aaa7763 取得；补丁仅恢复本片 backend Go/schema/原 harness 与输入控制的历史差额，不倒退主线前端。主线后继新增而原 Go 输入缺席的测试文件恢复为缺席。

原 native01 自有 TLS/Account/Project/PG 两子例有限 wholePASS，原 unit01/02 FAIL、unit03/vet、compile/list 全部保留；不把该 PG 轮称生产 initializer 或浏览器验收。原 1703 编译输入与 1704 运行输入仍在，1381 补丁差额逐文件与两清单相符。

早期 unit invocation 明确记有 “checkpoint plus” 未提交修复/新 schema_test.go，未保存其完整逐文件清单。本次保全对应已知提交和全部现存日志，不回填未记录字节，也不声称早期 unit 完整输入可重新证明；最终已冻结 1381 输入不受此限制。

原 owner 已停写且无自有后台；当前 Usage、AgentRunner、Move 无这棵树的源码/binary/dist/cache/node_modules 运行依赖。可再生 `node_modules`/`__pycache__` 未额外复制；后继依赖恢复必须沿固定锁及工具版本。保护的热 Go cache、RO modules 和活动工作树未触及。未运行产品测试、Go、服务或网络，也未删除旧树或 Git 引用；目录名义体积不是实际释放容量。
