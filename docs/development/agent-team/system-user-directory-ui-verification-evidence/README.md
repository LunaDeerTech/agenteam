# 系统用户目录 UI 最小证据包

接受范围和实际命令见[正式报告](../system-user-directory-ui-verification.md)。归档只读冻结原件，不包含依赖缓存、完整web、22个dist实体、runtime或可执行文件。

- [archive-index.json](archive-index.json)逐项记录原绝对路径、SHA-256、字节数、用途及物理位置。264个逻辑原件去重为147个新物理文件、16个复用旧档物理文件；新原件1,993,538字节。`../dialog-outside-focus-repair-verification-evidence/`仅复用已提交相同SHA原件，未修改旧档。
- `author/input01`至`input04`保存各版输入/检查清单、22项dist哈希、必要差量和精确19源；同字节可能映射到另一版。最终244项检查及每条author-check所指raw均保留，首次准备/单测/Go缓存/类型失败没有覆盖。
- `author/readme-final01`保存另行完成的第20路径README原文、最终文、差量、文档检查和20项交付清单。它在真实验收后交付，不算浏览器已测源码。
- `verification`保留原独立报告、静审历史、三组冻结组合、pure两测试及编译/类型命令；七轮真实顶层原件含实际driver、probe、命令/raw、输入前后、PID/资源、双清理与实际wait。独立两次CDP失败和各自原probe仍在。
- 12张截图包含new03八个主题/尺寸组合、independent03两张、new02两张原布局。实际审阅范围见正式报告；不将所有归档图片宣称为人工逐张审阅。

重建源码时用固定Git `3affc0194214101cfa1e6fdc583afa5d60005db8` 加input01–03各自19源，或 `b53895f7eb1d020276e8f54a99a7c0821b286481` 加input04的19源；清单中的其他输入从相应Git提取。20项正式交付由 `7ef3e30cf06b5df6d19516f24c34855a8308ef05` 定位。实际独立Go overlay及TS/config另有逐轮原件，scratch绝对路径是历史标识，不是要求沿用的机器路径。dist只可核原记录关联；本包不声称保存了可直接执行的历史构建。

离线核验只读取本包、复用旧档及固定本地Git blob，不读取当前产品工作树，不联网、不执行历史命令或启动真实资源：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-user-directory-ui-verification-evidence/verify_archive.py
```

预期PASS为264逻辑原件、147新物理原件、16复用物理原件、143个Git blob、四作者输入、19测试源/20交付路径、82最终组合输入与七轮实际结果/终局。它核持久证据完整性，不重新证明业务运行。原始log/diff中的空行/尾空格与冻结时态按原字节保存；基础焦点旧PPID1 Z限制及原准备失败仍以[旧正式报告](../dialog-outside-focus-repair-verification.md)为准。
