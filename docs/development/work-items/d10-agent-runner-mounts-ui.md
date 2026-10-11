# Agent Runner 与资源目录配置 UI

状态：有限 UI 范围已通过真实 runtime-off 默认 App / 浏览器 `native02`，待主线集成。最终产品 `2ff3cddd`，正式资产 `build02/dist-02`，复用 `compile-web02` 的 Go binary；原编译、焦点及其他失败全部保留。前端由 variables_review 实施，Go/PW 由指定独立作者编写，产品与真实业务/五图另行独审；Git 和运行窗口由主线程协调。

## 契约与范围

沿 [Agent Runner 挂载声明](d10-agent-runner-mounts.md)、[正式写协议](../../../api/openapi/agent-configuration.json)与[当前读协议](../../../api/openapi/agent-runner-mounts.json)。在原项目 Agent 创建、编辑页选择系统 Runner 并输入资源目录名，不增加 SystemAdmin 门或独立 Mount.Create 页面。只展示安全名称、说明及 online/offline/incompatible 状态；后两种状态仍允许选择，不表示可立即执行。

声明为有序 `runner_mounts:[{runner_id,workspace}]`，最多 128 项，相同二元组拒绝。目录名逐字采用正式 ASCII 单段规则，不 trim、拼接路径或替换为 WorkspaceID。Create 与旧 `allowed_mount_ids` 二择一；Update 未编辑时省略保留，显式删除全部发送 `[]`，不发送 null。旧 AgentConfig strict DTO 与原 command/key/expected_version/Lookup 不变。

## 当前读与恢复

Runner 候选按 ID 升序手动分页，每页最多 50。编辑声明须取得当前 GetConfig 与 runner-mounts 的同 Project/Agent/version，MountID 集合也须完全相等；不一致时不拼页或回填，提供明确重新读取并保留已有草稿和原版本，不自动轮询。旧无挂载 Agent 可读空数组；旧非空引用缺少稳定映射时显示受阻，不能伪造空数组。Runner 局部依赖错误不修改未触及字段；当前身份或实体拒绝必须撤下对应观察并阻止写入。

两 GET 复用原 Agent read action 和唯一 Session owner，保持实际 fetch/body/cancel 返回后才释放。身份、Project、Agent、路由或读上下文改变后，迟到结果不得发布；checking 隐藏旧观察。当前读、创建、保存或 Lookup 的权限/实体拒绝统一清除可见目录、配置、映射和草稿，并阻止写入；成功读取当前 Agent 目录后才恢复创建入口，原编辑须重新选择和读取。503 等局部不可用保留未触及字段及草稿。草稿遵原离页确认，Unknown 保留 Session 中原声明、expected_version 和 key，只查询原命令，即使已撤下可见草稿也不得重新提交。新 receipt 校验声明顺序、生成映射和 Agent 引用集，仅表示历史确认；确认后重新 GET 当前配置和挂载，不将 receipt 当当前编辑值。

## 文件与验证

前端产品限定新 typed read API、Runner/workspace 字段组件与原 Agent codec/controller/Create/Edit/View、client/useSession 两 GET；不新增 router/App/菜单或执行器绑定，消费已交付的后端配置能力。专属控覆盖安全闭集 DTO/预算/重复键、声明兼容与 receipt、版本错配与草稿、权限/迟到/实际取消尾、原 Unknown Lookup 以及真实组件键盘/焦点。旧 Agent fixture 仅按新增读和确认后回读适配，保留原断言。离线针对性控制与正式构建之后，经主线程排期完成下述真实默认 App 小链；不外推未运行的矩阵。

不开放 Tool/Secret 授权编辑，不新建 Runner 管理页，不把配置保存当物理目录已创建、Runner 可执行或默认 executor/dispatch 已绑定；原生产 initializer/ready 与其他 STOP 保持。

当前针对性验证：codec03 新旧两文件 17 PASS；稀疏声明输入在原实现真实失败后仅以 Array.from 修复。末尾换行的目录名和版本在原代码即拒绝，控制通过，不列作产品缺陷。state01 六文件 26 PASS / 4 FAIL，其中新旧状态/Session 四文件 20 PASS；两旧页面 fixture 未模拟新增当前 GET 或未更新明确读取次数，定向适配后 legacy-pages02 两文件 10 PASS，保留原保存、Unknown、权限断言和原 FAIL。

