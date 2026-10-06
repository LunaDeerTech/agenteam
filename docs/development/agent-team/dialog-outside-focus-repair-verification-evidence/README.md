# 遮罩关闭焦点修复：最小证据

[正式报告](../dialog-outside-focus-repair-verification.md)接受共享组件四路径提交 `b53895f7eb1d020276e8f54a99a7c0821b286481`，基线为 `0f68d445df66bb05228fca3604618d4f2a7e6ffa`。完整系统用户目录 UI 不在此结论内。

[archive-index.json](archive-index.json)映射原绝对路径、归档相对路径、SHA-256、字节数和用途，共110份逻辑原件、96份不同字节原件，原件实体共1,028,722字节。相同 SHA 只存一次，某轮建议路径可能复用另一轮实体；以索引映射定位，不把缺少重复副本当成原件丢失。原件不改写，报告/代码改用 `.txt` 后缀避免被当作当前源码或活动 Markdown。

- `candidate/`：四条接受源码；作者旧轮精确旧 UiDialog 另存，旧/新浏览器测试与配置按相同 SHA 复用。
- `inputs/`：作者四路径、隔离纯检查必要输入和独立十文件组件闭包。
- `author/`：原报告、版本、命令/raw、旧红新绿及原资源记录；首次缺颜色文档和 Node typeRoot 的准备失败保留。
- `verification/`：原独立报告、两版 probe、实际 wait/双清、首轮原 clean=false 与事后清理、作者旧轮两个 PPID1 Z 的精确只读核验。
- `history/`：原业务 UI new02 失败，以及原生 DOM 机制第一次长路径准备失败和第二次结果；不包含后续活动 UI。

重建作者 pure02 输入：从基线 Git 取 `web/`、`tests/account-captcha-web/package.json`、对应锁文件及 `docs/frontend-design/styles/colors-and-themes.md`，再按 [author-input01](inputs/author-input01.json)覆盖/增加四条候选。各文件必须匹配 [author-frozen-input](inputs/author-frozen-input.json)；其中90条原 web 文件仅 UiDialog 改变，新增组件单测另计。浏览器闭包只取独立输入列出的十文件；九项依赖可由基线 Git 精确恢复，无需复制整个已接受应用。运行环境和依赖版本须重新按原锁准备，归档中的旧临时路径不是可直接复用的运行授权。

离线核验命令，在仓库根执行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/dialog-outside-focus-repair-verification-evidence/verify_archive.py
```

脚本只读本目录和本地 Git blobs，不读取 scratch、不联网、不重跑浏览器/业务测试或执行存档 driver。它核完整性与原记录的一致性，不重新证明浏览器行为或资源的当前状态。缺少本地历史 Git 对象时直接失败，不触发 lazy fetch。

历史限制不可被后轮覆盖：作者 old01 两个 Z 已退出但未由当前父进程 wait；独立首轮原 clean=false 只在另份真实清理记录后收敛。首次 standalone tsc 仅保存当时工具结果记录；作者存档 runner 为后轮版本，未伪造旧 runner。原 trace/截图只作诊断原件，未归档 node_modules、浏览器二进制、全 web、缓存或 dist。

文档及派生文件的格式检查通过。全新增文件 whitespace 检查还指出三份原始日志自身的历史格式：`author/runs/old01/raw.log` 与 `history/new02/raw.log` 有尾空格，`author/runs/pure01/raw.log` 有末尾空行；这些字节与原 SHA 一致，故原样保留，不通过格式化改写失败证据。
