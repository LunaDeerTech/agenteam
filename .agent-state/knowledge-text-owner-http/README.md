# knowledge-text-owner-http 已交付材料恢复

## 已交付工作树材料保全

正式主线基准为 `6cbca34c`，原作者末端 `7e3ea3100dec8a33bc85be8074c215678a1e66e9`；原树 `/workspace/agenteam-knowledge-text-owner-http`。此记录只保全现存材料，不重判原 FAIL 或扩大交付范围。Git 与最终 worktree/ref 清理由主线程负责。

全部原任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/knowledge-text-owner-http](../../output/ai/knowledge-text-owner-http)，迁移瞬间 inode、权限、uid/gid、mtime 相同，无覆盖。原输出中的日志、FAIL、binary、dist、输入清单、截图与原状态均保持；旧绝对路径不改写，按此映射访问。


### 最小源码恢复

[retired-inputs.patch](retired-inputs.patch) 对应下表第一行。每个版本都独立以命名主线基准开始，不能叠加应用不同版本补丁；只在私有历史恢复树中使用，不覆盖当前产品。其余有界补丁、实际应用目录和原 apply 日志保存在输出 `source-recovery/`。

| 历史输入 | 恢复补丁 | 技术差额文件数 |
| --- | --- | --- |
| `d74df012`（真实 PG 与默认 App 原最终方法） | `native-final-d74.patch` | 6 |
| `567d591e`（原早期实现与上限/断言修复前源码） | `initial-567.patch` | 10 |

所有非空补丁均实际运行 `patch --batch --forward --fuzz=0 -p1`，原进程退出 0、无 fuzz/offset；每个差额文件逐字等于指定历史 Git blob（原版本缺席的文件恢复为缺席）。存在原输入 map 的差额文件亦逐项核对 SHA。没有对整个旧树进行大哈希；未改路径由 Git blob/tree 差额界定，未据此声称对所有运行环境重新校验。详细路径/摘要/映射见 [preservation01.json](../../output/ai/knowledge-text-owner-http/preservation01.json)。

已交付后端产品从 main 6cbca34c 取得；补丁仅恢复本片 backend Go/schema/原 harness 与输入控制的历史差额，不复制整个旧前端。主线后继新增而原 Go 输入缺席的测试文件恢复为缺席。

原 native-pg01 与 native-app01 的日志/owned/request 全在原 task output 内，没有另行遗漏的 /tmp 原目录。两条原 wholePASS、unit-01 ObjectID 编译 FAIL、unit-02 Unknown 断言 FAIL、unit-03/vet 及两 race binary 原样保留。App 范围仍为进程内真实 run/listener 与 signal channel SIGTERM，不代外部 OS 信号、真实 Unknown/撤权竞争。

原命令记录固定 d74df012，但未保存完整逐文件 runtime source map；现存已知 567/d74 源差额与命令、日志、聚合校验不变。不补造缺失清单，不声称未记录的早期输入可回溯证明。

原 owner 已停写且无自有后台；当前 Usage、AgentRunner、Move 无这棵树的源码/binary/dist/cache/node_modules 运行依赖。可再生 `node_modules`/`__pycache__` 未额外复制；后继依赖恢复必须沿固定锁及工具版本。保护的热 Go cache、RO modules 和活动工作树未触及。未运行产品测试、Go、服务或网络，也未删除旧树或 Git 引用；目录名义体积不是实际释放容量。
