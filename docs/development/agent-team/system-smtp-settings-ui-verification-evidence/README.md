# SMTP 配置 UI 最小不可变证据

关联[正式报告](../system-smtp-settings-ui-verification.md)。接受产品为 `628612cdfc730cc1d89cad4e24a5d20f36cc6812`，只归档固定记录，不执行其中源码、测试、driver或恢复运行环境。

`index.json` 的 `artifacts` 将逻辑名映射至原绝对来源、SHA256、字节数及 `object` 或 `git`。`objects/<SHA256>` 保存原字节，同SHA只存一份；`git` 是可用 `git show <commit>:<path>` 读取的固定提交原件。原绝对路径仅是历史定位，不要求scratch继续存在。`source_versions` 按相对路径／SHA保存实际消费者；`runs`保存原退出／时间／结果，不把失败重写为通过。

| 逻辑入口 | 内容 |
| --- | --- |
| `author/*-stage*/manifest.json`、`author/input01..04/manifest.json` | 阶段冻结源、差量、原基线与复用。 |
| `author/source-versions/`、`independent/source-versions/` | 78个候选／私有源版本；稳定闭包从固定Git读取。 |
| `author/runs/` | 原纯检查、诊断、七个新实际轮与五批旧19组；八张navigation04原图。 |
| `independent/api01/`、`api02/`、`owner/`、`page/`、`startup/` | 原阶段报告、probe、输入和原失败；owner额外FAIL与API旧11组复用边界。 |
| `independent/input01/`、`input02/`、`checks/`、`runs/independent01/` | 原标题静态更正、type01原版本、A/B实际命令和资源终局。 |
| `composition/` | f670→a942固定接缝、作者两旧PASS复用的独立审阅。 |
| `author/delivery29/`、`delivery29-v2/`及对应source入口 | README原版／仅CSRF措辞修订、原验证器失败与最终29清单。 |
| `main-combination/` | 63de0ac＋SMTP候选的三条Go命令、604／3504指纹与实际wait／两扫空。 |

不复制36dist实体、node_modules、modcache、GOCACHE、完整项目树、runtime目录或二进制；原旧回归48图仅保留索引／hash，正式8图保存原PNG。受测input04浏览器是f670，旧new01/new02/navigation03是40c904；主线Go组合不是再次运行浏览器。harness-format00/type00两处原记录缺口保持，不能宣称所有历史运行都有完整环境和逐轮源码绑定。

```bash
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-smtp-settings-ui-verification-evidence/verify_archive.py
```

脚本只读本档案和固定Git，核原字节／精确29路径／阶段源／卡技术／原运行退出／双清。保存的argv/env是证据，不由该脚本执行；运行时清单也不要求当前外部依赖存在。只有卡技术正文参与当前主树检查，产品对比始终指固定`628612c`，不读取后来活动产品作结论。
