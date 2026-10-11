# 系统 Runner 管理 UI

状态：基于 main `562d3538` 的独立实现，`native03` 两场景及全部原资源门通过，skill_package_review 已接受本片有限验收，随本次提交正式交付 main。前端 variables_review；默认 App/浏览器方法由 agent_display_settings 另域负责，skill_package_review 独审；root 负责 Git。

依据 [D15 正式协议](d15-runner-control.md#3-central-持久事实权限与管理协议)、[OpenAPI](../../../api/openapi/runner-control.json) 与 [系统设置布局](../../frontend-design/layouts/system-settings.md#5-runner-与-agent-模板)。默认 App 已有真实 Admin HTTP/Service，UI 不改后端、设备协议或项目 Owner 配置 Runner 的权限。

## 用户流程与边界

`/system/runners` 接入原系统设置：当前 SystemAdmin 分页查看设备、独立详情、创建、修改 name/description/tags；root_path 创建后只读。明确重新签发登记材料或撤销身份，之后重新 GET 当前事实。online/offline/incompatible 与设备报告仅只读展示，不表示可以执行。没有硬删除或独立 Mount 创建。

Create 冻结客户端 RunnerID、完整 request、Idempotency-Key；所有命令保原 expected_version/presence，Unknown 只显式 Lookup 原 command/request/key，不自动重放。Lookup committed 只确认历史 receipt，随后独立 GET。not_observed 不证明回滚。首次已知提交的 create/enrollment 响应才有一次性 token；Lookup/replay 的 token_available=false 不能恢复旧材料，用户必须在读取当前版本后明确发新 enrollment 命令。重签/撤销影响旧身份、token及连接，须确认。

Token 只由当前页面私有内存短暂持有，关闭材料面板、离页、checking、失权、身份/目标退役或过期时清除；不进 Session progress、持久存储、URL、日志、截图或方法原件。操作系统与浏览器内部副本不能宣称安全擦除。引导沿 Runner `--enroll` 从标准输入读取，绝不拼入命令参数或环境。

沿唯一 Session runAuthorized 及原 fetch/body/cancel 实际返回后释放；局部页面绑定身份与路由读代际。当前拒绝清可见材料，冲突保非秘密草稿，历史 receipt 不冒当前数据。非秘密未决原意图可留原 Session 查询，不跨身份迁移。

## 实现与验证

10 产品路径仅专属 API/controller/View/Editor/EnrollmentPanel 与 client/useSession/router 两文件/SystemSettingsView。四专属 client/session/state/page 控覆盖严格14字段/分页/命令绑定、实际取消尾、token退役、原 Unknown 与明确重签、冲突/权限/键盘焦点；后继实际默认 App 两场景由方法作者冻结后执行。使用现有组件/主题，同锁依赖仅物理私有复制，不下载。

既有 D15 已验提供方按原范围复用。生产 initializer/ready、物理 workspace、Runner operation/executor、Execution Runtime 与其余 STOP 保持；本片不宣称完整 D15/D27。

## 当前验证

协议/Session 首片已保存，独审指出的合法字节预算短页与 confirmed 同步退役后材料发布已实际复现并修复。原 `type01`/`build01` 可选 signal 与 transport 必填类型不匹配失败、`unit01` 短页失败、`publish-repro01` 同步退役失败以及 `unit03` 页面首焦点失败保留；焦点使用既有 UiDialog 的 data-autofocus。检查原件在 `output/ai/system-runner-management-ui/checks/`。client6/session7/state7/page4 为不同次受影响检查，`build02`（含 vue-tsc）实际通过，`dist-01` 保存本次 84 个生产资产，原 `binding01` 确认唯一 Session；当前不宣称真实设备登记或完整 D15 已交付。

首次真实默认 App 浏览器 `native01` wholeFAIL：normal/lost 均在首次 Create 的代理观察处收到 502，各为 3 请求、2 finished，cut=0、all_observed=false，未验证后续管理或恢复链。原 Go、两个 Node、Chrome 与外层均已实际 Wait，七资源双轮 absence、private/runtime/descendant 双空、75 秒内 HOST_TCP delta 双空及输入不变门完成；外层实际退出 1，不能因资源尾闭合写成业务通过。原件保留于 `output/ai/system-runner-management-ui/native01/`。

静态定位到新夹具将 43 字符 token 文本求摘要，正式 `EnrollmentToken.Digest` 则对解码后的 32 字节求摘要；已在 `72e0257e` 仅改为 `SHA256(decoded)` 后保留原内存清理，所有 SQL 关联、真实响应、敏感证据与退出门不变。原错误日志泛化，不能追认本轮已动态证明唯一内部断点。此前 `compile-web01` 缺少 updates 字段的 FAIL 与修后 `compile-web02`/唯一入口 list 成功分别保留；摘要修后 `compile-web03`/list 通过，再执行原两场景。

`native02` 仍为 wholeFAIL：normal 的浏览器场景通过，12 请求全部 finished、all_observed=true，随后 Go 最终计数报 `DATABASE_SQL_FAILED`；lost 已完成真实 Create 回执截断，但 Lookup 的计数后验被代理拒绝为 502，4 请求中 2 finished、cut=1，未完成恢复。全部原 Wait、资源及 TCP 双尾闭合，外层实际退出 1，不能把浏览器局部成功写成整轮通过。原件保留于 `output/ai/system-runner-management-ui/native02/`。

正式 schema 中 Runner ID 域和 Audit resource_id 均为 UUID；夹具同一计数查询却将同一参数用于 UUID 与 `resource_id::text` 比较。`60a479b6` 仅去掉该多余转换，保留原八项精确计数与全部业务谓词。原错误未携带实际 SQLSTATE，静态契约错误与运行首因推断分开记录。修后 `compile-web04`/唯一入口 list 已实际通过；其准备脚本首次误用旧输出目录，在 Go 启动前被原断言拒绝的 FAIL 也保留，旧原件未覆盖。Web 84 资产、Session binding01 和已验方法控继续复用。

`native03` 原两场景、Go 最终数据库后验及全部退出门实际通过：normal 完成创建、独立当前 GET、metadata 修改、明确重签、明确撤销及刷新；lost 完成真实 Create 已提交回执截断、原 ID/key/body Lookup（无 token）、独立当前 GET、用户明确新 enrollment 与刷新。原八项数据库计数确认没有设备连接或 Execution，未运行设备登记协议。网络事实保持 normal 12 请求=11 finished+seq10 GET `ERR_ABORTED`（454 字节完整 EOF/Close），lost 9 请求=7 finished+seq3 声明截断（原691字节）+seq5 GET `ERR_ABORTED`（444字节完整 EOF/Close）；对应 12/9 项联合证明均通过，不写成全部 requestfinished。真实 CDP Host、完整消费、原 key/body、SQL 提交与材料仅内存校验均保留。

本轮五张图仅覆盖实际可见的桌面创建/Unknown 与窄屏编辑/当前状态；材料展示时不截图，真实键盘/菜单及材料关闭断言沿同轮浏览器执行。原 Go、两个 Node、八个 Chrome 及 supervisor 实际 Wait，七资源双轮 absence、private/runtime/descendant 与 HOST_TCP 双空、原输入不变全部闭合，外层 `57117` 实际退出 0（supervisor 570073，133.297 秒）。原件位于 `output/ai/system-runner-management-ui/native03/`，01/02 wholeFAIL 不覆盖。本片仅系统 Admin 管理 UI，不外推真实设备接入、执行器或完整 D15。
