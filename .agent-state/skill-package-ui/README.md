# skill-package-ui 已交付材料恢复

## 已交付工作树材料保全

正式主线基准为 `54468a28`，原作者末端 `28cdfa3ea63626d5b8e2f7596a0523dc83d2fd26`；原树 `/workspace/agenteam-skill-package-ui`。此记录只保全现存材料，不重判原 FAIL 或扩大交付范围。Git 与最终 worktree/ref 清理由主线程负责。

全部原任务输出以同文件系统 `RENAME_NOREPLACE` 迁至 [output/ai/skill-package-ui](../../output/ai/skill-package-ui)，迁移瞬间 inode、权限、uid/gid、mtime 相同，无覆盖。原输出中的日志、FAIL、binary、dist、输入清单、截图与原状态均保持；旧绝对路径不改写，按此映射访问。 原 `web/dist` 同样迁为该输出的 `original-web-dist/`。

原 `/tmp/spw01` 保留；小目录副本为输出 `original-tmp/spw01/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。
原 `/tmp/spw02` 保留；小目录副本为输出 `original-tmp/spw02/`，5 个文件逐字及 mode/uid/gid/mtime 核对相等。

### 最小源码恢复

[retired-inputs.patch](retired-inputs.patch) 对应下表第一行。每个版本都独立以命名主线基准开始，不能叠加应用不同版本补丁；只在私有历史恢复树中使用，不覆盖当前产品。所有存在独有差额的历史补丁均在本目录随 Git 保存；实际应用目录和原 apply 日志仍保存在输出 `source-recovery/`。空差额版本直接由命名 main 基准恢复，不生成额外补丁。

| 历史输入 | 恢复补丁 | 技术差额文件数 |
| --- | --- | --- |
| `0a6adca44a6d22c8dd3b1c45fccf46a83e02cf9d`（原最终通过下载输入） | [retired-inputs.patch](retired-inputs.patch) | 18 |
| `53afa517c762280f87d9d45b24db3b3c53d1952f`（原 metadata 观察 wholeFAIL 方法） | [retired-inputs-53afa517.patch](retired-inputs-53afa517.patch) | 21 |
| `2a70c1f49cbfc86ef6b6ddde617e54e4612c3754`（原 App race binary 编译记录源码） | [retired-inputs-2a70c1f4.patch](retired-inputs-2a70c1f4.patch) | 22 |

所有非空补丁均实际运行 `patch --batch --forward --fuzz=0 -p1`，原进程退出 0、无 fuzz/offset；每个差额文件逐字等于指定历史 Git blob（原版本缺席的文件恢复为缺席）。存在原输入 map 的差额文件亦逐项核对 SHA。没有对整个旧树进行大哈希；未改路径由 Git blob/tree 差额界定，未据此声称对所有运行环境重新校验。详细路径/摘要/映射见 [preservation01.json](../../output/ai/skill-package-ui/preservation01.json)。

Skill 已交付功能本身与 main 54468a28 对应路径相同；原 browser 候选尚未合入同期 Agent/Task/Knowledge 的全部后继，因此源码恢复保留这些实际共享输入的有界差额，不能拿该历史补丁覆盖当前主线。最终 browser02 的 2423 项完整 map 原样保留，原清单中出现的补丁差额逐文件与其 SHA 一致。

原 browser01 wholeFAIL：Project GET 200/failed，未到详情/包/截图，原 errorText 未记录，不能补称 ERR_ABORTED。原 browser02 有限 wholePASS：7 GET 全 finished，真实下载文件 365B 与原 canonical ZIP/HTTP 字节联证一致；并未实跑 metadata failed 补证分支或完整撤权/取消矩阵。两轮原工具终态与所有资源尾不改。

browser-inputs-02 的 full_inputs 仅记录数量和 aggregate，未包含逐文件 map；本次保已知 53afa 源码，不将后轮 map 冒称为前轮原清单。原 invalid-target 配方/检查、compile 命令/binary、两轮 evidence 与 dist-01 均保留，未重编或重验。

原 owner 已停写且无自有后台；当前 Usage、AgentRunner、Move 无这棵树的源码/binary/dist/cache/node_modules 运行依赖。可再生 `node_modules`/`__pycache__` 未额外复制；后继依赖恢复必须沿固定锁及工具版本。保护的热 Go cache、RO modules 和活动工作树未触及。未运行产品测试、Go、服务或网络，也未删除旧树或 Git 引用；目录名义体积不是实际释放容量。

额外历史补丁复制自上述已经实际零 fuzz 应用并核对的原补丁，新增 tracked 文件逐字相同；本次只核保存字节，没有重复恢复验证或运行产品检查。
