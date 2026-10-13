# D05 Artifact Owner 只读 UI

状态：本片独立源码审查、定向单元、全前端类型检查、生产构建及真实默认 App 浏览器验收已通过，待主线程完成 Git 集成。范围仅当前 Human Owner 浏览产物；不表示文本创建、完整 D05 或长期目标已完成。

## 范围与正式依据

[产物布局](../../frontend-design/layouts/artifacts.md)和[项目导航](../../frontend-design/layouts/project-workspace.md)增加“产物”，在当前项目显式筛选、分页浏览和读取已存在的真实 Artifact。业务规则沿 [D05 B02](d05-object-storage-design.md#7-b02artifact-组合服务)，默认 App 服务已完成范围和未接入口沿 [Human Artifact 接线卡](d05-human-artifact-app.md)。

使用正式两条 GET：`/api/v1/projects/{project_id}/artifacts` 和 `/api/v1/projects/{project_id}/artifacts/{artifact_id}/files/{file_id}/content`。前端严格解析安全元数据；object 仅含 media_type、byte_size、sha256。外层引用、创建者、来源、时间和版本保原合同，decimal string 不转成浮点整数。本片没有直接对象地址、签名 URL、下载 URL、上传或创建按钮。

## 实现边界

- 独立 Session 只读 facade 与视图生命周期 owner；复用原 Session 实际请求互斥和异常退态，不增加认证队列。
- 名称与类型为草稿，显式提交或清除；每页 50 项，更多使用原已应用过滤与游标，整页校验项目、重复 ID、降序、过滤匹配，跨页继续校验顺序与循环。
- 详情固定完整引用，文本每段 8 KiB、纯文本呈现；上下段只用服务端 UTF-8 范围，重新读取回首段。合法零进展停止下一段并可恢复；图片/文件引用仅安全元数据，无效 UTF-8 不伪造文本。
- 401/403/404 清当前项目旧列表、详情和范围，不把隐藏项目当空列表。取消、关闭、路由、身份和项目代际隔离迟到结果，原请求实际结束前仍由 Session 保持互斥。
- 沿共享主题与控件，桌面卡片网格、窄屏单列、长值换行、明确反馈和焦点恢复。

## 已验证的前端基础

8 个 spec 合并去重后 **121 个测试实例通过，对应 115 个不同标题**；Account 中两个参数组各有 4 个同名实例，不把标题去重误写成遗漏测试。全前端 `vue-tsc --noEmit` 原 Wait 0 / 39.612s。首次生产构建原 Wait 0 / 1.995s，生成 86 个文件、1,552,688B；原进程退出、受测输入未变。后续浏览器复用该正式产物，未为测试夹具变化重复构建。

基础首轮保留 116 PASS / 5 FAIL：4 个新页面测试的按钮夹具误把没有 `.button-label` 的 UiSelect 当作普通 UiButton；另 1 个旧项目设置导航断言仍期待旧 4 项，未包含已实现的 6 个入口。修正仅两份测试，保留精确按钮与完整导航顺序断言。第二轮定向结果为 4 PASS / 1 FAIL、15 项未选中；剩余旧测试没有等待冷加载路由导航实际完成。改为等待原 RouterLink 的 `afterEach` 并校验无导航失败后，第三轮该 1 项 PASS、15 项未选中。原失败保持，已通过的 116 项及类型检查复用，未重复计数。

## 真实浏览器与视觉验收

Web06 原执行 `61803/71f340` whole Wait 0 / 66.018s，Go 1 top / 1 sub 与 1 个 Node 浏览器任务通过。真实默认 App 的 Human Artifact Service 准备 52 个产物，页面执行分页、名称/类型筛选、完整引用读取、UTF-8 分段、图片/文件元数据、非 Owner 拒绝及恢复、项目切换。20 次 Artifact GET 为 10 次列表 200、9 次内容 200、1 次内容 404；原 Go handler 的 started/ended/returned/accepted 均为 20，proxy 错误为 0，页面没有业务写入。

10 个辅助请求中 7 个 Playwright finished、3 个保持原 exact failed 终态；三者都有独立实际 Promise、reader EOF、长度/哈希、类型化返回与 CDP 联合消费证明，不能改称全部 Playwright finished。原 9 个 PID、6 个自有资源、私有目录和 runtime 均已退出；共用集成二进制在实际消费者退出后处分。运行摘要及 post 留在原 `output/ai/artifact-owner-http/web-native06/`，适用早期输入和失败材料继续保留。

浏览器作者已逐张实际查看本轮四张截图。1440px 浅色目录显示四列卡片、筛选和当前导航；首窗详情焦点框及长 ASCII 在容器内换行。390px 深色显示单列、SHA 换行、原 `<script>` 字样的纯文本、中文与 emoji，以及“上一段”焦点环和禁用的“下一段”。文件截图只覆盖详情首部，“此类型暂不支持在线预览”位于 viewport 外；该文案与图片投影依实际浏览器断言验证，不扩成截图可见结论。未发现本片视觉 must-fix，截图不证明创建表单或全部权限矩阵。

## 原失败与未完成范围

原 Web01–05 均仍为 FAIL：01 原因未明；02 观察器在正式对象初始化之前安装、被后续路由覆盖，修正观察器时序后重测；03 Playwright 失败没有捕获具体原因，保持未定因；04 明确辅助请求 `net::ERR_ABORTED`，旧证明不足以接受完整消费；05 的 16 次原 GET/handler 均完整，失败来自 UiState 的 aria-hidden `!` 被 exact 文本定位纳入。Web06 唯一修正为在原 alert 内精确匹配可访问 heading 并保留可见性断言；产品、Go 和 helper 未变。新通过不倒填旧失败，不由请求状态推断未观察到的产品取消行为。

本片 Runtime disabled，不调用真实模型；准备数据来自正式 Human Service，不能冒称生产 Execution/LLM 自动生成。文本创建 HTTP/表单正在独立后继工作树实现，尚未以本卡验收。Source、upload、下载、Agent provenance、自动保存模型文本与既有 STOP 均不因本片通过而完成或恢复。
