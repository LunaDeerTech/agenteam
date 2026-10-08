# 下一完整结果建议：Owner 项目入口、工作台与基本信息编辑

推荐先完成这一张 D26/D27 用户结果卡：当前用户从正式项目列表进入自己的工作台，沿 `/{username}/{project_name}` 直链、双导航与基本信息页读取并修改名称/描述，原请求失联后能安全查证、显式重放或放弃追踪。它真实消费已接受 List/Get/Resolve/Update/lookup，不是空壳或单函数卡，也不等待 Audit 产品。现在仅建议，未授权规格、产品或资源执行。

固定产品为 root 确认的 `cc850b2244cad771eb862a2c82d99887d5da7284`，配置归档 `b91cb89f0575cf7f54faa5d253b5571d7435b670`。有限25来源的实际 SHA 与读取边界见 [inputs.json](inputs.json)；没有读取 Audit 活动业务源或通过 Git 自验远端。此建议不新增产品决定，不重问已决 Summary。

## 依据与完整结果

开发计划 D27 明定“系统项目列表/项目工作区 → 三类设置”。现有 `web/src/router/index.ts` 只有账号、个人与 System 页面；`router/auth.ts` 登录返回目标仍为静态闭集，尚无 Project API、路由或项目草稿所有者。因此先补该接缝比把 Provider/Model 编辑器直接挂在孤立 URL 更完整。

| 已验依赖 | 本卡实际消费 |
| --- | --- |
| Owner read `901eb546` | 当前 Human Owner 列表、详情、分页及 lifecycle 安全投影；管理员无跨 Owner 豁免。空安装合法空列表，不伪造项目。 |
| Usage root `03a4a0b8` | 只消费正式名称 resolve；得到稳定 ID 后，各资源请求仍独立授权。不是 Usage/Invocation 生成。 |
| Owner Update `61bed1fc` | 名称/描述 PATCH、原 `update` lookup 三态，实际默认根与历史/current 区分。 |
| 已验 Account/前端框架 | 当前 Session/CSRF、同 Cookie 协调者、严格 transport、实际 read/cancel 完成、SettingsShell 与公共 UI。 |

交付列表分页/空态/失败恢复，项目首页空 Dashboard，项目设置默认基本信息（ID、当前 Owner、时间只读；name/description 保存/取消），有效直链/返回与窄屏双导航。只显示本卡真实可用导航；不制造 Meeting/Task/Knowledge 功能页。Project 创建、Archive/restore/delete 没有已验默认根，故没有这些按钮、进度承诺或 API；归档/处理中只展示已返回事实、普通表单只读，不把未完成 lifecycle 当已停止。此范围是既定布局的可运行切片，不声称完整 Project 设置或 D26/D27。

实施前规格重点核动态登录返回闭集与重新 resolve/授权、稳定 ProjectID 和名称重用隔离、Session/Project/generation 与实际请求尾部。PATCH 保存原 key/body/presence/version，允许原 no-op；区分 Update 三态 lookup 与 Model/Secret 两套语义，历史回执不覆盖当前 GET，改名后按稳定 ID 重新核当前路径。取消/放弃未知不等于回滚，不自动换key；严格 DTO/字符串版本/完整页与 endpoint 响应预算不能复用600000 B通用界后裁剪合法 Project 页。这些沿已有契约，不增加产品规则。

## Model/Summary 与其他候选

| 备选 | 已具备与未具备、排序理由 |
| --- | --- |
| Project Providers/Models/凭据管理 UI | 五读、六写、lookup、Credential metadata/create/update/delete 和安全目录已验，配置管理可以成为下一完整 UI 卡，不需 Invocation 或 Audit。当前缺本推荐的 Owner 工作区/安全路由基础；若一次并入会同时引入 Project Update、Model 与 Secret 三套不同恢复规则，扩大共享 owner 风险。后继表单遵守真实空 options/parameters/overwrites policy，凭据不回显、不假造 credential 列表；引用中的 Model 不能靠 UI 假造 Agent replacement adapter 或不存在的 Project deletion-impact。可交付已绑定配置范围，不能宣称全部模型运行管理。 |
| 项目 Audit UI | 等当前 Audit 完整产品接受及 Owner 工作区后，可复用安全查询；当前规格接受不是运行依赖，不能消费活动实现。 |
| 再做 Summary 设置 UI | S2 已完整接受管理员统一 initial/update（含首轮标题）设置及恢复，无需重复；Project 不增 selector/override，也没有另一个已验 Project Summary 读端点。 |
| Summary 真实生成、Invocation/Skills/Project 创建生命周期 | S3 仅库，生产 Resolution/Invocations、D24 与必要 canonical/runtime/Skills 初始化仍未绑定；不能把受控 fixture 当前事实或配置成功冒作真实消费。没有发现本次可安全越过这些依赖的更强完整结果。 |

