# System 用户目录读口持久证据

对应[正式验收记录](../system-user-directory-read-verification.md)，仅接受 D27 读口 rev2 六路径后端结果，交付提交 `3affc0194214101cfa1e6fdc583afa5d60005db8`。

[归档索引](archive-index.json)逐项保存原来源、归档位置、SHA-256、字节数及用途；覆盖验证负责人[冻结的 117 项原件选择](verification/archive-selection.json)，再保留该选择清单本身，合计 118 项逻辑原件。相同字节允许多个原路径映射到同一归档文件，共 103 个原件文件。原 `.go`、`.py`、`.md` 只改为文本后缀，内容未改；源报告内旧绝对路径和当时待验状态也未重写。

## 输入与恢复

- [input01](inputs/input01.json)是业务基线 `125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b` 加五路径；[input02](inputs/input02.json)是同一基线加最终六路径。其他源码和依赖锁从该固定 Git 对象读取。
- `archive-index.json` 的 `input_sources` 逐路径指定各版精确字节。最终六源保存在 `candidate/`；`history/input01/` 仅保留不同的三份旧版本，另两份未变生产源复用最终副本。恢复历史输入须从原业务基线开始，不能把 input01 覆盖到已含 HEAD 修复的新基线上。
- `author/` 保存两原报告、11 条 Go 检查 argv/env/exit/raw、原 schema checker 准备错误及修正脚本。`verification/` 保存静审纠正、最终独立报告、overlay/probe、初始资源基线及小型依赖来源证明。
- `runs/` 保存三轮原命令、raw、前后指纹、driver 文本、PID/starttime、exact-ID 与双清理。去重后的物理路径以索引为准；原件中路径没有改写为新位置。所有保存的脚本与探针仅作为证据，本归档核验不执行它们。

没有完整工作树、modcache/gocache、MinIO 源码树、二进制、runtime、数据库或私有运行材料。源码恢复只需要固定 Git 与精确候选文本，原 scratch 不再是唯一来源。

## 只读复核

在仓库根目录运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-user-directory-read-verification-evidence/verify_archive.py
```

[核验脚本](verify_archive.py)只读本目录及固定 Git blob：核原件 SHA/长度、117 项选择映射、两版源、六源交付提交、七项锁/边界、11 条作者检查与三轮原日志/清理的一致性。它不读取原 scratch，不运行 Go、Docker、浏览器、SQL、网络请求或保存的脚本，也不写文件。该检查验证持久记录，不能代替产品测试。

原 author01 的 HEAD405、driver exit1 和 rev1 静审遗漏保留；author02 两顶层及 independent01 一顶层实际 PASS，其他包 no-tests 与集成仅编译不计动态通过。具体接受范围和未验证边界以正式报告为准。
