# Knowledge Owner 文本创建与正文编辑 UI

当前有限实现和真实正常链已独立接受，随本次集成交付。消费[文本 HTTP](d12-knowledge-owner-text-http.md)、[正式 OpenAPI](../../../api/openapi/knowledge-text-commands.json)及既有[当前正文读取](d12-knowledge-owner-content-http.md)，不新增领域 writer、迁移或依赖；完整 D12 未完成。

## 产品行为

已有 initialized Project 的当前 Human Owner 在知识库页使用“新建文本”创建根文档，或在当前详情使用“新建子文档”。表单包含标题、父文档、纯文本/Markdown 格式和正文；父选择器使用已实际读取的同项目 active 目录。正文可为空，保留原文字与换行。完整编码写 JSON ≤1048576 B；原意图 Lookup 包装 ≤1048660 B，嵌套原请求仍 ≤1048576 B。

“编辑正文”先读取完整当前文本及版本，不能用默认分页阅读片段覆盖全文。替换仅发送 expected_version、media_type、text，保持当前标题和父文档。确认后沿真实 metadata、ancestors、content 与相关 children 重读，不把历史 receipt 当作当前正文授权，也不乐观拼接目录。临时读取失败只提供重读；当前 403/404/410 拒绝清除私有编辑材料，并保留非敏感的“保存已确认”事实。

App 持有编辑草稿，唯一 Session 持有原身份、目标、key 和完整 body。版本冲突保留草稿与旧版本，用户明确重读当前正文并采用版本后才能再次保存；Unknown 不自动查询/重发，只有显式 Lookup 返回 not_observed 才开放按原请求重放。同 Session checking 和页面卸载隐藏并保留草稿/原入口，真实身份变化或失权清理材料。切换文档、关闭、离页和退出均处理未保存确认。Markdown 按源文本展示，不执行 HTML。

## 验证与边界

产品输入冻结于 `9382b447`；浏览器最终 fixture 为 `ef1c8b47`，原 Go race binary 使用 compile-02。作者定向 API、真实 Session/Workspace、真实 App 控制共 24 个不同用例通过；连同适用 Knowledge/Variables 回归，10 文件 88 个不同用例有分段 PASS，不称当前 HEAD 一轮全量。原 `checks/unit01.log` 三失败（checking 相邻时序丢草稿、同步回读重入）保持；修复与真实拒绝控制见 `text03.log`、`denied04.log`。旧 `regression01.log` 63 PASS/2 FAIL 保留：两个 fixture 错把成功 EOF 后 outer.cancel 当必经，改为真正媒体早拒绝的 outer 尾入口，原 Stop/队列断言不变；该方法经独审，`regression02.log` 四文件 38 PASS。最终类型检查无诊断，fresh 构建实际 0，源码 before/after SHA 相同。

真实浏览器 `TestKnowledgeTextWeb` 的 native03 已全链通过并独立接受：

- 通过页面完成根 plain 和 Markdown child 各一次创建、一次替换及当前 metadata/ancestors/content/tree 回读；最终两文档均 v2，4 条 command、4 条 content_changed、未泄漏 reader，当前 guard/服务退出闭合。
- 28 个观察请求为 23 finished 与 5 个原 GET ERR_ABORTED；失败事件保留，按原请求 ID、响应消费与实际服务端证明核实。四个 POST 均 200/finished；26 份 response proof 为 25 Knowledge 与 1 Project，不称全 API 三联证。浏览器 Lookup 为 0，服务后验用 4 次原 key Lookup 核查提交。
- 键盘进入/提交、窄屏文档树抽屉、Escape 与焦点恢复、减少动效和品牌图片解码已检查；1440 浅色 plain 与 390 深色 Markdown 两图已独立查看。
- 原 Node/Go/driver 与四个 Chrome Wait 均为 0；七资源各两次 absent，private/runtime/descendants/TCP 双尾齐，输入 unchanged。Go 用例 27.78s，supervisor terminal 0 /126.552s，外工具原退出 0 /129.894s。

原 native01 整体 FAIL 保留：旧外部 dist 残留两份 Session bundle，严格唯一绑定静态复现拒绝；原早期 Node 诊断未覆盖该阶段，不补造日志。native02 已执行四写，但窄屏 fixture 错要求隐藏的桌面树常驻可见，整体仍 FAIL；改为真实 Drawer 操作后 native03 通过。旧 dist、两轮退出与资源尾原件均保留。

该正常链不代真实 Unknown、重放、撤权或并发冲突全矩阵；相应前端风险为受控网络证据。上传、移动/删除 UI、PDF/DOCX、下载 URL、全 Project 生命周期及生产 initializer 未由本片完成，ready=false/503 和 Object Runtime join 等显式 STOP 保持。隔离 fixture 只对已有 Project 前置使用 test-only initializer，默认真实 App/Account/Knowledge 服务处理业务；静态测试托管不代表 SPA 生产发布。

## 原件与最小恢复 recipe

本机结果根 `output/ai/knowledge-text-ui/`：`checks/` 保存上述分段检查；`checks/fresh02-input.json` 保存精确构建命令、actual exit 0、283 源文件 before/after 及 74 dist 文件 SHA。`native03/invocation.json`、`terminal.json`、`outer.log` 和 `/tmp/ktw03/ui-d2b7ea3520a64aa3.log` 保存原命令/Wait/资源尾。`native03/evidence/TestKnowledgeTextWeb/` 保存安全结果、stage、response proof 与 `knowledge-text-1440-light-plain.png`、`knowledge-text-390-dark-markdown.png`。本轮 input hash 为 `de604122a885030fc79863a3a519b44060ab999f0b155d58b41ce40260da4c4d`。

先恢复两个锁定 npm 根及正式后端固定 Go/PG/MinIO 依赖，再使用原 race binary/选择器；不更换 supervisor。Vite 在 web 根以外默认不清空输出，必须选择全新唯一 dist 目录并显式 `--emptyOutDir`，保留旧失败目录；先核原严格 Session 唯一绑定，再消费该冻结 dist。实际本轮目录为 `output/ai/knowledge-text-ui/dist-native02`。

浏览器私有环境沿原 invocation 设置 `AGENTEAM_KNOWLEDGE_TEXT_WEB_DIST`、`AGENTEAM_KNOWLEDGE_TEXT_WEB_EVIDENCE` 等既有项，重新运行时 runtime/evidence/output 均须 fresh；宿主 TCP 观察窗口与其他真实 native/外网任务协调。原命令为：

```sh
/usr/bin/python3 /workspace/agenteam-knowledge-text-ui/.agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver /workspace/agenteam-knowledge-text-ui/.agent-state/work-owner-http/root_chain_driver.py \
  --binary /workspace/agenteam-knowledge-text-ui/output/ai/knowledge-text-ui/compile-02/app-race.test \
  --run '^TestKnowledgeTextWeb$' --output /tmp/ktw03 --root-chain
```

命令中的本轮 runtime 已留作原件，新运行须改为新的私有短路径；既有本轮 PASS 不通过重复执行补写。后续仅有实际产品/方法变更或新的风险才重跑对应输入。
