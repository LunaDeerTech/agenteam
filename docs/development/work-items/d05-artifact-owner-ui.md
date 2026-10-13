# D05 Artifact Owner 只读 UI

状态：实现候选，待独立源码审查和实际验证。以已冻结的 Owner List 与完整 Project / Artifact / File 内容读取 HTTP 为依赖；本卡不宣称浏览器验收或产物创建完成。

## 范围与正式依据

[产物布局](../../frontend-design/layouts/artifacts.md)和[项目导航](../../frontend-design/layouts/project-workspace.md)增加“产物”，在当前项目显式筛选、分页浏览和读取已存在的真实 Artifact。业务规则沿 [D05 B02](d05-object-storage-design.md#7-b02artifact-组合服务)，默认 App 服务已完成范围和未接入口沿 [Human Artifact 接线卡](d05-human-artifact-app.md)。

使用正式两条 GET：`/api/v1/projects/{project_id}/artifacts` 和 `/api/v1/projects/{project_id}/artifacts/{artifact_id}/files/{file_id}/content`。前端严格解析安全元数据；object 仅含 media_type、byte_size、sha256。外层引用、创建者、来源、时间和版本保原合同，decimal string 不转成浮点整数。没有直接对象地址、签名 URL、下载 URL、上传或创建按钮。

## 实现边界

- 独立 Session 只读 facade 与视图生命周期 owner；复用原 Session 实际请求互斥和异常退态，不增加认证队列。
- 名称与类型为草稿，显式提交或清除；每页 50 项，更多使用原已应用过滤与游标，整页校验项目、重复 ID、降序、过滤匹配，跨页继续校验顺序与循环。
- 详情固定完整引用，文本每段 8 KiB、纯文本呈现；上下段只用服务端 UTF-8 范围，重新读取回首段。合法零进展停止下一段并可恢复；图片/文件引用仅安全元数据，无效 UTF-8 不伪造文本。
- 401/403/404 清当前项目旧列表、详情和范围，不把隐藏项目当空列表。取消、关闭、路由、身份和项目代际隔离迟到结果，原请求实际结束前仍由 Session 保持互斥。
- 沿共享主题与控件，桌面卡片网格、窄屏单列、长值换行、明确反馈和焦点恢复。

## 待验证与限制

定向单元覆盖两 GET 精确输入/投影、非法整页、UTF-8/零进展、Session 捕获及实际请求尾、分页/过滤/上下文隔离、真实共享控件和纯文本/焦点；旧 transport、Session/Workspace、导航直接回归由统一 runtime 按真实影响选择。whole type 与生产构建尚待运行。

独立浏览器作者准备通过真实默认 App 的 Human Artifact Service 创建测试产物，再以页面执行分页、筛选、文本分段、图片/文件投影与 Owner 访问失效恢复；所有业务读取须走实际 HTTP。受控 fixture 不冒称生产 Execution/LLM 自动生成。浏览器与后端能力测试由 runtime 统一安排，尚未记录实际结果。

本次只读页面不闭合用户创建产物：目前无平台用户可见 Artifact 创建 HTTP/表单入口，后继最小功能为 Human inlineCreate、原幂等键恢复与文本表单。Source、upload、下载、Agent provenance、自动保存模型文本与既有 STOP 均不因本片恢复。