## SPA 停止的精确范围

保留的停止原记录是 `docs/development/agent-team/recovery-2026-10-06-continuation.md:312`（§26），并在322复述：**concurrent-publication 被平台内容安全机制终止，无run目录/执行证据，stage02竞态未关闭，脚本返修/发布暂停，不重试、改写或转交。** 这是主线程当时通知的持久原文；不能补造被阻探针的 raw 或动态结论。

其正式16路径由 `d28-central-spa-hosting.md` §2/6界定：`scripts/build-central-web.mjs`/测试、`internal/central/webassets/**`、`app/app.go`/`webassets.go`、`agenteam_web` embed构建、共享bundle/staging/`dist/agenteam`/摘要、Central原生托管及对应process/browser探针。新建议不改、不执行这些路径或链，不能重做并发发布测试。候选22路径与该16路径没有重叠；前端源码是将来发布的上游输入，不代表发布缺陷已解决。

验收沿已接受 S2 `system_meeting_summary_web_fixture_test.go:125–161,209` 的独立 loopback 同源静态 harness + 真实后端 API 模式，以及 `web/package.json` 的普通 Vite生产构建，产物固定在任务独占目录，单作者/单读者窗口冻结，不发布 Central二进制，不调用上述脚本/tag，不碰共享bundle或dist/agenteam。现有 System 页面也在此边界内验收。新直链刷新只证明这个 harness 下 Vue/客户端行为，**不证明生产 Central History fallback 或正式部署**。旧浏览器回归若硬读共享 web/dist，必须另外冻结/交接/还原已验单窗口资产方案，不能自行并发交换；新卡不修其发布机制。若正式交付要求嵌入 Central 发布、生产托管或必须修复该stop才能运行，则这部分仍 blocked，交root处理，不以“新UI”命名绕过。

## 所有权与验收建议

候选21技术＋前端README末件共22路径见 [candidate-paths.json](candidate-paths.json)：新增 Project API/composable/ProjectNav/五视图、三纯测试、两浏览器入口及两Go私有fixture源；窄改 client、App、router/auth/index、原认证/Session测试。没有后端生产、Account root、schema、共享fixture/脚本、依赖锁或发布文件写权；正式卡形成前只作为唯一作者的候选清单。

与 Audit 源无相同写路径，但 tests/account → app/account.go 的真实编译/运行依赖相交。前端静态/纯测可在自身冻结上独立准备；默认根/Go/browser图必须等 Audit 停写并由root选定完整接受基线或明确隔离固定 cc850b22，不把活动源码当输入。真实资源严格单窗口，新 live PID/starttime/Docker/TCP 基线；不复用历史shim数。

验收至少覆盖：普通Owner/管理员非Owner/撤Session、分页全部状态与旧名重用、真实修改/重名/版本冲突/no-op、断响应后同原意图查证与显式重放、迟到响应/卸载/换项目/Logout实际owner尾部；桌面/窄屏/键盘焦点/dirty取消/深浅色/reduced-motion。私有 fixture 用正式身份链与原正式 Project库、明确持久Skills等测试前置，不新增生产创建成功。实际安全 HTTP body 绑定 target/status/header/run 后走公开client与标准schema；新共享App/router范围回归原认证/个人设置/System表单及 Summary 双草稿代表。

工具/依赖/命令另在正式卡固定：离线图与必要编译沿45秒/subreaper实际wait，真实fixture沿完整20包/4容器3网络、新top120秒含Cleanup/包6分钟；浏览器分组按原既定budget冻结、不为整合随意延长。最后适用完整npm check和未参与实现者真实风险验收；所有owned实际wait、PID/TCP及七资源双清、daemon未wait差量分别记。现在没有执行其中任何检查。

无新用户产品问题。建议 root 采纳方向后再起草有界正式规格；本建议不启动三停止、不完成 D08/D09/D24/D28/E01。
