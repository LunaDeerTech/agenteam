# Email canonical roundtrip 不可变证据

本目录服务于[正式报告](../email-canonical-roundtrip-verification.md)，绑定产品提交 `f670cb1fe1f07ebd21bdb90a2b96cd565c33f05a`、基线 `819aba1b8f764328f1e2e67b53c274fad0db877d`。

[index.json](index.json)保存394个逻辑原件的来源、SHA和长度；194个唯一原件位于`objects/<sha256>`，9项原件以固定Git字节复用。`origin`是历史来源标识，不是离线验证依赖。读取对象时保留其原字节；原报告中的相对链接或scratch引用须通过索引逻辑路径解析，不能因迁入对象目录而重写原件。

| 逻辑命名空间 | 内容 |
| --- | --- |
| `author/` | 两阶段源、原失败与12次离线检查、三轮真实命令/raw/实际wait/双清、末件文档与十路径清单。 |
| `core/` | 独立原S oracle、边界/wire两代表、原函数比对纠正及依赖门禁。 |
| `spec/`、`harness/` | 原静审报告、接受页首全文固定Git引用；原候选全文缺件在正式报告明确。 |
| `independent/` | 两版probe/overlay/driver/冻结门禁、两轮真实原输出和清理、失败分析、CLI/空间准备说明及最终四子例组合。 |

`delivery`为精确十条产品路径；`runs`记录28次原检查，其中五次真实资源运行。对象共9,661,988字节，保留原失败，按SHA复用相同副本，不复制依赖树、缓存、dist或二进制。621/886阶段文件从固定Git恢复，最终881只读依赖与3501/3512运行时文件指纹保留在原metadata；运行时指纹不承诺归档工具本体。

在仓库根运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 GIT_OPTIONAL_LOCKS=0 python3 docs/development/agent-team/email-canonical-roundtrip-verification-evidence/verify_archive.py
```

[离线脚本](verify_archive.py)仅解析原件和读取指定Git blob；不执行归档中的代码、不访问scratch、网络或真实资源，不以当前活动产品源码替代固定接受输入。它核对五轮实际退出/双清与按子例复用，不会把independent01整体失败改写为全绿。正式报告、卡片页首及本索引是后续归位材料，与其中保留的原作者/独立报告分列。
