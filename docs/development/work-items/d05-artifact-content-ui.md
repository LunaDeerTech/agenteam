# D05 Human 文本产物创建 UI

状态：实现候选，待定向工程检查、独立审查及真实 App 浏览器联调。只读浏览沿 [Artifact Owner UI](d05-artifact-owner-ui.md)，Human 业务边界沿 [Artifact App 接线](d05-human-artifact-app.md)。本卡不表示完整 D05 或 E01 已完成。

## 可见流程

当前 Owner 在 active 项目的“产物”页面新建纯文本或 Markdown 产物，填写名称、可选描述与正文。内容按原样保留；名称最多 255 UTF-8 字节，描述最多 4 KiB，正文最多 1 MiB。共享弹窗提供字段反馈、键盘操作、离开确认及返回焦点；布局继续使用既有主题和窄屏控件。

三个 POST 使用同一 Project、原 Idempotency-Key 和完整 `name/media_type/content/description?`。Session 仅在内存保存原身份、CSRF、key 和 payload，与 Knowledge 写意图双向互斥。已发请求的失败即使标为后段 NotCommitted 也保留原意图，用户显式查询五态结果；不自动重发、查询或取消。仅明确 Lookup `not_observed` 且项目当前 active 时允许重试原创建，`pending/cancel_requested` 允许明确取消。取消待清理与已取消明确区分。

确认提交后保留历史回执，并通过真实当前列表和完整 Project / Artifact / File 内容读取展示结果；不伪造插入列表。读取失败只提供 GET 重读，不重新创建。临时同 Session 检查保留草稿；真实身份、Project 或路由变更清除私有材料并拒绝迟到结果。导航和退出在 Session 重查之前确认，放弃本地编辑不触发服务端取消。

## 验证与边界

新增定向测试覆盖三 POST 严格输入/回执、UTF-8 边界、原意图恢复和实际请求尾、Knowledge 互斥、确认后的 GET 失败恢复，以及真实 App 表单、离页确认和取消反馈。目前尚未运行；实际结果由统一 runtime 返回后更新。

浏览器将接真实默认 App、PG 和 MinIO 的正常创建、真实已提交丢回执 Lookup、未观察到的原请求重试、pending 取消与只读回看。未验证实现不能替代该链路。文件上传、下载、来源引用、Agent provenance、自动保存模型输出和既有 STOP 不在本片。
