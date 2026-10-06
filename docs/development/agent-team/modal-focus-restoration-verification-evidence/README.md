# 共享模态关闭焦点恢复最小证据包

接受范围与限制见[正式报告](../modal-focus-restoration-verification.md)。原件保持冻结原字节；本包不执行原测试或资源命令。

- [archive-index.json](archive-index.json)将374项逻辑原件映射到204份按SHA去重文件及61项固定Git来源，共4,698,647字节物理原件；同一字节可能有多个历史逻辑路径。每项保存来源绝对路径、长度和SHA，源码与脚本以文本保存。来源scratch路径只是历史标识，不是重建前置条件。
- `author/input01/`与`author/input02/`分别保存两版四源。其余组件/纯测输入从固定Git `6be5321c51f24108d886a139c7a31bb0aab92db3`重建；最终四源核对正式提交 `79f922ec259d2838052a903612e2a27005618c11`。历史source快照和probe按索引复用相同字节，不复制完整web、依赖、dist、浏览器二进制或缓存。
- 作者20轮command/raw/terminal、六轮浏览器终局及必要runner/实际输入保留；独立六轮动态、两份编译记录、完整probe版本与两报告保留。作者旧红和返修旧红、独立装配红与产品红的截图/错误上下文/trace原件保留，只保留最终zip，不复制展开trace缓存。
- 原new01缺运行时spec哈希绑定，只有保存副本同字节；索引不会制造缺失的运行清单。input02是局部复验加未变部分复用；历史PPID1 Z不属于清理成果。共同重挂只覆盖组件，不接受邀请Session/pageshow。

离线核验只读本包及本地固定Git对象，不读取活动产品树、不联网、不执行归档中的脚本：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/modal-focus-restoration-verification-evidence/verify_archive.py
```

检查保存原件/原失败、两版输入与四交付源、固定隔离闭包/锁、20条作者命令、六轮独立结果/两份编译记录、原输入指纹，以及作者170/独立68个PID终局和独立18次wait记录。PASS表示证据字节及交叉引用一致，不代表重新观测资源或重跑行为。新README/索引/核验脚本属于归档派生文件，原件不作格式改写。
