# 邀请 UI 最小证据包

对应[正式验收报告](../system-invitations-ui-verification.md)，固定已接受交付 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`。本包不执行原命令。

[archive-index.json](archive-index.json)保存503项逻辑原件，映射到261份按SHA去重实体与22项固定Git来源，实体共2,756,912字节。每项含历史source路径、长度、SHA及archive或Git定位；scratch路径仅为来源标识，离线核验无需这些目录存在。

- `author/input01..07`：七版manifest与精确21源，六个原delta、原check日志、观察器源码；24dist只保留manifest哈希及构建原日志。固定Git79f922加候选源可重建产品源码；锁/后端/迁移仍来自9b32015，早期独立pure闭包按6be5321或79f922逐SHA映射。
- `author/runs/`：七轮原command/raw/driver/input/退出及双清、完整新旧顶层结果。最终八截图和new05离场缺口/已审两图保留；第22路径README原/终字节、diff和原检查单独归档。
- `verifier/`：最终报告及其67件选择原件、pure01–08精确probe/driver/命令/结果/指纹、静审/观察归因报告；两真实轮完整必要原件、三项编译/list。原工具首红与真正产品红不删除，分版子组复用不改称整轮通过。

不复制node_modules、Go/浏览器缓存、compiled binary、dist实体或全树。历史manifest“尚未运行”和README deferred保持原时态；后续动态及补交记录解释最终状态。孤立早期本地日志未配完整命令/环境/逐轮快照时，不补造缺件。原Markdown/脚本以文本原件保存，不为排版改写。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-invitations-ui-verification-evidence/verify_archive.py
```

脚本只读本包与本地固定Git对象，校验原件SHA、七版源和复用、22交付路径、31项独立pure闭包、锁/迁移不变、七作者真实/八独立pure/两独立真实以及原资源终局。它不联网、不读活动产品树、不启动浏览器/服务、不执行归档命令；PASS是证据字节和交叉关系一致，不是重新验收产品或清理资源。脚本、此说明和索引是派生档，其余原件按字节保留。
