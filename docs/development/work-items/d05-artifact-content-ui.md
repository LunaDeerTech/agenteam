# D05 Human 文本产物创建 UI

状态：当前 Owner 文本创建、原意图恢复与真实列表/内容回看已通过定向检查、独立审查和默认 App 浏览器验收。只读浏览沿 [Artifact Owner UI](d05-artifact-owner-ui.md)，Human 业务边界沿 [Artifact App 接线](d05-human-artifact-app.md)。本卡不表示完整 D05 或 E01 已完成。

## 可见流程

当前 Owner 在 active 项目的“产物”页面新建纯文本或 Markdown 产物，填写名称、可选描述与正文。内容按原样保留；名称最多 255 UTF-8 字节，描述最多 4 KiB，正文最多 1 MiB。共享弹窗提供字段反馈、键盘操作、离开确认及返回焦点；布局继续使用既有主题和窄屏控件。

三个 POST 使用同一 Project、原 Idempotency-Key 和完整 `name/media_type/content/description?`。Session 仅在内存保存原身份、CSRF、key 和 payload，与 Knowledge 写意图双向互斥。已发请求的失败即使标为后段 NotCommitted 也保留原意图，用户显式查询五态结果；不自动重发、查询或取消。仅明确 Lookup `not_observed` 且项目当前 active 时允许重试原创建，`pending/cancel_requested` 允许明确取消。取消待清理与已取消明确区分。

确认提交后保留历史回执，并通过真实当前列表和完整 Project / Artifact / File 内容读取展示结果；不伪造插入列表。读取失败只提供 GET 重读，不重新创建。临时同 Session 检查保留草稿；真实身份、Project 或路由变更清除私有材料并拒绝迟到结果。导航和退出在 Session 重查之前确认，放弃本地编辑不触发服务端取消。

## 验证与边界

定向与兼容检查去重 **38 个用例通过**，覆盖三 POST 严格输入/回执、UTF-8 边界、原意图恢复和实际请求尾、Knowledge 互斥、确认后的 GET 失败恢复，以及真实 App 表单、离页确认和取消反馈。类型检查及构建通过；组合验收复用已通过的基础检查和当前 86 文件构建。原 `basic01` 表单用例及类型失败保留，必要窄修后通过，不改写首轮结果。

组合候选的 `TestArtifactContentWeb/create-and-recover-original` 已在真实默认 App、PG 和 MinIO 上 **1 Go top / 1 sub、1 Node 用例全通过**，外层 Wait 0 / 64.226s。实际 9 POST、7 GET 完成正常创建、已提交但本地 typed ACK Unknown 后显式 Lookup、`not_observed` 后同 key/完整正文重试、真实 pending 取消和列表/内容回看；`not_observed` UI 没有发出 Cancel。首创建在原 Store 确实被持有时检查加载状态，释放后才确认提交。16 个原响应、浏览器消费和 handler 返回均闭合，退出后无 owned 资源或子进程残留。

[结果](../../../output/ai/task-backlog-reorder/content-web-native01/result.json)与[退出复核](../../../output/ai/task-backlog-reorder/content-web-native01/post.json)记录本次实际通过；同目录 `artifact-content/TestArtifactContentWeb/create-and-recover-original/` 保存原浏览器证明和四张截图。作者与独立审查者实际查看：宽浅色草稿和已创建详情可用，脚本文字保持纯文本；390px 深色的未观察到结果及取消完成提示清楚，原请求重试与关闭可操作，未见页面横溢。窄图为弹窗内部滚动位置，不表示全部字段同时可见。

本轮 Runtime disabled，无模型调用；Unknown 来自局部原 Store typed ACK 控制，不是外部网络故障。文件上传、下载、来源引用、Agent provenance、自动保存模型输出和既有 STOP 不在本片，也不据组合候选宣称 Task 或 Execution 验收完成。
