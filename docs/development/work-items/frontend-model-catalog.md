# 新 Vue 模型接入与平台模型用途

本轮模型类型筛选已完成Vue作者实现、定向自测及适用独立验证：ModelSelect调用方必传四值类型，用途页通过强类型映射、删除替换通过最新原模型传入；列表请求传type，服务端过滤后分页，组件隐藏错类型结果。同类型不可选原因、备注、思考策略及保存重验保留，类型变化退休旧读取并清行/搜索/游标/展开层。

作者7项定向unit、完整Vue类型检查及生产构建实际exit0。首次两项失败为首动态import/业务同计时超5秒及jsdom删除fixture准备缺失，最小调整准备与fixture后同5秒业务预算通过，首次失败不追认成功。旧三份browser fixture仅更新类型过滤/隐藏预期并通过语法与加载检查。作者构建未显式设置或记录编译期 `VITE_ACCOUNT_ENABLED`，不能据此确认部署产物启用了账号API；部署由platform显式设置 `VITE_ACCOUNT_ENABLED=true` 重新执行必要的生产构建，已通过的类型检查与unit结果复用。

独立HTTP及真实PG检查实际exit0，测试25.09秒，覆盖四类型过滤先于分页、与q/provider组合及cursor绑定类型。独立浏览器桌面覆盖四用途与四类型删除替代，390窄屏完成本轮限定旅程，实际exit0；独审已实看页面且无溢出。浏览器使用合成API，HTTP/PG使用独立真实后端夹具；两者分别证明各自边界，不合称同一真实浏览器后端E2E，也不扩大为旧全能力或维度矩阵通过。

上一轮标签增量已完成适用验证：模型选择器保留“供应商/模型 ID”主行，第二行使用非交互简标签，类型优先。Chat 按已配置能力显示 `128K`、`图片`、`文件`；嵌入显示 `1536 维` 与已配置输入长度，例如 `8.1K`；重排仅类型，图像生成补 `文生图`。未知容量不填零或无限；K 按 1000、最多一位小数向下保留，title 保留完整 tokens 数及“上下文/输入”含义，图片/文件的 title 明确输入能力。备注、思考菜单、不可选原因及草稿/Unknown 行为不变。

嵌入模型新增必填 `capabilities.embedding:{dimensions,dimensions_mode}`，维度为 1–4096 的规范十进制字符串，模式为 `fixed|request`；其余模型该键必须为 null。编辑表单提供“向量维度”和“请求指定维度”，新建默认 fixed、维度不猜测；fixed 只验证实际输出，request 另请求供应商输出指定维度，切换类型不提交隐藏旧值。新字段不保留旧 DTO fallback，也不将平台防御批次或部署环境维度冒充模型能力。

六项定向单测分轮通过，本轮类型检查与生产构建实际 exit0；简标签 `ed42b39e` 仅复验既有跨供应商/tag 单例 66549，实际 exit0，完整值 title 与非交互标签断言保留。独立桌面深色、390 浅色与原生 200%/reduced-motion 均通过 fixed/request、标签/空容量/长 ID/禁用说明、菜单及键盘焦点；最后简标签以 27446 单桌面 GET-only 再验实际 exit0、零写入，独审材料 `23e2d3c9`。没有重跑旧业务或完整浏览器矩阵。

原失败仍保留：选择器首 5 秒超时后的同预算阶段诊断通过，首超时原因未定；作者桌面标签、搜索与选择保存已走通，后续详情首行等待失败，下一次诊断截图超时未保留原观测，不追认整条作者链通过。独审首两批分别因 aria-disabled 点击方式和窄屏首焦点假设失败，仅修 probe 后通过，无产品返修。UI 浏览器使用合成 API，不能代真实浏览器后端 E2E。

后端生产修复 `d032`、独立受验输入 `63f18e59`：`TestModelEmbeddingDimensionsIndependentReview` PASS 57.16s，72887 外层与 App 原 Wait 实际 exit0。fixed 省略/request 整数、错维度拒绝及同 key 单次出网、迁移 153 严格 profile SQL 均通过；B 暂停时 A 旧 snapshot 的二维 query/read 三轮仍可用，B 三维发布后旧 profile/digest 保持。6 次 Embedding、3 次 Chat 的受控 TLS wire、Secret 释放及 App/Knowledge/runtime join 已实际断言。此前 27.30s 的 serving 漏 v3 为产品问题并已窄修，42.81s 的全 Project 退休时点为新 harness 错误并只修测试；最终通过不追认首次成功。Memory 复用作者适用门，不扩大为本次独立 App 覆盖或真实外部供应商验证。

独立链 3 容器、3 网络、4 进程均归零，6/6 基线保留且无额外资源；唯一 91,612,295 字节测试 binary 在核无消费者后精确删除，两空 scratch 目录已删，三批安全日志与清退 manifest 保留。两次私有 Vite 核身份后精确退出，原 launcher 88620、20064 均实际 143，5281 无监听且可绑定，停前子进程为零；浏览器/profile 全部关闭清理。共享依赖、正式 harness、截图与必要失败证据保留。公开服务仍为 `6d8ad217`，本轮未部署、未操作真实用户数据或外部模型。

### 本轮简标签与嵌入维度截图

下列合成数据截图均经作者与独审实看，图床上传及匿名下载 HTTP 200、字节一致。桌面为最终简标签；窄屏为同一未变的 request 表单。简化前的窄屏标签图只保留为旧输入证据，不作为新版展示。