当前权限拒绝缺口先由实际反例确认：denied-repro01 三个读取入口 FAIL（另三个 503 草稿控制 PASS）、create-denied-repro01 创建入口 FAIL、page02 PATCH403/Unknown Lookup404 两项 FAIL。统一清理修后 denied-fixed01 新旧 state 两文件 19 PASS，包含原状态行为、三个局部 503 保留草稿及显式当前目录重读恢复；page03 实际组件、Session 和 codec 单文件 8 PASS，保留原页面交互及 PATCH403/Lookup404 原门，拒绝后再次查询仍携带同原 key 与完整原声明。type01 在新 page 控尚未加入时通过，仅作为该阶段证据。已组合 main Usage 与后端 c189，固定 14562113 的 merge-regression01 四文件 26 PASS，覆盖 Runner client/Session 与 Usage client/真实 Session 状态；build01 一次完成全源 vue-tsc 和 Vite，实际退出 0。两步 web 源码相对冻结提交前后无差额，最终 page 控包含在类型检查输入内；82 个正式资产已物理保留为 dist-01 并逐项核对原产物。原件保存在本树忽略的 output/ai/agent-runner-mounts-ui/checks，上述不同输入的分段结果不写成单次全绿；真实默认 App 验收另见下文。


## 最终浏览器范围与原失败

`compile-web01` 因新 Go fixture 未使用的局部变量编译失败；修正后 `compile-web02` 与精确 selector list 实际通过。`native01` 两个子例均在创建/编辑表单首焦点门失败，尚无浏览器写入，原整轮 FAIL 和完整资源退出尾保留。分段 held 模型→Runner 读取的 `focus-repro01` 四项 FAIL 确认首聚焦过早：前两项实际观测到 Runner 仍未返回时的 FocusEvent，后两项也因同一早期事件失败，不冒称原迟到响应另抢焦点。

最终产品以独立初始化 token 覆盖 Create/Edit 和重验的整段读取，保持原 Session/read busy 门；两表单只在当前权限、表单和输入仍有效时首次聚焦，手动刷新不重新抢焦点。修后 `focus-fixed01` 四文件 34 PASS，包含原页面/状态行为、两入口最终一次焦点、创建离页和编辑403退役控制。`build02` 一次全源 vue-tsc / Vite 实际退出0，web 输入前后保持冻结；82 个资产保存在 `dist-02`，旧 `dist-01` 不覆盖。Go/PW 方法未因产品修复改变，第二轮绑定新 Web 输入及资产、复用原 `compile-web02` binary，没有重复编译或放宽原 PW 焦点门。

`native02` 的 `TestAgentRunnerMountsWeb` 两子例通过真实 runtime-off 默认 App、普通非 Admin Owner、正式 Runner / Agent 服务与 PG：

- `runner-mounts-normal`：实际键盘打开/取消创建及编辑并恢复焦点，选择离线 Runner 和 `workspace-a` 创建；确认后独立 GetConfig / runner-mounts 同版本回读，再切换 Runner 与 `workspace-b`，最后显式清空 `[]`，版本依次为1/2/3。每次确认后及末尾显式刷新均读当前事实。浏览器1次 Create、2次 Update、0次 Lookup。26请求为24 finished及2个原GET200 ERR_ABORTED，26个原同请求联证完整，不写成全 finished。
- `runner-mounts-lost`：从正式 seed 当前配置开始，唯一 Update 已提交后由自有代理物理截断回执，浏览器进入 Unknown，仅用原 key / expected_version / 完整声明 Lookup，再独立双 GET 读取当前版本2，末尾另有显式刷新；没有重发 Update。17请求为16 finished及1个声明的回执截断，17个原同请求联证完整。

两子例合计46.46秒；原 Node、Go、driver、八个 Chromium 的 Wait、七资源双 absence、private/runtime/后代双尾、自动 baseline TCP 差额双空及输入不变均齐，外层实际退出0，完整整轮已获独立有限接受。五张原图的1440浅色/390深色可见布局、键盘首焦点/取消返回/移除后焦点及 Unknown 恢复已获有限独立接受。原件位于 `output/ai/agent-runner-mounts-ui/native02/`；原 `compile-web01`、`native01`、`focus-repro01` 及此前所有 FAIL 持续保留。

本轮真实浏览器选择的是离线 Runner；incompatible 可配置由正式合同及组件控制覆盖，不冒真实在线或不兼容协议执行矩阵。后端 setup 的一次性 Runner 凭据只留 Go，浏览器仅消费安全候选/映射与必要会话材料。生产 Project 初始化、物理 workspace 创建、非空 ExecutionCapture、默认 executor/dispatch、在线执行、完整 D10 和既有 STOP 均不因本 UI 通过而完成。
