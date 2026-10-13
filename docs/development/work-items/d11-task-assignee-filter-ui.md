# D11 当前 Sprint 的 Agent 筛选

状态：限定验收通过，本批文档待本地合入；GitHub 认证失效，尚未远端保存。

沿[任务布局 §3](../../frontend-design/layouts/tasks.md#3-kanban-栏目与字段)与[Work Owner HTTP 查询合同](d11-work-owner-http.md#3-查询投影与有界分页)，在现有任务搜索中增加“筛选 Agent”。选项为全部 Agent、未指派及当前项目正式 Agent 目录；每批 50 项，可明确加载更多。标签沿目录 display_name/name，不从 Task 首屏推测可选 Agent。

筛选草稿在“搜索”或 Enter 后应用，与关键词、优先级、类型作服务端 AND 查询。全部省略 `assignee_agent_id`，未指派编码查询字面量 `null`，指定 Agent 编码其真实 ID；客户端验证每个返回摘要的 assignee 与已捕获筛选一致。七列重新从第一页读取，继续分页沿已应用条件；只改草稿不改变原 cursor 的条件。“清除筛选”同时清空四项。

Agent 目录读取不改变已应用筛选。第一页没有已选 Agent 不是删除证明；同项目保留草稿和已应用项的标签，真实身份、Project 或 Sprint 切换沿原代际清理，当前 Project Get 明确拒绝访问也清除已选材料。目录 cursor 失效清除旧目录链，只提供明确重读，不自动换条件或重试；普通传输失败保留旧 cursor。分页按钮消失或重读结束后，在原上下文且用户没有移走焦点时返回 Agent 选择器。

复用原 Session GET 协调、Project/Tasks controller、UiSelect/UiField 与主题 tokens；不新增 Task/Agent 写入、指派或执行行为，不新增后端或 API 契约。生产范围为 `client.ts`、`work-review.ts`、`useProjectTasks.ts`、`ProjectTasksView.vue`。单元沿 `web/src/tests` 检查三态 wire、错误响应、原 Session/Workspace 分页与身份失效、真实控件及焦点；真实浏览器复用既有 Work HTTP shell、正式 Agent/Work 服务准备及 PostgreSQL；Project 准备使用持久化的测试 Skills receipt，不代 Project 创建 HTTP、生产 Skills 或默认 App 生命周期验收。

[frontend-basic01](../../../output/ai/task-assignee-filter-ui-delivery/frontend-basic01/result.json) 的完整类型检查通过；7 个测试文件运行 43 项，其中 41 通过、两个旧测试断言失败。一个仍期待未包含 Agent 的旧错误文案，另一个对整个 document 断言无 form，误排除了既有 Sprint 只读搜索。修正仅触及这两个旧 spec：保留抽屉无写表单、五类任务写入口不可见、全页仅原搜索表单以及一次原 Transfer、三次同 key/body 原 Lookup 的完整检查。[frontend-basic02](../../../output/ai/task-assignee-filter-ui-delivery/frontend-basic02/result.json) 仅定向复验原失败两项并通过，与原 41 项组成 43 项有效结果；不称一次全量全绿。原 FAIL 保留，生产字节未改，[构建](../../../output/ai/task-assignee-filter-ui-delivery/build01/result.json)通过，继承 Usage 主线后复用未变的 84 个 dist 文件。

受测源码 `a8635af8` 的 `TestTaskAssigneeFilterWeb` [native01](../../../output/ai/task-assignee-filter-ui-delivery/native01/result.json) whole PASS：1 top/2 sub 共 160.62s（分页 77.29s、组合与详情 83.33s），两个原 Node 调用均成功返回；外层原 `38938/420762` Wait 0、263.995s。实际读取 51 个正式 Agent 的 50+1 目录，使用第二页 Agent 筛选超过一页的已指派任务；改草稿后继续分页仍带原 Agent/priority/type，明确应用才换条件。两组同时核对省略/null/真实 ID、关键词与枚举相与、空结果、清除及详情往返后的筛选保留；另一个 Project 的同词任务不混入结果。浏览器只有 GET、没有写入或 Lookup，相关 Project/Agent/Work 历史、Outbox/Audit 及 Dispatch/Execution 计数前后不变，见[原链日志](../../../output/ai/task-assignee-filter-ui-delivery/native01/chain.log)。

[退出核验](../../../output/ai/task-assignee-filter-ui-delivery/native01/post.json)闭合原 Wait、14 个原 PID、六个自有资源的两轮 absence、三个私有目录及 runtime、descendants、Host TCP 双尾，受测输入未漂移。唯一测试二进制在编译及原消费者共 17 个 PID 均退出后，按原路径、大小与 SHA 核实并精确删除，释放 103,534,830 B 文件；[处分凭证](../../../output/ai/task-assignee-filter-ui-delivery/compile01/binary-disposition.json)确认路径已不存在，原日志、失败和截图保留。[四张实际截图审阅](../../../output/ai/task-assignee-filter-ui-delivery/native01/visual-review.json)仅覆盖 1440×960 浅色的分页卡片滚动位置/组合筛选，以及 390×844 深色的保留/清除状态；没有本轮 must-fix。分页图不包含筛选控件，不冒完整响应式或无障碍矩阵；键盘、回焦、reduced motion 和无整页横溢按实际浏览器断言接受。

目录失效、真实 Session 清理和当前 Project 拒绝仍按受控前端测试范围验收，不代真实浏览器权限转移矩阵。本片不完成完整 Explore、规划写操作、运行调度、默认 App 生命周期、默认 Registry 或 E01，既有 STOP 保持。