- [新版桌面简标签](https://img.lunadeer.cn/i/2026/10/17/6ad387dfb1648.png)
- [窄屏请求指定维度](https://img.lunadeer.cn/i/2026/10/17/6ad3857126cb3.png)

状态：2026-10-17 原包起点 `97ac04e3`；统一总结模型、ModelPicker及从`90a1229b`开始的供应商列表/详情配置及模型列表动作增量已完成各自适用验证。记忆与会议共用单配置、四用途整组保存不变；此前页面与验证保留原输入边界，不替代本次结果。本卡是本包共同合同。仅系统管理员配置系统模型；项目模型、Agent、Executor/Runner与既有STOP不扩大。允许按新页面重做接口，不新增旧UI或路由迁移；当前公网实例、用户凭据和外部服务不用于开发验收。用户此前要求先交截图、不部署公网；此后已按新的明确授权将 `6d8ad217` 部署到原预览入口。当前部署、保数据证据与公网 TLS 验证限制见[恢复状态](../../../.agent-state/current.md#当前部署与恢复入口)；截图仍先上传图床再提供公网图片URL。

前轮多模态增量：现有 Chat 模型测试弹窗提供测试文本和受支持附件，明确开始后一次发送，显示有界纯文本回答与截断提示，不扩建 Meeting/Task 页面。前端 18 项定向用例分轮通过；作者两尺寸与独审桌面/窄屏、能力拒绝、Unknown 原请求、长回答滚动、附件移除回焦及原生 200%/reduced-motion 已通过。最后附件键盘移除焦点落 BODY 已修复；首大请求用例超时、首屏等待及测试夹具问题保留原事实。最终类型、限定格式和生产打包通过，首次 ENOSPC 后使用独占 `/tmp` 输出。浏览器API为合成数据，不代真实浏览器后端E2E；该轮开发验证当时没有更新公网，后续授权部署另见上方恢复状态。

独立受控真实App已分轮通过Task/Meeting/Summary（43.13s）、inline管理API（43.75s）及MCP unsupported（54.17s）。inline覆盖媒体wire、UTF-8回答边界、同key仅一次出站、严格DTO/超限/能力拒绝和撤销admin后receipt重放403。先前上传MIME、Task必需工具回复不匹配为测试fixture失败，修后通过，不追认首次全绿。

Lifecycle原case现已PASS 42.20s、App原Wait实际0，覆盖active Task附件省略保持/[]清空、冻结后继source、取消后无后续HTTP及最终reader/Model/Secret/App join。json_schema支持能力被误当实际schema请求、active Task合法更新被HTTP投影误拒的两处产品问题均已修复并验证；此前一次Task创建本地HTTP错误未复现且根因未明，保留原失败。

MCP supported最终原case PASS 47.82s（子场景42.96s）、App原Wait实际0；source==1、精确ownership与released_at、reader0/active0、Secret释放、两次模型请求及successor PNG字节均通过。SourceReads私有context/独立清理预算修复的5项单元与静审复用。保留此前5m17s超时actual2/ProcessGuard Flock、修后旧observer计数误期望Fatal/Wait1及nullable DTO等fixture失败，不追认首次通过。

真实App环境模型PNG+DOCX唯一POST已消费0600 ledger，测试FAIL 35.76s；安全回执failed/network_failed、617ms，无可验回答、两个标记未匹配且未截断。没有重发，外部混合输入未成功。有限只读诊断未发现确定的proxy/endpoint/CA接线缺陷；network_failed可能来自DNS/TCP/CONNECT或成功HTTP后的body读取，完整非2xx应为upstream_rejected。没有阶段证据，617ms不能定位，也不能认定未到供应商、未越TLS或附件不支持，原因最终保持未知，本轮不重发。受控真实链实现验证完成，不代外部混合输入成功；成功支路logout/lease SQL/app_joined未执行，不能补记通过。此前直接协议探针PNG成功、原生PDF两次400原因未知仍保留；受控链或单独探针不代表外部供应商全格式支持。

最终序列与外层actual exit1，4类nonce的容器、网络、owned临时目录祖先及原进程自动清退并独核归零；6个部署/基础设施/外来基线及原网络未动。此前PG清理15秒失败与MCP超时遗漏清理后按精确nonce/id补清的事实保留。清理和进程退出不代替未执行的外部成功断言。Vue全部浏览器已关闭，自有5281核验后TERM，原launcher实际143、无进程/子进程/监听；仅删除无消费者的独占 `/tmp` 生产产物，私人浏览器profile无残留。共享依赖、正式harness和截图保留；所有原Wait已收、无app.test进程，唯一91,514,935字节测试binary精确删除并核路径不存在。原目录12文件含binary，现保留11个必要私有log/script/已消费ledger，ledger权限0600且无凭据；工具链/cache与6个基线资源未动。部署前12:51:57 UTC的历史只读终核中，严格本机TLS/release/livez200、nonce与hash通过，当时公开服务仍为b9d且原用户数据保持；单次18443公网请求curl60/HTTP000、未取得nonce，当时多模态尚未部署。

[桌面回答](https://img.lunadeer.cn/i/2026/10/17/6ad361ade9674.png)、[窄屏附件](https://img.lunadeer.cn/i/2026/10/17/6ad361ce9fdec.png)、[未知结果恢复](https://img.lunadeer.cn/i/2026/10/17/6ad361cdaa16d.png)均为合成数据，作者和独审已实看，图床匿名下载 HTTP 200 且字节一致。

前轮增量（4107e01b后）：模型ID作为主标识、原名称改为可空备注，类型补英文后缀，容量字段标注tokens；共享选择器更多菜单编辑用途思考策略。前端22项适用单测分轮通过（7领域、8搜索选择默认行为、7Select/Menu），最终type、生产构建及限定格式通过；作者两尺寸与独立4场景加键盘补段均通过。覆盖四种策略只在显式保存时提交、同模型保策略/换模型default、备注空保存、ID层级与tokens、390触控及原生200%/reduced-motion。共享测试缺matchMedia和辅助函数类型标注已仅修测试；补段Tab进入浏览器工具栏经hasFocus/往返诊断确认无trap，修正过严断言后通过，前端产品无返修。

独立真实App输入`daff9b9b`（后端产品`31f396b3`、前端`e042a489`）：`TestModelPickerReasoningIndependentReview` PASS 35.81s，session88803及App原Wait实际退出0。真实链覆盖可空/2000 Unicode码点备注与256字节规范模型ID、配置严格DTO/幂等回放、Memory提取归纳与嵌入及两个显式false快照；Meeting使用声明的vendor:deep调用失败后，即使平台配置与旧模型能力改变，仍按原snapshot/value重试。引用更新、不兼容替换拒绝及兼容替换保留choice，撤权后fresh/replay/lookup/list拒绝均通过；实际wire为4次Agent、4次Summary、1次Embedding请求，备注不进入身份或wire，日志/Secret边界及drain/join通过。

App实际覆盖enable_thinking:false、default和effort；on与thinking_type正反开关沿作者受控TLS矩阵复用，不扩大为本次App或真实外部供应商验证。保留首compile因旧fixture的input.Name遗漏而失败、合法清空备注因事件闭集拒400的产品问题经31f修复，以及后两次独立测试UUID join/text response_format省略的误期望修正；不追认首次通过。四批各自3容器、3网络、helpers/App均精确清退，所有自有临时描述/凭据目录、唯一binary及空目录已删除，共享cache与外来持久服务未动。

上轮四类型Go review的旧DTO夹具已按notes、reasoning_toggle_mode及ModelChoice/default机械随动，未改产品或原行为断言、未重跑旧链，不扩大上述PASS范围。

全部浏览器已关闭，自有Vite核身份后TERM、原launcher实际143，进程/子进程/5281监听清零且可绑定；仅unlink本树自建依赖链接，共享依赖及公开5245未动，公网未更新。

### 模型备注与思考选择截图

以下7张均为合成数据，作者与独审逐张实看，图床匿名下载200且字节一致。

| 状态                                       | 原图                                                               |
| ------------------------------------------ | ------------------------------------------------------------------ |
| 普通列表：模型ID为主、类型弱化、备注不堆叠 | [1440深色](https://img.lunadeer.cn/i/2026/10/17/6ad354f0cedc0.png) |
| 桌面等级侧层：纯备注与等级名称             | [1440深色](https://img.lunadeer.cn/i/2026/10/17/6ad354f1a047e.png) |
| 无等级且有映射：供应商默认/开启/关闭       | [1440深色](https://img.lunadeer.cn/i/2026/10/17/6ad354f133806.png) |
| 窄屏第二视图：返回与三项策略               | [390浅色](https://img.lunadeer.cn/i/2026/10/17/6ad355163e247.png)  |
| 窄屏长备注与等级                           | [390浅色](https://img.lunadeer.cn/i/2026/10/17/6ad3551642104.png)  |
| 编辑：ID、可选备注、类型与tokens           | [1440浅色](https://img.lunadeer.cn/i/2026/10/17/6ad355174275f.png) |
| 原生200%及reduced-motion                   | [缩放检查](https://img.lunadeer.cn/i/2026/10/17/6ad3551671e84.png) |

## 1. 页面与完整流程

系统“模型配置”分类下保留“模型接入”和“平台模型用途”两个入口，系统11项改为10项，正式页面由40叶改为39叶；供应商动态详情仍属于模型接入。系统与项目设置中的共享分类名均为“模型配置”；名称调整不改变既有URL或项目功能范围。删除旧 `/system/providers` 入口，不保留兼容跳转。

| 路由                         | 任务与载体                                                                                                                                                                                                                                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/system/models`             | “模型接入”正文显示“供应商列表”小标题；页头右侧新建供应商。每行实际启用开关替代状态文案，名称链接进入默认模型Tab；右侧“配置”进入`/system/models/:providerId?tab=config`，不再仅展示已配置凭据状态。                                                                                          |
| `/system/models/:providerId` | 页头下设“模型”/“配置”UiTabs；模型Tab仅显示本供应商的provider_model_id，配轻量类型标签、可保存启用开关及并列“编辑”/“测试”按钮，可空备注保留在编辑弹窗。配置Tab直接编辑API端点、凭据及供应商属性/选项。创建供应商、模型添加/编辑、发现导入和测试结果使用对应弹窗。返回目录恢复列表位置/焦点。 |
| `/system/model-selection`    | 独立的平台用途页；嵌入、总结、重排、图像生成模型四用途平级，共用一个配置、版本与保存动作。“重新获取”位于页面标题行右侧，保存仍贴近表单；右侧选框常显“供应商名称/模型ID”，点击打开下方锚定ModelPicker。                                                                                      |

完整出口是供应商与凭据配置→模型手工添加或发现勾选导入→显式真实测试→合法用途可选。复用SettingsShell、SystemList、UiDialog和共享页头动作；目录使用宽正文，标题无需装饰描述。页级动作在标题右侧，表单保存/取消贴近字段。宽窄、空态、读取失败、权限失效、409、Unknown、取消及返回焦点在对应任务中闭环。

供应商启用开关必须执行正式Provider更新命令，不以本地状态冒充保存成功；等待、已知失败、409和Unknown沿既有version/key/receipt规则，未知时不允许重复生成新意图。已被用途引用的供应商仍可停用，引用保留、后续当前选择解析明确不可用，已持久化snapshot不改；不能把非空供应商的删除阻断误用于停用。进入详情本身不写配置、不自动测试供应商；API端点与凭据的独立命令、已确认凭据后的关联失败及恢复边界保持§2–3约定。

模型启用开关沿完整input和expected_version保存，仅改变enabled；确认回执后采用读回模型，读回失败只重读。等待、冲突及Unknown时保留行owner并禁用换页、切Tab和其它行操作，显式读取最新版本后才能采用保留的启停意图。被引用模型允许停用但不解除用途引用。仅本供应商列表省略供应商前缀，跨供应商选择器和用途页仍显示“供应商/模型ID”。编辑弹窗在干净且无未决命令时提供次要“删除模型”入口，复用既有引用/替换确认，取消返回编辑，完成返回列表。列表“测试”只打开既有测试弹窗；明确开始才按当前模型与供应商版本发请求，并遵守双启用、协议、权限及真实测试能力门，不因无凭据拒绝合法匿名配置。

模型定义按类型显示，现compatible供应商family可配置对话（Chat）、嵌入（Embedding）、重排（ReRank）及图像生成（Image Generation）。Chat固定文本基础与文本输出，可额外声明图像/文件；管理测试的受支持格式、能力门与验证范围见第4节，能力勾选不代替实际调用证明，保JSON Schema及思考能力。嵌入固定text→vector、可填context，max_output始终null；重排为查询与文档→排序结果，wire input=[text]/output=[]；图像生成为text→image。两新类四项Chat布尔能力全false、等级/结构化数组空、context/max皆null，不显示无效高级项。新建/导入可选类型，编辑已有类型不可改；切换类型不泄漏隐藏草稿。各类启停/用途选择/显式管理测试沿真实服务端资格与能力门，不等于重排检索或图像Artifact业务consumer已接通。

表单先显示模型ID，再显示“备注（可选）”；备注不作为上游身份、不在普通模型选项堆叠。上下文容量标为“上下文窗口（tokens，可选）”，不能解释成纯输入上限；Chat最大输出标为“最大输出（tokens，可选）”。无等级且支持思考时显示“思考开关格式”，可选供应商默认、enable_thinking或thinking.type，按供应商API明确选择；出现等级、关闭思考或切到非Chat时清空该映射。

思考等级采用紧凑名称/值/叉号行，窄屏不拆成上下两行；可见编号去掉，原编号label仅视觉隐藏，保留可访问名称、错误反馈与新增/删除焦点。下方“添加思考等级”不创建默认值，原name/value及空等级语义不变。新建、编辑和导入仍共用ModelFormFields，版本/Unknown/确认后只读恢复不改。

四类型与紧凑行已通过7项受影响表单/用途资格用例、type及生产构建。独立合成浏览器4项通过四类型保存/切换、紧凑等级键盘与焦点；桌面和390窄屏的名称、值、删除按钮中心差均0，窄屏两输入各122px、删除目标44px，无横溢。首单元三项因测试从组件wrapper查找Teleport选项而失败，改为沿aria-controls定位实际listbox后通过，产品未因此变更。图像测试预算新增4项回归通过：130秒等待与原请求恢复、其余默认预算、20秒/120秒回执界限及预算适用路径；预算增量后的最终type、生产构建与限定格式通过；预算以受控计时及限定测试验证，未实际等待120秒。下述前轮结果保留原范围，不替代本轮验证。

本轮独立真实App验证输入`aca5853f`：`TestModelFourTypeManagementIndependentReview`首轮PASS 23.66s，编译、原测试Wait及整session24307均实际退出0。真实管理HTTP覆盖四类API保存读回与各类能力拒例、迁移150的合法native协议与不相容类型约束（SQLSTATE 23514及约束名）、四用途持久化；5次固定TLS出站包含超过旧1MiB上限的合法base64，同key回放不二次出网、返回URL零下载、5个lease释放，撤权后的新请求与重放均403且零出站。静审和作者预算用例沿同输入复用，不将管理测试成功写成完整业务consumer、图片下载或Artifact已接通。3个精确容器、3个网络、helpers/App进程及唯一临时binary目录全部清退；外来持久服务和cache未操作，独立测试树已停止写入。

前轮两类型模型定义已完成13项定向组件/DTO/恢复用例、最终type、生产构建及限定格式检查；发现导入的合法`__proto__`模型ID草稿经显式逐项代理与浅容器修复，真实键盘操作及部分导入恢复已通过。独审1440×1000深色、390×844浅色及reduced-motion已覆盖Chat普通/Schema/思考空等级和name-value等级、重复值零写、等待禁用、类型锁定、新建类型切换、Embedding保存、取消回焦及全部保留高级项。保留首setup超时、Discovery首次修复不足与两个Embedding首列表5秒等待失败记录；后两次等待原因未定，新增观察器后原输入/预算通过，不追认产品修复。

独立Go验证已通过真实PG、持久snapshot及受控TLS调用：选中value传递、冻结快照重放、未选及空等级不发参数、多模态能力声明下纯文本可调用、实际媒体消息拒绝且无额外请求；不把此范围扩大为完整Agent装配。独立App API与Vue链亦已通过：真实创建保存trim后的name/value及JSON Schema、GET与刷新编辑读回保持，关闭思考后一次PUT推进版本并清空等级；全程无外部模型调用。首UI遗漏类型选择已作为测试夹具修正，不改产品。截图均为合成数据，未使用真实供应商凭据或调用外部模型，未部署公网；当前公网前端保持原私有冻结快照。全部测试会话/浏览器已退出，独立App/PG资源已清退；自有Vite核身份后TERM，原launcher实际143、进程与5281监听清零，仅删除本树自建依赖链接，保留共享目标。

本轮全部合成浏览器已关闭，独立真实App链无需前端服务；自有Vite经PID/start_ticks/cwd/参数和监听核验后TERM，原launcher实际退出143，进程、子进程与5281监听清零。仅删除本树自建依赖链接，保留共享目标；未部署公网、使用用户数据或调用外部模型。

本轮7张选图均为合成数据，作者与独审已实际查看，图床匿名下载HTTP200且字节一致。桌面Chat一张包含全部保留高级项；390上下段共同覆盖同一表单。前轮普通/Schema/思考无等级等图保留其原版式归属，不能替代本轮紧凑等级或四类型证明。

| 类型与状态                             | 完整截图或连续分段                                                                                                         |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| 对话：紧凑思考等级、媒体声明及高级容量 | [深色桌面](https://img.lunadeer.cn/i/2026/10/17/6ad3287b1ad40.png)                                                         |
| 嵌入：固定文本→向量、上下文容量        | [浅色桌面](https://img.lunadeer.cn/i/2026/10/17/6ad3287b1f98e.png)                                                         |
| 重排：查询与文档→排序结果              | [浅色桌面](https://img.lunadeer.cn/i/2026/10/17/6ad328823e9af.png)                                                         |
| 图像生成：文本→图像                    | [浅色桌面](https://img.lunadeer.cn/i/2026/10/17/6ad328820e1e7.png)                                                         |
| 新建图像生成：类型可选                 | [深色桌面](https://img.lunadeer.cn/i/2026/10/17/6ad3288ad15a9.png)                                                         |
| 390浅色对话：等级同排与全部高级项      | [上](https://img.lunadeer.cn/i/2026/10/17/6ad3288790985.png)、[下](https://img.lunadeer.cn/i/2026/10/17/6ad328989e970.png) |

### 可复用模型选择浮层

ModelPicker锚定在触发选框下方，不使用居中modal。调用方必须传入所需模型类型；浮层仅显示匹配类型的一层跨供应商模型列表，承载搜索、当前选中勾选及同类型模型的不可选原因，不再要求先进入某个供应商才能找模型。空选框显示“点击选择模型”，已选时“供应商名称/模型ID”始终可见，hover只改变样式、不替换内容。搜索、分页、空结果、失败重读及迟到结果由同一请求owner管理。普通行以供应商/模型ID为主，类型及资格原因弱化；有备注或思考设置时在行尾提供更多按钮，备注只在更多顶部显示且不加label。桌面侧层可左右避让，窄屏改为父层内第二视图并提供返回；Escape逐层回焦，外点不抢新目标焦点。更多内思考选项只更新用途草稿，不自动PUT；关闭浮层不清页面草稿。窄屏与缩放需保持触发关联、搜索及结果可达，正式组件接口由[共享组件规范](../frontend/components.md)维护。

四用途各保留一句功能描述：嵌入用于知识/记忆检索，总结用于记忆提取整理及会议摘要，重排用于检索结果排序，图像生成用于生成图片。重排与图像生成可以配置、选择及显式管理测试，但对应检索或图像Artifact业务consumer尚未绑定，不能以管理测试成功暗示完整业务已接通。选择只改本地草稿，单summary资源保存、409与Unknown合同不变。

## 2. 既有接口与数据

以下路径均在 `/api/v1` 下；精确字段、nullable和枚举沿[model-system OpenAPI](../../../api/openapi/model-system.json)，后端唯一维护schema。普通读取200，配置命令200返回receipt，不返回伪造的当前投影。所有写与POST lookup使用当前System Session、CSRF和原命令Idempotency-Key；业务事务再次鉴权。version及receipt计数为规范十进制字符串。

| 资源                                 | 方法与正文 / 返回                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/system/model-providers`、`/{id}`   | GET列表或对象；POST `{input}`；PUT对象 `{expected_version,input}`；DELETE对象 `{expected_version}`。对象为`{id,input,version,created_at,updated_at}`。                                                                                                                                                                                       |
| Provider input                       | `{name,protocol,base_url,enabled,credential_ref:ID\|null,options:{}}`；protocol创建后不可改。新UI仅创建/编辑`openai-compatible`供应商family，同一供应商共享base_url与credential，允许同时添加chat、embedding、reranker和image_generation模型。旧五种协议记录不迁移，在新UI只读；不再要求同一服务按模型类型重复配置供应商。                   |
| `/system/models`、`/{id}`            | GET/HEAD列表可选`provider_id`、`type`、`q`、`cursor`、`limit`，省略provider为跨System供应商；POST `{provider_id,input}`；PUT对象 `{expected_version,input}`；DELETE对象 `{expected_version,replacement:ID\|null}`。模型对象为`{id,provider_id,input,version,created_at,updated_at}`，列表项另含下述provider安全投影，详情DTO不变。           |
| Model input                          | `{notes,provider_model_id,type,enabled,parameters:{},request_overwrite:{},header_overwrite:{},capabilities:{…}}`；notes必传可空，最多2000个Unicode码点且8000 UTF-8字节，禁止NUL；只用于备注/搜索，不作模型身份。family按type选择四类既定协议，能力必须通过真实校验，不能从ID或备注猜能力。capabilities各字段严格沿schema，未知容量保持null。 |
| `/system/model-credentials`、`/{id}` | POST `{value}`；GET对象只读`{credential_id,purpose:'model',version}`；PUT `{expected_version,value}`；DELETE `{expected_version}`。写结果`{credential_id,purpose,version,deleted}`，材料从不回显。                                                                                                                                           |
| 配置receipt                          | `{kind,resource_id,version,affected_references}`，kind使用`provider.create`、`model.update`等现有点号枚举。Provider/Model/用途各有独立版本。                                                                                                                                                                                                 |
| 配置lookup                           | POST `/system/model-commands/lookup`，body `{command}`、原key→`{found,receipt}`；Secret用 `/system/model-credential-commands/lookup`，create `{kind:'create'}`，update/delete另带`credential_id,expected_version`→`{observed,result}`。                                                                                                      |

管理列表使用真实cursor/limit（后端1..100、默认50；管理页默认10，可选5/10/20/50），页长或供应商变化清cursor历史。ModelPicker用同一服务端列表做跨供应商查询，不拼接各供应商页面或把当前页过滤称为全局搜索。

全局查询`q`可省略，trim后lowercase规范化；原始与规范化值各最多128个UTF-8字节，拒绝控制字符。它按字面子串匹配供应商名称、模型备注或provider_model_id，`%`与`_`不作为通配符。返回`{items,next_cursor}`，每个列表项新增必填`provider:{id,name,protocol,enabled,version}`安全投影，不含凭据；next_cursor为string或null。游标绑定当前用户、provider过滤、type及规范q，沿既有created_at/id水位排序，不承诺跨页MVCC快照；查询变化清旧cursor并退休旧读取。可选 `type` 使用 `chat` / `embedding` / `reranker` / `image_generation` 四值，在分页前筛选，省略时管理列表仍返回全部类型。选择器必传 `requiredKind: ModelType` 并送出 `type`，不得以当前页过滤代替服务端筛选；防御隐藏错类型返回，缺失/非法类型不发请求。用途页通过强类型映射传入，删除替换使用最新读取的原模型类型。类型变化清行、搜索、游标和展开层并退休旧读取；同类型模型的停用、协议、能力和思考策略资格检查保留，保存时仍由后端重验。列表与详情均使用新的ModelInput；全局查询不改变权限边界或按备注重新排序。

配置Tab的凭据输入留空表示不发替换请求，非空才创建/替换；输入框旁提供独立“删除凭据”按钮，移除解除关联checkbox和详情重复菜单入口。无已保存凭据、旧协议只读或有未保存/未决编辑时不可发起删除。凭据写入和Provider关联是两个独立命令：已确认的凭据不能因关联失败再创建，显示实际部分完成并只恢复未完成步骤。取消不回滚已确认写入；解除关联和删除Secret不是同一事实，不能误删被其他配置引用的凭据。Provider删除不自动级联模型或独立Secret。

已保存凭据的输入框以固定圆点占位表达已配置，仅依据已保存关联元数据；实际输入值仍为空，圆点不进入草稿或命令。填写新值才替换，清空新草稿恢复占位，真实删除并读回后移除占位；SMTP及代理配置密码复用同一显示规则，账号密码表单不受影响。

此显示增量已通过现有3项定向组件用例、type、限定格式与生产构建；独审4个合成浏览器输入验证供应商/SMTP已配与未配、空保存、新输入替换、删除/移除后提示及键盘往返。代理仅核共享控件状态接线，不新增其网络矩阵；没有真实密码回传、外部SMTP或后端E2E操作，临时浏览器和服务已精确清退。[供应商配置示例](https://img.lunadeer.cn/i/2026/10/17/6ad314eb2769b.png)与[SMTP示例](https://img.lunadeer.cn/i/2026/10/17/6ad314ece0678.png)已实看、上传并匿名下载核对一致。

删除使用一次明确确认、两个独立命令：固定原credential ID，先以已保存Provider完整input和版本仅将credential_ref置null，收到匹配回执后才执行该Secret的版本化DELETE。解绑Unknown不能推进删除；删除被其他引用或lease阻挡时显示“当前供应商已不使用此凭据，凭据尚未删除”，普通关闭保留目标与继续入口，不重复已确认PUT。用户可明确结束未完成删除或确认离开；这不回绑供应商、不声称Secret已删除。删除Unknown明确显示结果尚未确认，保原key/body恢复。完整删除后读取失败仅GET，重读成功前不能关闭并丢失已确认结果。409显式重读，若原绑定目标改变则取消后重新发起，不能改删另一个凭据。

## 3. 用途、删除影响与恢复

唯一 `GET /system/model-selection`→`{id,version,configured:null|{embedding,summary,reranker,image}}`；PUT发送`{id,expected_version,embedding,summary,reranker,image}`。四字段均须显式提供：embedding与summary必填`ModelChoice={model_id,reasoning}`，reranker/image可null。`reasoning`严格为`{mode:"default"}`、`{mode:"on"}`、`{mode:"off"}`或`{mode:"effort",value}`。非Chat或不支持思考只允许default；有等级时只允许default或已声明value；无等级时仅显式`reasoning_toggle_mode`允许on/off，null映射不能冒充支持开关。模型能力中的映射必传null、enable_thinking或thinking_type，仅Chat且reasoning=true、等级空时可非null。embedding须为当前启用的System embedding模型；summary须为当前启用的System chat模型、供应商亦启用，并具备`json_schema`能力。候选按模型type、能力及当前启用状态判断，同一openai-compatible供应商可分别提供四类模型；未配置null不等于读取失败。

“总结模型”同时供Memory提取、整理、reflect与Meeting摘要/首轮标题使用。两类消费者统一解析`SummarySelector='summary'`，平台引用role为`summary`，各自的业务Purpose、提示词、用量归属和生命周期保持。系统只存一个用途配置、一个id/version和`model.selection.update`回执；删除旧独立meeting-summary HTTP/command/store/owner/锁，旧路由返回404，旧memory字段严格拒绝。Project既有`project_summary`未绑定预留契约不属第二个System配置，本轮不删除或启用。

`json_schema`是共享模型的能力门；Memory沿既有结构化输出调用，Meeting本轮只切selector，仍以`ResponseFormat=text`、空Tools生成并解析固定JSON envelope，不额外重做会议生成流程。

页面维护一份四字段嵌套草稿，同模型保留已有策略、换模型重置default；菜单选择按name显示、按value保存，默认不等于关闭。单次PUT原子保存整组，取消或无改动不写入；不再串行写两个资源，也不存在用途保存的部分成功。409保留草稿，显式重读并采用最新版本；Unknown冻结原key/body/version，lookup仅观察，须由原Execute返回匹配receipt才确认。已确认成功后的GET失败仅重读，不再次PUT；放弃未确认草稿不宣称服务端已回滚。模型身份统一显示为“供应商名称/模型ID”，本供应商列表仅显示模型ID；备注不参与身份。

历史00149仅重置System用途配置及其平台引用、移除旧独立配置表，再初始化version1的未配置单例；不自动选择任一旧模型、不保留旧接口兼容。账号、SMTP、供应商/模型/凭据及业务Purpose不因此重置。本轮仅在新建隔离库验证，现有公开实例保持。

模型删除先读 `/system/models/{id}/deletion-impact`，显示真实引用组、替代要求与`reference_adapter_unbound`等阻断。总结模型仅有共享平台引用，替代仍须为启用的System chat且支持`json_schema`，同事务替换该引用并推进整组版本；新调用使用新选择，已接受调用保持原snapshot。影响读取不授予删除，最终命令仍重验当前引用、替代模型和版本；不因旧计数为0假定删除安全，不扩大未绑定引用适配器。Provider非空、Secret被引用等失败保留真实反馈，不自动级联或解除其他配置引用。

既有配置/Secret的lookup只观察历史身份，`found/observed=true`不能证明本次body匹配，false也不证明未提交或可换key。Unknown冻结原method/path/key/body/version；用户显式恢复时必须由原Execute严格核对原材料并返回匹配receipt后才终结，不仅凭lookup显示成功。lookup可以辅助核对，不能代原Execute。Secret原材料仅留该命令owner内存，确认、放弃或身份失效后清理；不写本地存储、日志或URL。已确认receipt后的GET失败仅重读，不再次执行成功写入。409保留非秘密草稿并显式采用新版本；Secret已知未提交（not_started或not_committed）的409须清秘密输入，显式读取新凭据版本后重新填写，不能沿用Unknown的原材料恢复；权限失效清本包数据，不伪造Session注销。

## 4. 新增发现与有界真实测试

新增能力由后端真实System管理调用owner提供，不能借Project Invocation身份读取凭据。本包只开放`openai-compatible`供应商family，以Model.type派发Chat、Embeddings、Jina兼容Rerank或OpenAI Images协议；重排格式不称为OpenAI官方标准。四类共用已保存供应商端点与凭据，配置校验、snapshot/Profile与管理测试一致解析。旧协议记录只读；新增管理能力不等于完整检索或图像Artifact业务已接通。保存配置、选择模型和打开页面均不自动出网。

| 接口                                          | 严格输入与输出                                                                                                                                                                                                                 |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| GET `/system/model-capabilities`              | `{protocols:[{protocol:"openai-compatible",model_types:["chat","embedding","reranker","image_generation"],discovery:true,test:true}]}`；新建入口只有这一个family。                                                             |
| POST `/system/model-providers/{id}/discovery` | `{expected_version}`→`{operation_id,provider_id,provider_version,outcome,reason,items:[{provider_model_id}],duration_ms,created_at}`；只从已保存供应商的固定`models`路径获取ID，不据ID猜能力。失败或未知时items为空。          |
| POST `/system/models/{id}/test`               | `{expected_version,provider_version,input?:{text,attachments:[{name,media_type,data_base64}]}}`；只有Chat允许input，省略时沿固定最小测试。结果保原回执字段并必传`answer_text`和`answer_truncated`，不接任意URL或客户端Secret。 |

Chat input要求非空白文本或至少一个附件，原文本保留且UTF-8最多16KiB。附件最多4个、原始字节合计8MiB；PNG/JPEG/WebP/非动画GIF及PDF每个最多4MiB，纯文本/Markdown/DOCX每个最多1MiB，后三者明确提取文字。图片要求模型声明image，全部文件（含提取文字）要求file；只接本地文件，不下载用户URL。文件名须为非空basename、最多255字节，禁止路径分隔符、控制字符、`.`/`..`及首尾空白。最终JSON最多16MiB，预算只放宽精确模型测试POST，其它端点不变。

选择文件仅构成本地草稿，明确开始才上传；未发请求的移除/关闭可清本地材料。pending与Unknown锁住输入和移除，原owner保留同一文本、附件、key/body供显式恢复，不能清空附件后伪造原请求，也不把停止等待说成上游未执行。普通关闭、重开、身份变化和卸载清本地材料；不新增长期管理附件资源或object URL。移除当前持焦附件后焦点转到相邻移除按钮，最后一项回选择附件。

outcome为`succeeded|failed|unknown`；成功reason为null。安全reason闭集为`unsupported_protocol,configuration_changed,credential_unavailable,policy_rejected,network_failed,tls_failed,timeout,cancelled,upstream_rejected,invalid_response,response_limit,interrupted`。duration_ms为JSON整数：Discovery及无附件的非图像Test为0..20000，Image或带附件Chat Test为0..120000，通用Test读取器上限120000。created_at沿正式UTC时间。Chat成功的answer_text必须为字符串（可空），最多8KiB且保持完整UTF-8边界，answer_truncated明确截断；其它类型或失败/未知固定null、false。回答只按纯文本显示，不渲染模型HTML。返回不含原附件、认证头、上游原始body、生成图片/URL或Secret；图像管理测试成功仅证明响应结构有效，不证明图片下载或Artifact保存。一次成功不标记供应商持续在线，不替用户保证后续调用可用。

两POST均要求System/CSRF/Idempotency-Key并持久绑定原身份、body和版本。原key/body完成重放返回同一结果，**不再次出网**；未决为`COMMIT_UNKNOWN/unknown`、`retry_hint=replay`，无额外lookup。有可靠原进程退出事实时可收敛`unknown/interrupted`，不猜上游未执行。重试一次外部调用必须是用户明确发起的新操作，不能因响应丢失自动换key。

Discovery、无附件Chat、Embedding与Rerank主操作20秒；Image及带附件Chat总预算120秒，响应头等待不超过总预算、读取空闲30秒；真实I/O结束并join后有界5秒持久/lease收尾。这两类Test前端使用130秒等待余量，其它模型操作仍30秒；停止等待和Unknown原请求恢复不变。全局最多4个、不排队，满额`RESOURCE_BUSY`。发现响应最多2MiB、1000个ID，超限明确失败而非假完整目录；普通测试响应最多1MiB，Image原始响应8MiB、解码结果6MiB，不回传图像。只允许已保存配置派生路径，拒绝redirect；复用统一出站DNS/当前规则/代理/TLS，不读取环境代理兜底。实际网络期间当前Session/admin与Provider/Model/Secret版本由共享锁保护；关闭/取消需实际停止并join，界面不把取消请求当上游未执行证明。

同一供应商发现一次，勾选导入时逐模型明确四种合法类型之一及对应能力，再使用现有`model.create`逐项串行，单批最多100项；每项独立key/body/receipt并显示已成功、失败、未开始或Unknown。不重复成功项，不把部分完成写成整体回滚；取消仅停止未开始项。导入前明确合法类型和能力配置，不从发现结果制造上下文、工具或推理支持。

## 5. 实施与验收边界

account_backend唯一维护后端/API/schema及00147、00148、本轮00149；proxy_copy唯一维护新Vue；本卡、蓝图和当前摘要由account_contract维护，root负责Git。测试、视觉与安全由root指定owner独立分工，不把作者检查写成独审。

供应商模型列表动作已落地：仅本页省略供应商前缀，类型标签、真实启停、编辑及显式测试沿第1节合同。新增4项开关与2项编辑恢复回归、最终type、生产构建和限定格式通过；独审桌面/390的启停失败与Unknown恢复、嵌套删除取消、编辑取消焦点、保存后列表读取失败保新版本测试均通过。静审的receipt待读离开保护、canonical先采用及实际取消焦点问题已修复；首轮合成列表缺provider投影和测试声明类型问题保留，旧harness只做入口适配和加载检查。未新增真实后端E2E、外部模型调用、reduced-motion或公网结论。

[模型列表](https://img.lunadeer.cn/i/2026/10/17/6ad319070f921.png)与[编辑弹窗](https://img.lunadeer.cn/i/2026/10/17/6ad3192a9f03e.png)均为1440×1000深色合成示例，已实看、上传和匿名字节校验。所有浏览器与自有临时服务已退出，原launcher实际143、进程和监听清零，共享依赖与公开服务保持。

凭据删除入口后续调整已完成：输入框旁的独立按钮替代解除关联checkbox和重复菜单，真实两阶段删除合同见第2节。新删除5项、受影响旧组件12项、最终type、限定格式与生产构建通过；作者两尺寸正常/取消/确认后读回失败检查与独审Unknown、部分完成继续/结束、无凭据及只读门通过。最终桌面输入与按钮中心差0，390无横向溢出；[配置示例图](https://img.lunadeer.cn/i/2026/10/17/6ad31187aefc9.png)已实看并上传、匿名下载字节一致。

原type声明、取消焦点及按钮对齐失败经限定修复；独审同名关闭按钮定位问题修正，最后窄屏首load失败原因仍未定，同预算单次诊断复验通过。全部检查使用合成数据，不新增真实后端E2E或reduced-motion结论；浏览器与自有临时服务均已退出、进程和监听清零、共享依赖保留，未更新公网或用户数据。

供应商列表/详情增量起点`90a1229b`，Vue唯一实现树`/workspace/agenteam-provider-settings-vue`；account_backend只读确认现API足够、后端无改动。基础`4eafeeee`、修复`268d27eb`及最后窄屏CSS组合的适用验证与运行清退已收口：

- 作者1440深色/390浅色正常链通过开关、配置入口、跨Tab草稿及空凭据仅保存Provider；8项受影响组件用例分轮通过，旧provider4项通过。最终逻辑type、最后两CSS后的build及限定格式/CJS语法通过。
- 常驻inline在open不变时的props同步问题经修复静审关闭；成功后相同baseline回传清提示的问题加最小guard，回归覆盖真实父级shallowRef反馈及后续version续存。编辑放弃后、开关保存/恢复后的焦点落BODY已定向修复，独审两原case通过；自有焦点交接不抢已外移焦点。
- 独审另两Secret Unknown/390只读场景已通过；最后390 Unknown反馈区溢出仅以局部wrap与max-width/box-sizing修正，同停点复验root390、main scroll/client均378，已实看。所有独审浏览器已关闭；未以旧正常布局或旧原型代替新增失败态验证。
- 首轮组件测试的JSDOM缺dialog.close、错误期待Unknown409自动解锁以及raw/reactiveproxy宿主偏差均保留，限定修正后分轮通过。API未改，本轮没有运行真实App/PG；旧后端证据沿原输入复用，catalog-ui的语法/格式/--list结果不称新的E2E通过。

最终两张1440×1000深色合成示例已实看、上传并匿名下载200且字节一致，密码字段为空：[供应商列表](https://img.lunadeer.cn/i/2026/10/17/6ad308b112151.png)、[配置详情](https://img.lunadeer.cn/i/2026/10/17/6ad308b192f95.png)。此前截图保持其旧版本归属。作者自有Vite5281按PID/start_ticks/cwd/参数核对后TERM，原launcher实际退出143，进程消失、无监听且停止前无child；仅unlink自有node_modules链接，保留共享目标。全部作者/独审浏览器已关闭，公开5245未操作；本轮不部署公网。

### 已完成的ModelPicker验证

ModelPicker后端生产`d95dffb2`稳定；Vue最终`1b8bd205`包含分页重试回焦修复`575db175`，前一主树产品集成`82ff92d6`。独立工作树为`/workspace/agenteam-model-picker-backend`、`/workspace/agenteam-model-picker-vue`、`/workspace/agenteam-model-picker-tests`；组件规范由Vue owner同步。适用验证及隔离资源清退已完成：

- 后端定向unit、HTTP/OpenAPI route通过；唯一真实PG搜索检查通过跨供应商三字段、字面%/_、游标过滤/水位、安全provider五字段、System权限及Project隔离，实际Wait/helper退出0并完成专属清理。两轮位置struct字面量编译失配及首helper缺AGENTEAM_GO、尚未RUN的失败保留，具名字段和环境限定修正后通过。
- Vue新共享7项、页面13项、独立搜索1项及原共享/q14项分轮通过，基础实现type/build通过；575焦点增量后最终type通过、未重复build，具体复验见下一项。搜索case曾多次超时；实际阶段材料显示页面动态加载4415ms、5秒预算内尚未进入fakeTimers。后将fixture准备移入限定beforeEach hook，业务预算仍5秒，断言保持；不称原整体计时边界未变，也不把原失败归为已证fakeTimer死锁。
- 作者两尺寸受控浏览器通过搜索、选中、Escape、不可选原因和单PUT，四图已实看；首次带可选副标的heading定位失败仅修harness。独立视觉已通过桌面深色/390浅色、触控长名、浮层翻转及内部滚动、删除嵌套复用、Tab/ShiftTab/Escape回焦与原生200%/reduced-motion。查询命中数和滚动方向的初始测试假设已纠正。实发现的分页500重试后按钮移除、焦点落BODY已修：重试前仅在焦点仍属panel时同步移到搜索，不新增异步抢焦；定向2项通过、6项未选及原视觉case复验通过，最后type通过，未重复全量build。几何未变部分复用原冻结输入截图。
- 真实App只读API case actual0，覆盖跨供应商分页、规范q、安全投影、游标绑定及匿名/普通权限。真实UI同样actual0，两个供应商共13模型超过10项页长，真实q/cursor搜索与翻页、选择只改草稿零PUT、唯一保存版本加一、刷新保持及替换持久化通过；两case均严格TLS/nonce、finally logout204→401及浏览器关闭，零模型外呼。
- 真实UI前四轮失败保留：两次`response.json`出现CDP No data，第三次裸`response.finished`等待挂起，第四次搜索/分页/草稿已过后被旧“图像模型”标签断言阻断。Playwright1.56的requestfailed不结束finished已确认，但具体请求/CDP detach根因未证；后续改用状态、准确q/cursor、UI、原生GET及单PUT持久状态核验并同步“图像生成模型”标签，未改产品/TLS或放宽预算。最终通过不倒推前三次原因，也不写首次整轮全绿。

三张合成数据图已实看、上传并匿名下载校验一致：[完整用途页](https://img.lunadeer.cn/i/2026/10/17/6ad2f91336676.png)、[桌面展开浮层](https://img.lunadeer.cn/i/2026/10/17/6ad2f91320396.png)、[窄屏选择](https://img.lunadeer.cn/i/2026/10/17/6ad2f9136cb12.png)。截图属于受控视觉，不替代真实API/UI结果，不代表公网部署。

运行owner已精确清退App、外链和原launcher，实际退出均0；4容器/4网络/4目录均不存在，5282/5283无监听，App二进制经SHA与零消费者核对后删除。Vue5281按精确身份TERM，原链退出143、进程及监听归零，只unlink自有依赖链接，共享主树依赖保留。私有安全结果/原失败材料保留不入Git，用户公开5245及既有数据未动；以上不扩称全仓全量检查通过。

### 已完成的统一总结模型验证

统一总结模型产品集成`846fe701`（backend `b87ed09b`、Vue `0852da63`、harness `5edd81b1`），后续产品输入未变，适用验证已收口：

- 作者contract、Model分轮及Memory/Meeting单测通过，tests/model integration编译通过；源码静审缺口与00149 summary CHECK独立复核已关闭。原route测试、schema环境与既有media fixture失配保留，修后仅按受影响结果记录，不称首次全绿。
- Vue受影响18项单测分轮通过、type/build通过；导入结果裸模型ID的P2已改用共享斜杠名称，局部type及原受控case复验通过。有限独审覆盖四用途单GET/PUT、Unknown、390长ID及焦点；这些使用受控输入，不代真实API或数据库验证。
- 真实API与两条Vue链通过单summary的id/version/receipt、同key重放不递增、旧memory字段400、旧meeting-summary GET/PUT 404及唯一共享引用替换。用途页四项单PUT/版本加1、刷新后斜杠名称、无`json_schema`候选不可选及取消/替代删除通过；目录受影响的引用、发现导入、两adapter测试、失败与原key恢复通过。浏览器均finally logout204→401并关闭。
- 独立两Session并发保存只有一成功、另一RESOURCE_BUSY，版本只加1；不同body复用key409、撤销Session401及用合法新版本恢复原配置通过。此为本轮独立并发/权限门，不扩大为所有模型消费者全矩阵。
- 作者真实PG验证五个业务Purpose共用模型/版本、单引用替换及Audit/Event回滚、CAS/Origin/CSRF/同事务Session、schema与旧接口拒绝；MemoryOrigin初次迁移准备超时，test-only启动预算从20秒对齐正式30秒后精确重验通过，不倒推原失败原因。Unknown committed/rollback通过，正则额外选中的different-after-rollback也通过，单列为额外执行范围。
- App MemoryTools真实retain/recall/reflect→Meeting摘要共11次调用，均退役并释放租约。Cold首次停于DATABASE_HEALTH_FAILED、未进入业务且原因未明；同binary/输入/预算在其它隔离运行清退后唯一复验通过。实际kill/Wait后保留summary current_source/binding及未发布版本，不重新Resolve/dispatch或伪造输出，owner取消并完成join；不称自动恢复生成成功。

上述为分轮、限定范围的实际结果，不称首次全绿或全仓全量通过；只验新建owned数据库，不承诺含旧绑定数据的原样升级。既有Project未绑定端口、旧协议STOP和外部服务范围不变。

本轮runtime原链exit0，App退出并join；4容器、4网络、4专属目录均确认不存在，5282/5283无监听。作者PG/Cold专属资源亦精确清净；Vue5281 TERM后原链exit143且端口释放，仅unlink自有依赖链接。最后App测试二进制经SHA和无消费者核对后删除，必要私有材料留存，不入Git。用户公开服务、数据及外部SMTP均未操作。

两张当前示例图已实看、上传200并匿名下载核对字节：[四用途页面](https://img.lunadeer.cn/i/2026/10/17/6ad2ed1e4c216.png)、[模型选择器](https://img.lunadeer.cn/i/2026/10/17/6ad2ed1e40ef9.png)。仅用于页面交付，不代表公网部署；用户公开实例保持。

### 此前模型接入与页面调整结果

以下结果均属此前冻结输入，不能据此宣称新selector已完成或已部署。

首链为供应商/凭据→模型→测试→用途保存与刷新；并检查部分完成、权限/CSRF、409、Unknown原请求终结、引用阻断、能力不支持与取消。发现/test使用任务拥有的模拟供应商和真实App/Secret/出站实现，不用用户现有凭据或外部供应商实发。页面核桌面/窄屏、键盘/modal回焦、返回列表与部分结果；现系统导航10项需要新检查，旧11项通过事实保留为历史。新family已完成下述真实链；先前冻结输入的权限/生命周期等未变结果仅限定复用，不把旧协议整页或旧配置链当作新family验收。

最终主树集成`65e61bd4`，生产输入backend `e48dceaf`与Vue `2464e966`，运行harness为`c798f9b4`；结果按各自冻结输入和分轮范围记录：

- r3严格CA管理API通过发现、chat测试、同key终态重放零重复、429安全失败及成功后丢浏览器响应的原key恢复；独立权限probe通过普通403、无CSRF403、异body409、撤销Session401，对供应商调用增量均为0，自有对象和Session已清理。
- 真实Vue两case分轮通过：同供应商chat/embedding、凭据与配置/手动编辑、混合发现导入、首项受理后停止未开始项并继续、两adapter测试、429和丢成功响应后同key恢复；用途保存刷新、两处引用的取消及替换删除均有持久状态核对。各case finally logout204→401、浏览器退出。用途有效通过所在外层因目录定位器失败exit1，目录限定修复后单跑通过，不写成初次整轮全绿。
- 最后作者组合门采用生产`e48dceaf`/取消测试`271b68`、PG17.8，实际PASS11.06s/外链exit0：同family两type与SQL约束、引用删除409、带凭据认证代理真实调用及原key零重复、规则拒绝零hit；真实HTTP取消后caller/handler/socket join，持久cancelled可原key恢复，Model/Proxy租约归零，删除模型/供应商/凭据后历史receipt仍返回。此为作者组合检查，非新增race或独立全矩阵；其中embedding创建不单独代表调用，实际embedding调用由前述API/UI链证明。
- Vue最后13项受影响单测、格式/type/build通过；之前17文件86unit及字典定向仅按原输入复用。秘密材料closure与known Secret 409两合法状态处理已修并独审关闭；有限视觉复核包括窄屏横溢、列表滚动330→330/回焦、409与Unknown。

原失败保留：r2启动前依赖失败尚未归因；管理harness初次缺正确CA，补CA后未降TLS；UI关闭按钮定位器歧义经限定修改后通过；旧PG16 setup失败未进入业务。没有使用用户凭据或外部供应商，不宣称所有供应商协议、全部项目/Agent执行或公网已验。

r3原链actual exit0，App/TLS/供应商join，4容器+4网络及4专属目录清零；风险fixture另6资源/3目录亦清零。Vue5281 TERM后原wait143且无进程/监听，5282/5283无监听；5288仅确认进程/监听已无，旧launcher wait不可恢复。两个测试二进制按原SHA删除，必要私有证据保持0700/0600、不入Git；既有公开服务和用户数据未操作。

前轮四张示例图均由视觉owner上传并匿名下载核对；旧分区、五用途单保存及名称调整图仅保留对应历史输入，不作为本轮四用途页面证明。截图证明页面呈现，不替代真实API验收，也不代表公网已更新：

| 页面                       | 原图                                                               |
| -------------------------- | ------------------------------------------------------------------ |
| 供应商目录                 | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d9385b204.png) |
| 同供应商聊天与嵌入模型     | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d9385c771.png) |
| 发现与导入                 | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d938aa5e8.png) |
| 前轮深色平台用途（旧分区） | [查看原图](https://img.lunadeer.cn/i/2026/10/17/6ad2d938569be.png) |

五用途单保存增量以`09efa870`为起点，仅修改页面和对应单测，后端不变。定向15/15、vue-tsc、Vite build及限定格式通过；既有正式spec适配通过语法/格式与列表加载，未重跑真实PG。独审4个受控实际Vue场景通过：双dirty单保存串行、第二阶段400后仅续写会议摘要、Unknown同key/body恢复且不重发成功四用途、四用途未配置时仅保存会议摘要。1440/390浅深、焦点与reduced-motion实看无返修；这是受控UI验证，不新增真实API或公网结论。

临时5314经精确进程身份核对后TERM，原launcher actual143，进程消失且端口无监听，浏览器已关闭。新[五用途单保存深色原图](https://img.lunadeer.cn/i/2026/10/17/6ad2e10dcc7bb.png)为1440×960示例数据，已实看、上传200、匿名下载200且字节一致；本轮仅交截图，不部署公网。

后续名称与页头调整起点`e36992d8`：系统/项目共享分类“模型配置”、页面及导航“模型接入”，用途“重新获取”置于标题右侧，URL/API与保存逻辑未改。作者15项受影响单测、type/build和限定格式通过；独审1440深色/390浅色两场景通过页头同行、名称/返回、键盘与重读两个GET各一次/零写入，未重跑保存、Unknown或真实PG。当前[用途页头与命名示例原图](https://img.lunadeer.cn/i/2026/10/17/6ad2e47c1752d.png)已实看、上传及匿名下载200且字节一致；此前用途截图仅保留对应输入，不作为当前按钮位置依据。临时5314精确TERM后原launcher actual143、进程消失且无监听/子进程；本轮不部署公网。

精确恢复状态与待办见[当前状态](../../../.agent-state/current.md)。
