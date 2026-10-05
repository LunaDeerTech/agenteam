# D26 认证规格原始证据

对应[持久规格记录](../../d26-authentication-spec-verification.md)与[正式卡](../../../work-items/d26-account-authentication.md)。这里只证明已接受的规格与前置契约审查；前端业务尚未验收。原被审卡 rev1、rev1.1、rev1.2 分别保存在各 author/card.md.txt，最后一份 SHA `76745915d15c24dcd86ba061b0ff7bfada440d0a251f23f026166b971838edcd`，不把校验绑在会继续更新的活动卡。

| 目录 | 内容 |
| --- | --- |
| `feasibility/`、`rev1/` | 原可行性、rev1卡/自查与独立F1 BLOCKED；只读输入manifest，不复制fixed源码目录。 |
| `api-correction/` | 原schema纠正作者源码/输入/输出/报告及独立复核。修前拒80、收43是预期重现，原exit0保留；接受提交为df78a92。 |
| `rev1.1/` | 消费正式pass80纠正、独立STATIC PASS，以及作者原no-index检查期望错误。 |
| `root-dependency/` | 422e0c1→457b197固定10路径影响、三生产原patch和独立STATIC PASS。 |
| `rev1.2/` | 统一457b197的新卡、差量、自查及独立STATIC PASS；采纳提交64e47fb。 |
| `environment-preparation/` | 当时只读版本、浏览器文件、锁缓存调查；不是安装、浏览器启动或业务通过。 |

[provenance.json](provenance.json)逐项映射46份原件的绝对来源、原相对名、SHA和字节。`.md`与原validate.py增加`.txt`存储后缀只为保留历史内容而不按当前相对链接或可执行脚本解释；字节未变。原SHA256SUMS/索引仍指向原目录命名，保持历史原样，不冒充本目录的新索引。

[SHA256SUMS.json](SHA256SUMS.json)绑定本归档自身（不循环hash自身），[archive-checks.json](archive-checks.json)记录实际归档核对；[whitespace-exceptions.json](whitespace-exceptions.json)只列实际原字节空白告警路径。新增说明和代码必须无空白告警，原件不为格式检查绿化。完整Git源码、npm/browser/cache不进入目录。

本地Git离线核对与有界恢复：

```sh
python3 docs/development/agent-team/evidence/d26-authentication-spec/rebuild.py --check
python3 docs/development/agent-team/evidence/d26-authentication-spec/rebuild.py --output /tmp/new-owned-d26-spec-evidence
```

[rebuild.py](rebuild.py)只核46份原件、归档指纹、固定选读源及3个Git物件；可在全新仓库外目录恢复原件、旧/新account.json和已采纳原卡。脚本不联网、不运行schema validator或业务代码、不写Git；已有固定对象缺失则失败。旧作者validate.py.txt按原字节保留，其中绝对执行路径是历史命令的一部分，不能直接当成当前可运行路径；再次验证应在新私有输入上明确适配，不改原件。

[后续独立计划](independent-plan.md)尚未执行。原schema、规格和依赖STATIC PASS不能代替Go/npm工程、正式dist浏览器、Cookie并发或数据库组合验证；D28正式单站hosting和原已知Object/Artifact/Project边界保持。
