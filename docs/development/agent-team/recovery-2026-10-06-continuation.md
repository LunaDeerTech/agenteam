# 2026-10-06 恢复续接

本记录固定本次从 `125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b` 恢复时的 Git、源码、持久证据与派工事实。[此前恢复记录](recovery-2026-10-06.md)及[任务台账](tasks.md)旧段落保留当时状态；其中的实例名、临时路径和“实施中”不表示旧实例或未提交候选已经恢复。本页是恢复交接记录，不是新增产品接受报告。

## 1. 本次 Git 与运行目录

- 主线程初始核对为干净的 `work / 125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b`；实际 `ls-remote` 返回同一哈希。主线程随后 fetch，创建跟踪 `origin/main` 的本地 `main`，没有未推送提交。
- 文档负责人只读复核：`HEAD = main = origin/main = work = 125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b`，`git status --short --branch` 仅返回 `## main...origin/main`，`git rev-list --count origin/main..HEAD` 为 `0`。该干净状态是本次作者开始写入前的快照。
- `/workspace/scratch` 存在且初始目录为空。旧 scratch 中的 driver、运行资源或未提交源码没有随本次恢复出现；不能将旧清理记录当成本次 Docker、进程或运行目录检查。本轮文档任务没有启动业务进程或占用测试资源。

## 2. 已提交结果与本次离线复核

本次 `recovery_verification` / verification_worker 在固定 `125e3c2` 上读取归档核验脚本后执行只读检查，并将相关源码与固定 Git、当前工作树及原清单逐字节比较。以下结果复用原业务验收，没有重跑业务测试。

| 已提交完整结果 | 本次离线结果 | 保持的接受边界 |
| --- | --- | --- |
| [Invocation/Usage ledger rev1](invocation-usage-ledger-verification.md)，源码 `36e5ff1` | 核验脚本 exit 0：188 档案、185 原件、16 输入；20 交付源及 13 项组合上游匹配。原日志解析仍为 final01 的 11 顶层 PASS、combined01 的 3 顶层 PASS | 账本库及 00018 schema；生产 Facts/Runtime、发送重试、Usage HTTP/default root 未绑定，完整 D09 未完成 |
| [System Model 管理读口 rev1](system-model-management-reads-verification.md)，源码 `ecd7337` | 核验脚本 exit 0：77 档案、71 原件；input01–05 的源数依次为 9/9/13/13/13，可按证据重建。最终 13 源匹配；原独立日志为 2 顶层 PASS | 当前凭据 metadata 与有限精确删除影响读口；管理 UI、真实外域 reference adapter、Provider 调用未交付，预览不授予删除权限 |
| [公开 Account 入口 rev1–rev5](public-account-entry-verification.md)，源码 `787a5c7` | 核验脚本 exit 0：438 档案、854 逻辑原件、16 历史输入、333 映射；25 源匹配固定 Git 与当前源码。15 项 dist 仅核归档哈希 | 邀请兑换、找回申请、密码重置及卡内共享认证/设置结果；没有新增 SMTP 实投、原生浏览器缩放、生产 SPA 或真实 Vite proxy 结论，完整 D26/D27 未完成 |

复核所用最终清单 SHA-256：ledger input13 为 `b0dbeefe78312c2b2ca81f7e6de80fc8cde6b4f3354cfd3d6c7497a847b3009e`；管理读口 input05 为 `0208b2dcdce8623f65f2d7c2364a3daf083994f6271aeb63c02ca6fba7915a27`；公开入口 input13 为 `f824cf28b414a2b41276ae383342fd9bb861987784623929b0e35690ceff9fd4`。现行 dist 没有实测；旧失败、分版本证据组合及原资源观察限制均保留。

验证负责人实际命令如下，工作目录均为 `/workspace/agenteam`，三条均 exit 0；文档负责人没有重复执行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/invocation-usage-ledger-verification-evidence/verify-evidence.py
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/evidence/system-model-management-reads-verification/verify_inputs.py
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/public-account-entry-verification-evidence/verify_archive.py --candidate-root /workspace/agenteam
```

## 3. 旧 tools 与 Artifact 候选的精确路径核对

文档负责人按以下已提交清单逐项只读检查当前路径、`HEAD` Git 对象、原业务基线和候选文本哈希，没有将归档复制回源码树或运行保存的测试、driver、probe。

| 候选与精确清单 | 当前源码树与 HEAD | 持久归档 |
| --- | --- | --- |
| OpenAI tools：[14 路径清单](openai-chat-tools-wire-verification-evidence/candidate-files.json)，业务基线 `ecd733711caff5df46e423cadab52b32c34f785e` | 10 个新路径全部缺失；4 个旧路径逐字等于业务基线。14 项中没有当前源码匹配旧候选 | 14 份候选文本仍在，逐 SHA 与清单相同；清单 SHA-256 为 `1da8b486447a58f8f9ae5ffdc6ff81947a4f8f462ae98403f32c80c63c20084a` |
| Artifact：[16 路径清单](evidence/artifact-project-stop-verification/candidate/manifest.json)，业务基线 `6658a6cb1f29299521773bc8dc86b2f607b8c809` | 11 个新路径全部缺失；5 个旧路径逐字等于业务基线。16 项中没有当前源码匹配旧候选 | 16 份候选文本仍在，逐 SHA 与清单相同；清单 SHA-256 为 `79e5eb812c0fda2a42d390a7e2703b1ebca4ce581c055e1c2a99fda1ab76914e` |

tools 的旧四源是 `internal/central/model/adapter/` 下的 `openai_chat.go`、`errors.go`、`sse.go`、`transport.go`；缺失源为清单中 `baseline_git_blob = null` 的全部十项。Artifact 的旧五源是 `internal/central/artifact/` 下的 `service.go`、`create.go`、`upload.go`、`publish.go`、`commands.go`；缺失源对应[原卡 rev3](../work-items/recovery-artifact-project-stop.md)的三项新生产、三项新单测和五项新集成测试。精确路径以各清单为准，不把文档归档路径视为产品源码路径。

验证负责人另已审读并执行 `env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/openai-chat-tools-wire-verification-evidence/verify_archive.py`，exit 0，核对 174 档案、167 原件、14 候选、9 历史输入及 117 映射。这只是[有限 tools 证据](openai-chat-tools-wire-verification.md)的离线完整性检查，独立动态仍为 **NOT RUN**，产品候选未恢复、未提交、未接受。[Artifact review09](artifact-project-stop-verification.md)继续只保留本域有限结论；共享 guard、更强恢复与完整卡仍 **BLOCKED**。

## 4. 保持停止、待定与未完成的范围

Object Runtime join 原修复任务及 `tools-independent-01` 原独立任务都曾被自动安全筛查以 **“possible cybersecurity risk”** 中断，继续保持停止；本次不重试、改派、换方式重建或执行其保存的探针。Object [已知 join 缺陷](object-runtime-join-regression.md)没有修复，Artifact 最终共享 guard/完整卡与 Project 领域绑定仍受阻；tools 原独立动态未运行，不能写成已执行失败或独立 PASS。

Summary 创建初值已由主线程重新询问，截至本记录没有用户答复，仍为待定。Anthropic Messages [规格接受](anthropic-messages-wire-spec-verification.md)仅限静态规格与来源，tools 产品前置未满足，其 19 条候选实施路径没有因恢复而获得写权。

生产 Project、执行与绑定、Artifact/download HTTP、Runner 传输授权/协议、Model Resolver/Invocation/Usage、真实 Provider/MCP 调用及其余后继能力没有因离线核验完成绑定。`/readyz` 503、`ready=false` 的既有边界保持；本轮没有启动进程重测健康接口。完整 **D08–D28/E01 尚未完成，E01 尚未开始**；E01 游戏、参考版本与完整清单尚未冻结。

## 5. 当前派工与接续

| 本轮实例 / 角色 | 唯一范围与阶段 |
| --- | --- |
| `recovery_architecture` / architecture_worker | 唯一编写 [D27 System 用户目录读取补口规格](../work-items/d27-system-user-directory-read.md)。rev1 已冻结并经独立 STATIC PASS、主线程采纳，规格已单独提交推送 `c11b512d5953501c4bb1e15c92e6cc8a84564b51`，主线程核远端一致 |
| `recovery_verification` / verification_worker | 完成已提交 ledger、管理读口与公开入口的离线持久证据复核、tools 归档只读核对，以及 D27 补口 rev1 独立静审；这些静态结论不代表新业务实现通过 |
| `directory_backend` / backend_worker | 已按主线程授权正式实施 D27 补口卡内五路径；本记录冻结时尚无该实现的业务验收结论 |
| `recovery_documentation` / documentation_worker | 唯一写本续接记录及台账页首短恢复入口；保留台账原正文和所有旧文档，文档自查后停止写入交主线程审查 |

D27 补口针对已经确认的需求与恢复基线输出缺口：`ListUsers` 查询并读取 `created_at` 用于分页，却没有将注册时间返回给用户列表；[系统设置布局](../../frontend-design/layouts/system-settings.md)要求显示注册时间。rev1 静审输入 SHA-256 为 `4edb37f8c7cd1f9aa492b2a797f348d6e8bb5a92c0a52b405c365eb40537d216`；采纳后仅更新页首，技术 §1–5 未变，提交稿 SHA-256 为 `b3d7c1b5da7a6cbca3a5c8b709adbdfe8cdf7d79eb5d96549e74445ad0650ec1`。规格已单独交付，五路径实施已启动；该状态不表示 System 用户页面、返回字段或完整 D27 已交付。后续实现、自测与独立业务验收另行记录，本页不新增业务写权或测试资源运行权。

## 6. 文档范围、检查与交付

本次遵循[仓库规则](../../../AGENTS.md)、[团队流程](README.md)、[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)和[开发计划](../development-plan.md)。文档负责人实际执行 Git 状态/引用/未推送计数只读核对，使用 Python 按两个清单检查 30 个精确路径、原基线及候选文本 SHA；没有修改任何候选源码。

本次仅写 `docs/development/agent-team/recovery-2026-10-06-continuation.md` 与 `docs/development/agent-team/tasks.md` 的页首短恢复入口；后者原正文逐字保留。两文件的本地链接、Markdown 结构、UTF-8/LF、末尾换行、尾空格及限定文件 whitespace 检查通过；其余旧文档保留原内容。没有运行 Go、npm、浏览器、数据库、Docker 或产品测试，没有执行 Git 写操作。

文档范围完成自查后停止写入，无关联后台命令或自有运行资源；由主线程审查后按已有授权独立提交并推送，交付提交通过这两份文件的 Git 历史定位。本记录不预先声称已提交或推送，D27 规格及实施文件不属于本次文档交付。

## 7. D27 用户目录后端接受与 UI 前置交还

§1–6 保留本次恢复起点与当时派工。注册时间读口现按[rev2 六路径](../work-items/d27-system-user-directory-read.md)完成，由 `directory_backend` 实现、`recovery_verification` 独立 PASS，主线程采纳提交推送 `3affc0194214101cfa1e6fdc583afa5d60005db8`，实际远端一致。[正式报告与持久证据](system-user-directory-read-verification.md)固定 input02 SHA-256 `004ced3247661feca93ef7899dbc539f9f638a17daa824c30692881f26622c96`；六源、两项依赖锁与五项原边界末检匹配。

author01 首红为合法 admin HEAD 实际405、原断言要求200；rev1 规格/静审遗漏了 Account 严格 method 分派，不能用 serializer 的空 body 能力证明路由已通。原输入、driver exit1 和失败日志保留；rev2 新增 users 显式 HEAD、精确 query 分类和 OpenAPI 后，author02 原两组全 PASS、独立探针一组 PASS，两个 driver exit0。原 HEAD200 及旧权限/路由断言未放宽，no-tests/仅编译不计动态通过。

三轮均已 actual wait，各四容器/三网络 exact-ID 双次 absent，原两容器/四网络的 ID/name/labels 不变，所属进程与 runtime 为空，窗口已交还。归档只保留原报告、精确源码版本、命令/raw、静审纠正与资源终局；文档负责人只做离线原字节/Git/链接核查，没有重跑业务。

该结果只接受专用九字段目录与 GET/HEAD 读口，原共享八字段 User 保持。[系统用户目录 UI](../work-items/d27-system-user-directory-ui.md)的后端依赖现已满足，后续页面、同一 Cookie owner 组合及浏览器验收另行推进；没有前端、邀请投递补口或完整 D27 通过声明。Summary 待决、Object/tools 原任务停止、Artifact/Project 阻塞、生产未绑定、ready503、完整 D08–D28/E01 未完成与 E01 未开始保持。

## 8. 共享遮罩焦点修复接受，UI 组合继续

系统用户目录 UI 的真实 new02 在390px遮罩关闭后未保持触发按钮焦点，原 Navigation 第544行严格断言失败；同轮 Read通过、Authority另有失败。原生 Chromium 对照说明同步恢复后默认 mousedown 可再次使 trigger失焦，但对照本身不是组件验收。主线程据此独立拆出[基础修复卡](../work-items/d27-dialog-outside-focus-repair.md)，不改原 UI 焦点期望。

`directory_backend` 完成四路径，唯一生产变化是 UiDialog 对顶层且允许 outside 的真实遮罩事件先 preventDefault、再走原关闭路径；UiDrawer 自然继承，useLayer、样式和业务壳未改。旧组件同一浏览器断言实际红，新候选11例 PASS；隔离基线 web 的161测试及格式/类型/build通过，`recovery_verification` 独立一个真实组件顶层两轮 PASS。主线程采纳提交推送 `b53895f7eb1d020276e8f54a99a7c0821b286481`，并核远端一致；[正式报告与最小证据](dialog-outside-focus-repair-verification.md)记录固定四源、原始失败和实际命令。

作者 old01 首 runner 缺 subreaper，两个 PID1 所属 Z 记录已经退出，但当前父进程无法 wait；该轮不写成全清零。作者新轮的实际回收只适用于新轮；独立首轮原 clean=false 及之后实际转换缓存清理分别保留，最终轮实际 wait/双清与监听基线不变。漏复制颜色文档、standalone tsc 缺 Node typeRoot、机制首次长 TMPDIR 启动失败也保留。归档按 SHA 去重，不复制全 web、依赖、dist 或可执行文件，没有重跑浏览器或业务测试。

基础结果先独立交付，系统用户目录 UI 正按已接受 `b53895f` 重建冻结组合，仍须复验原 Navigation、权限与布局，尚无页面接受结论；本次仅在 UI 卡页首追加此基础依赖，其技术正文不变。邀请读口/00019 未进入该组合；完整 D27、D08–D28/E01 未完成，E01 未开始。既有 Summary 待决、Object/tools 原停止任务、Artifact/Project 阻塞、生产未绑定和 ready503 边界保持。

## 9. 系统设置壳与用户目录 UI 完整卡接受

§1–8保留恢复、后端补口与共享焦点分离交付时的事实。[用户目录UI完整卡](../work-items/d27-system-user-directory-ui.md)现已完成独立验收，主线程采纳并提交推送 `7ef3e30cf06b5df6d19516f24c34855a8308ef05`，远端与HEAD一致。[正式报告和最小证据](system-user-directory-ui-verification.md)固定input04 SHA-256 `c8f823330cd8ab9a226f96d5b2bdc6cbe9ec3d5d4a81080080ca8bdcc4b35966`，测试19源消费后端 `3affc01` 与共享焦点 `b53895f`；第20路径README在动态通过后另行文档检查。迁移前缀为00001–00018，邀请投递/00019没有混入此结果。

作者完整check实际244/244测试、格式/类型/build通过；最终new03三个新顶层、old01三个旧顶层、independent03一个独立顶层均PASS，driver分别exit0/71.438s、84.826s、64.675s。独立两项pure在input01使用真实client/controller核唯一Cookie owner的实际fetch/body/cancel尾部，依所涉源至input04字节不变复用。不能以原native-reader的DTO观察或额外Session身份oracle代替owner尾部证据。

new01/new02原红及原输入保留：尾空格比较错误、clone在真实401后的abort观察问题、局部表格nowrap和真实遮罩焦点缺陷分别处理；共享焦点仍沿§8独立接受后组合，原断言未放宽。独立01/02的CDP缓存缺失属于测试观察方法失败，实际登录200已确认；两轮原probe保留，最终使用稳定页面后的同浏览器原生Session GET证明身份，第三轮同一独立顶层通过。没有将测试观察失败称为产品失败。

new03的light1440/dark390已由验收者、主线程及文档负责人实际查看，八主题尺寸组合的25行五列几何及窄屏标签顺序有真实断言；独立最终两截图也经验收者实际查看。七轮各自actual wait、7资源exact-ID双次absent、原2容器/4网络不变，所属PID/runtime清零；最终独立86PID、4实际adopted wait，源/dist/验证输入不变，窗口已交还。共享焦点旧作者两个PPID1 Z已退出但未由当前父wait的限制继续沿旧档保留，不以后轮清理倒推旧轮。

本次只追加报告/证据、台账页首、本节与UI卡接受页首，技术正文和旧焦点档不变。证据按SHA去重，22dist只留哈希/输入/构建原log，不复制依赖缓存、可执行文件或完整web；离线脚本只核原字节及固定Git，未重跑浏览器、业务或资源。仅此目录UI卡接受，生产SPA、Vite真实代理、原生zoom、其他浏览器及完整D26/D27未验。Summary待决、Object/tools原停止、Artifact/Project阻塞、生产未绑定与ready503保持；完整D08–D28/E01未完成，E01未开始。

## 10. 系统邀请最近投递读口与 00019 接受

§1–9保留各次恢复和交付的原时态。[邀请最近投递读口 rev1](../work-items/d27-system-invitation-delivery-read.md)现已独立最终 PASS、主线程采纳并提交推送 `9b3201547f9b7b61fd9716a6ba6540084961496c`，主线程核实远端一致。[正式报告与最小证据](system-invitation-delivery-read-verification.md)绑定 input04 SHA-256 `41382f9f482ddad686d76ae1492361031fe6a2485fa012c2a10708ee09b9fe12`；八源与Git一致，产品基线为 `7ef3e30`，两Go锁和迁移00001–00018不变。00019只增加已规定的 invitation link 部分索引，跨root最近接受排序、同Tx当前授权、单statement分页/投递投影和失败零候选均已验。

作者原三红与真实输入均保留：planned gate装配错误、误期待原DB锁等待至少3s、误要求所有PG取消都进入DeadlineExceeded错误链；生产未为测试假设变化。最终为author01六组PASS加author04完整Selection一组PASS，不称一次七组全绿。pure实际3s/父约75ms与query尾部join，和实库约1s锁超时/父约68ms分别记录。plan01两份旧测试原件缺失、pure03缺env不补造；最终规模/迁移结论由author01冻结源码与11份原计划支持，100000无关intent/2049偏斜历史只是SQL成本fixture，不是业务发送或无限规模SLA。

独立两项原子HTTP投影与跨root真实邮件/生命周期首次均PASS，分别6.74s/10.34s，whole actualexit0/145.337s。9个资源exact ID双次absent、212个所属PID/starttime消失、原两容器/四网络baseline不变、runtime空、monitor0；主命令实际wait，adopted-waits为空，窗口已释放。真实日志→SMTP接受后丢回复保留unknown，与旧root人工重试的新接受时间、到期只读/同邮箱新ID隔离均有独立证据。

本次新增报告与去重原件，更新台账页首、本节和read卡接受页首；read技术正文及旧档不改，邀请UI卡由其负责人独占。离线核168逻辑/101物理原件、四版源、八交付源、两锁、迁移、六轮原结果/清理及固定Git通过，没有重跑产品或资源。邀请UI与完整D27仍未接受，旧目录UI的1–18证据不外推00019组合；Summary待决、Object/tools原停止、Artifact/Project阻塞、生产未绑定/ready503、完整D08–D28/E01未完成与E01未开始继续保持。

## 11. 共享模态关闭焦点恢复接受

§1–10保留原字节与当时状态。[共享修复 rev2](../work-items/d27-modal-focus-restoration.md)现已独立最终PASS、主线程采纳并提交推送 `79f922ec259d2838052a903612e2a27005618c11`，主线程核实远端一致。[正式报告与最小证据](modal-focus-restoration-verification.md)固定 input02 `584a9de02239ca17c6bea8834c2cf262aec255cff690780bd0a9c97ffe78c4a6`；四源对齐Git，基线 `6be5321` 加授权四源，不消费活动邀请树。恢复保持同步、最高剩余modal允许范围和非顶层门禁；真正focus成功才结束，失败继续合法目标，无modal原规则不变。

input01作者287项完整纯测及30项真实矩阵通过后，独立仍发现继承editable后代focus无效、BODY逃出模态的产品红。原失败保留，input02以相同独立完整probe3项及编辑宿主正例1项、纯测3项复验通过；作者返修52项恢复纯测、原nested-menu1项、10项浏览器及格式/type/build/tsc通过。旧矩阵只按未变部分复用，不把各版结果合成一次全量执行。旧new01缺运行时spec指纹，保存副本同字节不能扩为当轮独立绑定；repair-old01/new01实际前后指纹齐全，不倒填旧档。

作者六轮浏览器170个所属PID/starttime和独立六轮动态68个所属PID/starttime双扫空；独立18次adopted wait实际完成，server.close、私有TMP/壳及监听基线终局通过。历史两个PPID1 Z Chromium及原记录的既有crashpad均不属本次清理，不声称未来成功回收了旧孤儿。

本次只新增报告/去重原件、台账页首、本节与共享卡接受页首，技术§1–4原字节保持。离线脚本只核保存字节、固定Git、原命令结果/清理，不重跑产品或资源。主线程已恢复邀请rev4的22路径作者与业务窗口，仍需完整组合独立接受；受控组件共同重挂不代表真实App/Session/pageshow。邀请卡由原负责人维护，本次不改AGENTS/指南/产品。Summary待决、Object/tools原停止、Artifact/Project阻塞、生产未绑定/ready503、完整D08–D28/E01未完成与E01未开始继续保持。

## 12. 系统待注册邀请 UI 完整卡接受

§1–11保留各阶段原时态。[邀请 UI rev4](../work-items/d27-system-invitation-ui.md)现已独立最终PASS、主线程采纳，22路径提交推送 `1c82d888adfef0d8b58ab51c920557ca4e2f084f`，主线程核远端一致。[正式报告与最小证据](system-invitations-ui-verification.md)固定input07 `5a0de8fd67b9fd46f9249686d469258c06a8ecb7674e8ae8b02338b9c922dfa6`的21源码/测试路径，消费邀请读口9b32015/迁移1–19及共享焦点79f922；第22路径README在独立通过后单独检查并绑定同次提交，旧报告deferred字样按原时态保留。

作者453测试/type/format/build通过，17Web源和24dist从input03至input07字节未变。六新组按Lifecycle=new04、Delivery/Read/Authority=new02、Outcome=new05、Navigation=new06组合通过，old01五旧组通过；不是同一最终版本轮重跑全部。真实产品红包括陈旧成功反馈、初次确认宿主次序与checking重挂确认非top/inert、焦点恢复；业务同页宿主与共享焦点分别修复后组合，不放宽断言。CDP原body、checking观察时序、隐藏字符标题、错误Resend409前提另列观察/fixture归因。new05 light390离场图不计页面证据，input07等待物理overlay0后new06补图；主线程实际查看原light1440/dark390及替换light390。

独立未变owner子组及input03 pure07三组/pure08四组复用，保留pure01–06原红。最终真实 `TestAccountInvitationsIndependentComposite` driver exit0/56.812s、Go9.48s、Chromium一例4.5s：原201截断保留未确认，待决确认经真实Session503和同身份恢复，真实焦点/Tab留在modal，Session/列表不作receipt，精确原key/body/CSRF重放后201与随后GET503分类分明，最后Go SQL实际核commands1/intents1/invitation1。independent01因私有testDir误收归档副本而Running2/第二例429，首例安全事实不能代替未执行的Go末尾DB；原失败保留，仅私有config排除runs、list1后整组通过。

作者七轮与独立两轮真实命令实际wait、各自exact资源双absent、原2容器/4网络基线不变、所属PID/runtime清零。独立最终7资源/86PID-starttime/4 adopted wait/monitor0，窗口释放；pure实际wait范围和保留编译缓存分别记录。旧共享任务两个PPID1 Z限制不计本轮清理。按SHA去重保留七输入、全部原红/分版复用与原命令/退出/清理；dist仅哈希与构建记录，不复制全树、依赖或二进制，文档负责人仅离线核原件/Git。

本次只接受邀请管理完整卡；Provider后继独立推进，生产SPA、真实Vite代理、其他浏览器、native zoom及真实BFCache未验。卡技术正文保持，旧档、AGENTS与指南不作大块同步。完整D26/D27及D08–D28/E01未完成、E01未开始；Summary待决、Object/tools原任务停止、Artifact/Project与生产未绑定、ready503等边界保持。

## 13. System Provider 管理 UI 完整卡接受

§1–12保留各阶段原事实。[Provider rev4](../work-items/d27-system-provider-management-ui.md)已由作者完成、独立最终PASS并获主线程采纳，22路径提交推送 `f465f45b899e21c225e7d4107c099b256538239c`，主线程核远端一致。[正式报告与最小证据](system-providers-ui-verification.md)绑定input07 `e758cfd704f8677fe23cc57c8a41a23adff769fd85461a07092bedf1c6b26428`的21源/26dist指纹与后补README第22路径；固定1c82d88、后端9b32015/迁移1–19及1016只读依赖，卡技术§1–7保持原字节。

接受Provider管理、Credential创建后独立绑定与安全恢复、Models只读子列表。API原WHATWG拒绝Go合法URL两项、owner清材料通知同步重入一项产品红保留，原probe返修后分别19/19、5/5通过，另有补充probe和App纯组合；实际read/cancel尾部沿未变生产字节的纯屏障。完整603测试/format/type/build通过，caption局部CSS后做受影响检查；新6按new02/new03/new05精确差量组合，旧7在input07通过，不称最终一次全量重跑。

作者new01 label前提原红、有界中断和原cleanup=false不改，精确私有目录另次清理后双核true；new02登录完成/可访问与物理层数前提红保留，不补造未记录DOM。new03缺sr-only、new04caption定位与取景问题使原图被拒，input07局部定位和取景门禁后new05八图接受，作者逐张、主线程实际看light1440/dark390。固定CSS/简化DOM诊断与正式业务分列；pure03/04缺完整旧源只保留原指纹/失败，不补造。

独立真实01因两物理Playwright副本注册失败，未进入浏览器业务；02 Replay PASS而Partial错要求text/plain503 reader EOF，原失败不记产品红。私有probe只删不可能的503EOF前提，成功恢复200仍需新seq EOF与精确身份；03仅Partial实际exit0/56.399s、top9.93/browser5.6，复用未变02 Replay11.35/browser7.5组成最终PASS。覆盖两阶段接受回执截断与原key/body/CSRF重放、唯一DB事实、确认后GET503、部分成功材料销毁、same Session503/200共同宿主恢复、真实焦点/Tab及只重基Provider，未以lookup或列表代receipt。

九真实轮各自actual wait/输入不变/原2容器4网络基线不变、exact资源和PID-starttime终局保留；最终独立7IDs双absent、82所属PIDgone、4adopted wait、monitor0/runtime空，窗口释放。历史非自有PPID1 Z未触碰且不称已wait。归档676逻辑/409物理原件与16Git引用，离线verify核固定f465f45的22路径和原字节，不读活动Model树、不复制dist/依赖/二进制、不重跑产品。Model后继按其独立卡推进；完整D09/D26/D27及D08–D28/E01未完成、E01未开始，Summary待决、Object/tools原停止、Artifact/Project与生产未绑定、ready503及生产SPA/Vite/Runtime/其他浏览器等边界保持。

## 14. System Model 管理 UI 完整卡接受

§1–13保留各阶段原事实。[Model rev2](../work-items/d27-system-model-management-ui.md)已完成并获独立最终PASS、主线程采纳，26路径提交推送 `bc17167c42ee5d5fc1427adaac099ff888ca959b`，主线程核远端一致。[正式报告与最小证据](system-models-ui-verification.md)绑定input03 `d12df3fe63fdb9a405db2ef72864acb48a6de3280d7938559f78272128754ff6`的25源/29dist指纹和末件README，固定Provider f465f45、后端9b32015/迁移1–19及1030基线；卡技术§1–7原字节保持。

作者829测试/format/type/build通过，19个Web源与29个dist从input01至03保持相同。新6由input02/new01的Lifecycle/Outcome与input03/new02其余四组组合通过，旧core7和Provider3在input03通过，不称同一最终整轮全绿。三处页面产品红为陈旧替代候选、离场编辑器晚到复核、首次删除误作版本复核，原source/raw/result与返修绿保留；409错误预期和36路径运行期绑定不足是先于真实运行的静态阻断。new01四个snapshot比较失败没有现场字段差异原件，后续真实httpModelDTO五形状serializer与固定源码支持input03仅DB→wire形状投影，全DTO比较不削弱。

独立API16、owner8、page/App5按未变输入复用；真实首轮Replay9.58s/browser5.0s与Replacement10.44s/browser6.0s均PASS，driver实际exit0/155.994s。覆盖Model版本不变时正式新增引用、截断接受响应后原key/body/CSRF重放与affected_refs1、当前跨Provider候选400/not_started零部分副作用、显式复核、同身份503/200恢复待决确认和原生焦点/Tab，再以新命令原子替代required memory；最终Go核唯一command/Audit/event与selector/索引。私有enum类型准备原红保持，独立真实无首红；native观察器不替代纯API/owner实际尾部证明。

作者四轮及独立一轮原command实际wait、7exact资源各自双absent、原2容器/4网络基线不变、源码/dist/私有输入不变、所属PID/runtime清零。独立最终212所属PID/starttime、8adopted wait、monitor0，窗口释放；历史PPID1 Z未触碰、不称已wait。早期pure没有完整env照实保留。作者八图全部实际查看，主线程接受light1440/dark390；归档不重跑资源。README首稿及最终单处说明差量都保留：引用变化本身不必拒绝，正式DELETE事务重新裁决；无业务改动。

本次档案按SHA去重为794逻辑/302物理原件，离线核固定bc17167的26路径与1030基线；不复制依赖/dist实体/缓存/二进制，不读取后继Selection活动产品作结论。唯一agent负例只用自有Model反向索引模拟未绑定引用，UI零DELETE与另次正式HTTP503/零副作用分列，不声称Agent正式存在；selector引用全部正式GET/PUT。Selection UI、外部调用、Runtime、生产SPA/Vite/其他引擎/native zoom不在本卡接受内。完整D09/D26/D27及D08–D28/E01未完成、E01未开始，Summary待决、Object/tools原停止、Artifact/Project与生产未绑定、ready503等边界保持。

## 15. System 平台模型用途 UI 完整卡接受

§1–14保留各阶段原事实。[Selection rev2](../work-items/d27-system-model-selection-ui.md)实际26路径已完成并获独立最终PASS、主线程采纳，提交推送 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`，主线程核远端一致。[正式报告与最小证据](system-model-selection-ui-verification.md)绑定input02 `af2f9f4a50f4c435e849bf8faf9d8676a11ccd7c3c6d61416dcbfda57f8a72c5`的25源/31dist指纹及最后授权README；固定Model基线bc17167、后端9b32015/迁移1–19及1041依赖。原27候选依卡页首剔除无需改动的system-models.ts，技术§1–7保持被审原字节。

作者993测试/29文件与format/type/build通过。新6按new01五组+new02 Navigation一组组合，旧core7/Provider3/Model3通过，不称一轮新6全绿。原Navigation在浏览器前因Provider seed130/129 rune超128收到400，input02只缩短seed35→34，正式验证边界与全部原失败保留。P-SELECTION-PAGE-01为已观察disabled后canSave仍真，writes=0未发PUT；生产仅controller返修、原probe同字节复验，另有state回归测试。作者成功按钮/协议测试前提和H-SELECTION-01错误503EOF静态阻断分列，不冒充产品红。

独立API15、owner8、页面原probe7+补充5沿未变输入复用。真实01的A审计摘要遗漏canonicalization、B条件按钮名错误均保留，未到的后续场景不能记通过。02的B完整PASS11.95s，A已到当前E+2/旧目标404和lookup历史回执后因状态文案前提停止，尚未原重放或最终持久化核验；03只复验A最终完整PASS9.38s，实际exit0/61.415s，与未变B02组成接受。最终精确原key/body/同Session合法CSRF重放得到历史E+1、影响引用数0，唯一command/Audit/计划事件各1且无delivery，当前配置与引用不回退；B已验候选400零部分写、冲突409、显式核对、Session503/同身份恢复与真实modal焦点/Tab。原01/02仍exit1，私有前提修正不改产品。

作者五轮和独立三轮实际wait、7exact资源各自双absent、原2容器/4网络基线与固定输入保持、所属PID/runtime清零。最终A03为84PID/starttime和4实际adopted wait，monitor0，窗口释放；历史PPID1 Z不计回收。作者八图逐张查看，主线程实际看light1440/dark390；900px视口不证明全部下方字段。四份确认缺失的历史通过源码及十二份formatter输入暂态未定位分别保留原hash/command/raw/exit，不补造、重跑或声称每版可重建；失败测试源和最终受测源完整。

本次按SHA归档1235逻辑/445物理原件，离线核固定870ebbb的26路径、31dist指纹、1041基线和八轮终局，不读取后继账号安全活动源码、不复制依赖/dist实体/缓存/二进制或运行产品。账号安全UI另卡推进；完整D09/D26/D27及D08–D28/E01未完成、E01未开始，Summary待决、Object/tools原停止、Artifact/Project与生产未绑定、ready503、Runtime/生产SPA/Vite/其他引擎/native zoom等边界保持。

## 16. 共享 Dialog 显式页面焦点后备接受

§1–15保留各阶段原事实。[共享 fallback rev1](../work-items/d27-dialog-fallback-focus.md)已获作者与独立最终PASS、主线程采纳，五路径提交推送 `fd32120eba4c76f67b649248377f4825a78d5d79`，主线程核远端一致。[正式报告与最小证据](dialog-fallback-focus-verification.md)固定组件基线870ebbb、规格3c79fd4、作者input01 manifest `604a4738761e428b0bff3f8ea64319ae07f6e8e072b97dead925c309c09f6860`，共享卡技术§1–4保持原字节。UiDialog可提供本地显式fallbackFocus，正常顶层关闭且无剩余modal时读取最新ref；合法原trigger实际聚焦优先，目标有同document／非BODY-HTML／可见／禁用及实际焦点门禁。无prop、卸载、非顶层和剩余modal保留原规则，不增加延迟抢焦点或全局搜索。

作者old01因grep discovery无测试而actual1/2.008s，没有browser/server；原clean=false与实际所属两扫空、端口不变、TMP删除分列。old02原组件真实焦点RED为actual1/7.608s；原probe后来只按项目Prettier规范化，保留两版和等价证明，不称byte-identical，也不追填后来list先于旧红。pure01两处布局spy前提原红后，仅测试装配调整，pure02为Dialog102＋旧components12共114通过。作者new01原34＋新11共45项Chromium全PASS，actual0/123.627s、145所属PID／4adopted实际wait、输入不变、server关闭、两扫空、端口不变与短TMP删除；type/build/format和发现原件齐。

独立pure01七项通过、一项错误预期下层内容input而非内建关闭按钮；pure02只复验正确精确目标一项，组成8项通过，旧轮仍exit1。type01未定位已锁Node声明而exit2，私有配置接入原web/@types后type02通过，未改浏览器断言。browser01三项真实PASS、actual0/8.292s，验证initial-null重挂后最新目标、原生inherited editable focus no-op后备、合法trigger优先、卸载／null／剩余modal门禁及原生Tab／用户后续焦点。19所属PID双扫空、4adopted实际wait、server.close=true、TMP消失、端口不变，无清理信号，窗口释放；历史PPID1 Z未触碰且不称已wait。

本次219逻辑／139物理原件与140固定Git引用只做原字节、五交付源及16轮原退出／清理离线核对，不复制完整web、依赖、dist或二进制，不重跑产品。Account new01组合断言false与new02实际BODY、未直接采样captured trigger的边界只作固定交叉引用；账号安全正按rev2继续实际Session／Navigation组合，尚未接受，受控共享组件结果不能替代业务卡。AGENTS只新增此适用能力说明；完整D26/D27及D08–D28/E01未完成，E01未开始，Summary待决、Object/tools原停止、Artifact/Project与生产未绑定、ready503及Runtime／生产SPA／Vite等边界保持。

## 17. Selection 取消读取后的状态恢复接受

§1–16保留各阶段原事实。[修复卡 rev1.1](../work-items/d27-model-selection-cancelled-read-recovery.md)四路径已获作者检查、独立最终PASS和主线程采纳，提交推送 `debbb28deb7c883fd0b6b77a75354b9b5d7ece0b`，主线程核远端一致。[正式报告与不可变证据](model-selection-cancelled-read-recovery-verification.md)固定 fd32120、规格e484815和作者input01 `e05427019f96dd4e33dc3f51d4c4925e1cff8ce09a8ba28c8494cf5f5d0fe8e6`；四源、31dist指纹、1033接受基线与卡技术§1–4均明确绑定，不消费未验Account候选。

controller仅在公开主动放弃的同步退役边界将本批未完current/reference的loading转为明确可重读error，保留saved ID、完整ready pair、既有error/optional empty。旧代次/身份隔离、严格canSave、原命令恢复与actual owner finally释放保持；不自动补读或新增写。固定接受基线pure-red01实际整19例中两项正确终态产品红、原17通过；修后两例及targeted105通过。check01因私有导出缺已接受主题文档而1043/1044、build未执行，仅补固定只读文档后check02 1044/29文件与format/type/build通过；独立type01缺RouteMeta声明的私有前提红也保留。

作者Navigation actual0/119.897s，Read/Outcome另一轮actual0/84.293s通过；独立纯reference两例/current一例以真实App/Session/native stream证明cancel实际尾部及显式恢复，独立真实首轮代表actual0/60.523s、顶层10.10s通过。真实确认重挂→同Session新GET200 EOF→精确Provider hold尚未结束时放弃，验证ready/ID保留、三错误、零自动GET/PUT、显式恢复后一次正式PUT及严格receipt，Go随后核version+1/三引用及command/audit/event/key各+1。服务器hold结束不充作native owner tail证明。三轮均按45s/2m/6m、race/count1、worker1/retry0；各7exact IDs双absent，作者130/100与独立85所属PID两扫空，实际adopted wait为4/8/4，原基线与输入不变、monitor0/runtime空。历史PPID1 Z未触碰、不计回收。

八图只有作者逐图审阅及视觉报告；主线程仅读报告，没有view本轮截图，独立验证者也未图审。独立v1误称主线程代表图审的原文保持，v2只纠正审阅主体，不改功能、清理或源SHA。图片为可滚动内容顶部，不声称全部下方字段同时可见。原混合diagnostic02/04绿表示复现缺陷；01/03前提红、Account oldselection01 timeout及oldselection02完整引用场景诊断PASS分列，原首轮缺少现场分项，不能倒填唯一原因。

归档271逻辑/176去重原件，保留原raw/diff、26项本修复检查和旧诊断交叉原件，仅离线核保存字节与固定Git；不复制全树、依赖、二进制或dist实体，不重跑产品。Account后继必须在已接受两测试文件上精确重放原六叶/三组增量，保留本修复全部强断言并另验组合，尚非整卡接受。本次不写Account卡或README；完整D09/D26/D27及D08–D28/E01未完成、E01未开始，Summary待决、Object/tools原停止、Artifact/Project与生产未绑定、ready503及Runtime/生产SPA/Vite等边界保持。
## 18. System 账号安全 UI 完整卡接受

§1–17保留各阶段原事实。[账号安全 rev2](../work-items/d27-system-account-security-ui.md)已获作者检查、独立本域及最终组合PASS，由主线程采纳提交推送 `40c904c0dd88420fc621f40c0a737d243d514ec1`，主线程核远端一致。[正式报告与不可变证据](system-account-security-ui-verification.md)固定input06 `baeaeb37b348167d8407831314e83adb60615dfb7882f248506c7a314c0e4954`的26源／33dist指纹、debbb28的1051接受基线及最后README；27交付路径逐SHA核固定Git，卡技术§1–7保持原字节。

管理员账号安全内联表单区分当前GET、四字段草稿和历史PUT Settings；无lookup，未确认命令仅明确原key/body/expected version与合法CSRF重放，读取当前不充作确认。409保留草稿、明确采用才换基线，原Session期限不被追改。七域实际I/O尾部与当前权限、App期状态和共宿主确认保持；共享fallback fd32120及Selection取消读取debbb28分别已接受，本卡两重叠测试只重放原五处六叶三组适配并保留全部新强断言。

完整组合检查1115测试／32文件及format/type/build通过；Go发现首轮私有TMPDIR不存在在编译前exit1、同命令准备目录后通过，原失败保留。新五组由new01四组＋最终Account Navigation组成，旧十六组由core7／Provider3／Model3／oldselection01两组＋最终Selection Navigation组成。原new01未采样BODY或captured trigger；new02实际BODY亦未直接采样原trigger。页面fallback两产品红后返修通过、独立VTU代理身份前提红改uid后两例通过，均保留原件。oldselection01禁用超时缺现场分项，诊断绿和后续独立修复不能反填唯一原因。

独立input04 Replay／Conflict首轮actual0/160.685s：历史写响应截断后另一Session推进当前，明确原材料重放得旧Settings而当前不回退；正式409、同身份Session503→200EOF、待决确认／标题焦点／原生Tab和新旧Session期限均通过。最终input06仅24源不变＋两测试差量及既有Selection controller/state接受差量，独立增补核该复用与新组合，不称A/B在新dist重跑。两页Navigation actual0/81.887s、顶层17.12/16.49s全PASS；7exact IDs双absent、95PID/starttime双空、8实际adopted wait、monitor0、自有runtime空，基线／输入不变，窗口释放。全部十轮实际wait和各自双清保留；历史PPID1 Z不计已回收。

最终十六图仅作者逐张检查，主线程仅读报告，独立及归档不新增图审声明；窄屏下方字段需主区域滚动。早期Go编译未消费browser前态72e3a507…40d37无精确副本，仅保留原fingerprint/command/raw/exit，不重复搜索或补造，最终失败源与受测源齐全。早期env只据原记录，不扩为完整环境。本次559逻辑／388唯一SHA对象中353新增、35复用既有永久档，离线核27Git路径／1051基线／64原检查含十轮终局，不复制依赖/dist实体／二进制或重跑产品。

§16–17所述Account待组合现由本节独立接受闭合，旧正文不改。本次不写SMTP卡、前端README或产品；不接受SMTP发送／新后端／迁移、生产SPA／Vite代理浏览器、BFCache、原生缩放或Runtime。完整D09/D26/D27及D08–D28/E01未完成，E01未开始；Summary待决、Object/tools原停止、Artifact/Project与生产未绑定及ready503等边界保持。

## 19. System 邮件任务管理事实读口接受

§1–18保持原阶段事实。[管理读口 rev1.2](../work-items/d07-system-mail-job-management-reads.md)八路径已获作者新三组／旧两组、独立风险验收及主线程采纳，提交推送 `819aba1b8f764328f1e2e67b53c274fad0db877d`，主线程核远端一致。[正式报告与不可变证据](system-mail-job-management-reads-verification.md)固定input04 `4e3bee7144106e9b183977a8306314f08732a18ef54111997fbfb9a3a7ada5c4`、八交付源与私有3c79fd4基线935文件；Go1.27.1／3501运行时文件指纹、锁和固定MinIO均有原门禁。卡技术§1–6保持137e6a30…176bc原字节。

新增列表／详情两个GET-only管理投影，九required字段和可省reason、真实attempt渠道／结果nullable，旧channel仍是兼容摘要。单statement先materialize intent页、核双向job及exact current attempt；同Tx当前管理员授权，哨兵／Close／取消／Unknown等失败零候选，预认证前建立最长3s并继承更早期限，实际尾部join后返回。旧MailJob／InvitationDelivery／写receipt／cursor与正式retry资格不变；GET不是命令receipt，不新增lookup、自动重试／轮询、索引或迁移。

作者new-read01的HTTP原CURSOR_INVALID期望错误保留，同轮ReadBudget PASS；input03仅修该8193B期望，完整HTTP复验通过。new-attempt01撤销后期待ResourceDeleted而正式workerBusy的原卡／测试前提错误保留；rev1.2/input04先读cancelled/null再硬验Busy及DTO／五DB计数／SMTP零新增，完整AttemptFacts随后通过。backoff静态修正分清completed_at与schedule两时钟，保留10s下界及原12s期限，不伪造状态。api-pure01编译首红保留；全pure/race/vet/build、受影响integration编译／发现均已完成，编译不计行为。

新三组和旧两组按未变输入组合。独立pure01候选Close退役两例通过，预算组三事务计数前提错误由pure02两例复验闭合，原exit1不改；原Database.Connect准备编译错误也保留。真实A在independent01完整PASS3.05s：150ms父期限typed57014零候选，正式Logout发生在两读真实advisory等待期间，释放后同Tx授权拒绝、候选SQL0和实际join。A直调facade/PG，HTTP预认证由纯handler＋作者正式HTTP补足。

真实B首轮已到新log子周期DTO，却以公开backend_log误断DB原log，综合原raw无逐字段诊断，归因据固定writer/scanner/SQL，不能追填旧现场。只修该私有条件，independent02只B完整PASS4.58s、actual0/56.339s；旧unknown/fence/actual join、retry源版本增加1、新周期null→日志sent、same-key当前版本及重复读零新DB／SMTP全部通过。A原字节复用，未称首轮全绿。作者五轮＋独立两轮原command实际wait、7或9exact IDs双absent、各所属PID/starttime双空、monitor0/runtime空及原2容器4网络不变；最终B72PID，所有adopted wait为0，历史PPID1 Z不触碰、不计已回收，窗口释放。

受控handler真实context本地3s／更早150ms与真实PG75ms父期限／原1s锁超时分别记录，不能把DB1s说成3s自然到期。三个正式intent的normal/next/empty EXPLAIN返回3/2/0行，只证明小表计划和分页后关联限制，不证明无索引历史排序规模SLA。B仅自有受控SMTP和受限日志，没有外部邮箱／SMTP UI／浏览器／生产SPA或Runtime验收。

归档529逻辑／237去重原件，仅离线核八Git路径／935固定依赖／3501原运行时指纹与38检查含七轮终局，不复制完整树／依赖／dist／二进制或重新执行产品。Account§18接受不被改写，SMTP配置首卡及后继页面仍各自另验。本次只更新授权入口；完整D07/D27及D08–D28/E01未完成、E01未开始，Summary待决、Object/tools原停止、Artifact/Project及生产未绑定／ready503等边界保持。

## 20. 邮箱 canonical 再输入与 SMTP wire 闭包修复接受

§1–19保持原阶段事实。[D07修复卡rev1](../work-items/d07-email-canonical-roundtrip.md)十路径已获作者检查、独立风险验收及主线程采纳，提交推送 `f670cb1fe1f07ebd21bdb90a2b96cd565c33f05a`，主线程核远端一致。[正式报告与不可变证据](email-canonical-roundtrip-verification.md)固定819aba1基线、九源input01 `e2882f0a9c6458127d09034309540ab04b7c7257f6d92d931c1d832a5ae1c26a`与第十Account说明；卡技术§1–7 f62bf7…af15原字节保持。

只补原合法输入全小写canonical的再输入资格，旧NormalizeEmail成功分支和输出、持久身份/索引、MAC及历史回执保持。SMTP要求canonical材料，私有wire副本恢复IPv6标记并供MAIL/RCPT/From/To同用；普通地址、prefixed IPv4/mapped文本及原预算/TLS/join保持。无写服务/API/schema/迁移/锁变化，不消费活动SMTP前端。

原old01两个真正行为RED与new-minimal01同函数原字节绿保留。pure01缺七固定资产的前提红由恢复资产后的pure02闭合，完整受影响两包pure/race、vet/build及最终九源编译/发现通过；早期integration-compile旧测试输入不冒充最终版。独立core两纯代表覆盖原S oracle/边界和完整wire golden，harness静审与真实验收分列。

作者new-account01 service/HTTP两顶层通过，new-wire01四子例通过，old-smtp01原none/STARTTLS/TLS通过。独立independent01 actual1/123.034s两顶层FAIL：formal-test-intent及prefixed-ipv4-and-mapped两个子例完整PASS；http子例末guest.bootstrap强求backend_log、expanded子例TempDir ENOSPC分别阻断。原http raw无bootstrap DTO，且401/403/尾部尚未到达，不能追填。只修私有bootstrap调用与两个子例选择，independent02 actual0/72.577s，http-original-history2.27s、expanded-ipv6 2.23s完整PASS；原两个通过子例字节未变复用，未称最终四例全重跑或原轮全绿。

作者三轮与独立两轮均actual wait、exact资源两扫absent、所属PID/starttime空、runtime空/monitor0及原2容器4网络不变；末轮9IDs74PID，五轮adopted wait均0，历史PPID1 Z单列未触碰、不计已回收。作者old01命名碰撞exit1、独立错误--run参数exit2均在资源前停止；清空间只处理六个非活动可重建GOCACHE，未删源或证据。第十文档检查器误解no-index exit1的准备记录保留，文档候选未为此改变。

被审候选规格全文c48e3a…71c63缺精确副本，只保留原技术和接受页首全文73c7d5…81f4；不得重建推测旧页首。394逻辑原件以194个SHA对象与9Git引用去重，离线核十Git路径、621/886阶段闭包、881实际依赖及3501/3512原运行指纹，不复制完整树/缓存/依赖/二进制，不重跑产品。历史回执由候选创建，不证明跨二进制升级；受控SMTP不证明外部邮箱送达。

SMTP UI、出站HTTP及完整D07/D27仍各自待验，D08–D28/E01未完成、E01未开始；Summary待决、Object/tools原停止、Artifact/Project及生产未绑定／ready503等边界保持。本次仅归位授权入口，不改旧报告、SMTPUI/出站卡或后端说明。

## 21. System 出站规则管理 HTTP 完整结果接受

§1–20保留原阶段事实。[D04/D27卡rev1](../work-items/d04-system-outbound-policy-http.md)十二路径已获作者检查、独立风险验收与主线程采纳，提交推送 `a94277982620f01dc15488b09ae6ea9064977b5a`，主线程核远端一致。[正式报告与不可变证据](system-outbound-policy-http-verification.md)绑定819aba1、harness-stage02十一源及第十二后端说明；技术§1–7 44515e…f8dc原字节保持。

正式管理员 GET/PUT 复用原 Account HTTPBoundary、同Tx当前授权与同实例PolicyService/client，当前GET与原body/key历史回执分离。3s／30s预认证期限、实际body/服务/取消尾部及原规则字节/分类器/Unknown保持；不新增lookup、Reload、探测或代理HTTP。

原API api03 native keepalive unexpected EOF产品RED、作者real01事务fixture缺Challenges/DeliveryRequested的DEPENDENCY_UNBOUND、独立compile-account01 typed key编译前提全部保留。作者real01 actual1/32.295s四项PASS，事务初始化未到子例；仅换固定B02 fixture一行，transaction-real02 actual0/23.728s、事务top5.37s七子例通过。不是最终五组同轮全绿。独立independent01 actual0/27.539s，A历史回执／精确原command Audit2.58s、B同root进行中body跨规则删除2.49s首轮通过。

三轮均原actual wait、7exact IDs双absent、各39/30/36所属PID/starttime清零、adopted0、runtime空、monitor0及原2容器4网络基线不变；历史非自有PPID1 Z不触碰、不计已wait。真实parent约256ms／PG1s锁等待与API自然3s/原生期限分别证明，HTTP截断不冒充DB CommitUnknown，新旧Unknown证据按既定未变边界复用。

581逻辑原件用213对象和286个Git引用去重，离线核12交付、431→432源码／420→421依赖、3503／3506运行时指纹和35原检查；只保留必要原件，不复制整个树/cache/依赖/二进制。早期harness-compile01引用的旧附加闭包SHA73ca6172…95884全文未定位，原11候选源/命令/raw/退出及最终完整依赖齐，不补造历史清单。

本次只归位授权报告与入口，不改旧§1–20、SMTP卡或后端说明。SMTP UI及未来出站页面各自待验；完整D08–D28/E01未完成、E01未开始，Summary待决、Object/tools停止、Artifact/Project及生产未绑定／ready503边界保持。

## 22. System SMTP 配置与私有凭据 UI 接受

§1–21保留原阶段事实。[配置首卡rev2](../work-items/d27-system-smtp-settings-ui.md)29路径已获作者检查、独立有界PASS与主线程采纳，提交推送 `628612cdfc730cc1d89cad4e24a5d20f36cc6812`，主线程核远端一致。[正式报告与不可变证据](system-smtp-settings-ui-verification.md)绑定原input01–03及最终input04、独立A/B和第29README；卡技术§1–7 910b51…e269d86原字节保持。

管理员GET／PUT／unconfigure、私有密码保持／替换／移除及重试策略闭合；current GET不是历史写回执，applied与current分离，后读失败不丢未确认password/key/body/version，只能明确用当前合法CSRF重放原请求。第八域actual owner尾部、当前身份／权限清理、双确认同View宿主与最新fallback保持。没有lookup、自动重放、测试发送或连接检查入口。

作者完整1214测试／35文件、type／build／适用Go检查通过。新六组按input04四次受影响PASS＋new02未变Concurrency8.95s／Authority18.66s组合，原new02整轮exit1／180.320s与四FAIL不改；旧19分五批通过。独立input02修正准备标题后首真轮A/B全PASS，actual0／158.636s、7IDs181PID8adopted实际wait双清，原command／Audit各1、历史结果与较新当前配置、真实403、pageshow／Session503／原生Tab／双层门禁到达。身份准备已结束，配置阶段投递任务／intent无新增，不说全部准备从未产生投递事实。

原API旧契约RED与已接受邮箱闭包/API02新契约分列；API02实际1PASS＋旧11显式skip复用。owner原七例exit1仅六契约PASS，额外flush:sync栈内重入按主线程适用性排除，未删例／放宽／返修，不称7PASS。原页面stale反馈、非响应式dirty／maxVersion与disabled trigger产品红，new01错误SQL fixture、new02观察超时、navigation03焦点失败，独立typeRoots／标题静态前提和原静审勘误全部保存。旧现场缺观察不从后诊断倒填。

作者十二轮＋独立一轮均记录原actual退出、7exact资源双absent、所属PID/starttime空、实际adopted wait、runtime空、monitor0及原2容器4网络不变；历史PPID1 Z不触碰、不计已回收。导航八图仅作者逐张查看，主线程未图审；旧回归48图只有原索引／指纹，独立敏感轮关闭图／trace／video。

真实input04及最终独立仍绑定f670cb1／1050依赖，旧new01/new02/navigation03绑定40c904c／1040。后继63de0ac包含a942779的主线＋SMTP29只做604本地／3504外部输入绑定、race编译0／34.369s、两个纯路由0／3.548s、Central Runner构建0／17.675s，没有主线浏览器重跑。README v2只收紧当前合法CSRF句，28源／36dist未变。

834逻辑原件以499对象＋100逻辑Git引用去重，离线核29固定提交路径、2722 Git blobs、76条原运行记录含十三真实轮；不复制全树／dist／依赖／缓存／二进制，不重跑产品。harness-format00缺原完整argv/env／前后绑定，harness-type00缺独立child wait／精确当轮源指纹，照实限界；独立type01三旧原源实际已找到，不列缺件。

本次仅配置首卡接受。发送／投递页面、SMTP连接／认证／外邮箱、生产SPA／Runtime与完整D07/D27另验，D08–D28/E01未完成、E01未开始；Summary待决、Object/tools原停止、Artifact/Project及生产未绑定／ready503边界保持。旧§1–21、其它卡、frontend README及产品均不由本次文档归位改写。

## 23. SMTP 测试与投递任务 UI 规格采纳及私有实施启动

§1–22保留原阶段事实。[正式卡rev1](../work-items/d27-system-smtp-delivery-ui.md)已获独立STATIC PASS、主线程采纳并提交推送 `8bdfb006b32fbbc8889a190d7829f05e93e29787`，远端一致。[规格报告及最小不可变证据](system-smtp-delivery-ui-spec-verification.md)固定628612c产品、rev0.2全文f34d86…746ce与不变技术bee4f7…4cda；正式页首两版和原rev0.1／差量保留，不是本UI产品接受。

17候选（10新／7旧）只覆盖四API、test/retry两202回执、安全管理投影与原请求恢复、第九域实际Cookie owner尾部和当前View共宿主。提交后GetMailJob失败不证明写未接受，GET／Session无receipt资格；五种拒绝码须满足完整分类条件，先unknown保持粘性。原CSRF须仍等于同完整identity当前合法token，token变化更换epoch并销毁旧intent，不能换token续旧意图。

49逻辑Git引用去重47正式来源，五新／五旧selector及原预算仅获可执行性静审，未跑业务。原静态命令实际exit0／0.284s，只读Git／指纹／范围／格式；原retry204/header准备误述已纠正202JSON，页首内联节标记提取失败也保留。30逻辑原件以29对象＋正式卡Git引用归档，不复制整个源码／依赖／运行时树，不重跑产品。

主线程已另授唯一frontend作者17路径私有实现及适用离线检查，README末件最后；作者 `/workspace/scratch/agenteam-smtp-delivery-frontend-fpf9njbd` 四API阶段已实际启动，独立verification私有计划亦已实际启动。当前**未授真实资源，没有实现、浏览器、worker或实际投递PASS**；后续须冻结候选、独立验证及资源终局，不能从本规格记录外推。

本次仅报告／行政入口和新卡页首审查链接／授权状态，技术与旧§1–22保持。外部邮箱／所有TLS、完整SMTP页面及D07/D27／D08–D28/E01未完成、E01未开始；Summary待决、Object/tools原停止、Artifact/Project与生产未绑定／ready503等边界不变。

## 24. Central 嵌入 SPA 规格采纳与私有实施启动

§1–23保留原阶段事实。[正式卡 rev1](../work-items/d28-central-spa-hosting.md)已在三次独立静审后获有界 STATIC PASS，主线程采纳并提交推送 `7a490ac7d5b09c1fff564fc50af93bfe236c1d71`，主线程核远端一致。[规格报告与最小证据](system-central-spa-hosting-spec-verification.md)固定设计97f4551、前端628612c、被审rev0.2全文845290…1ab5a8及技术§1–7 f5399abe…967220。正式卡原页首保留其提交前事实，当前授权状态由本节追加说明，不改技术或旧历史。

rev0原三项静态接缝为初始化前纯构造、外层错误的安全instance／日志投影、既有data favicon与嵌入集合；rev0.1关闭三项但仍有未提交panic写500位于admission／期限之外的尾部问题；rev0.2限定SPA局部Recover与统一withWebResponse，实际写／Flush／stop＋join后才归还准入，拒绝503沿同预算。三次均为规格静审，没有对应产品动态RED或构建PASS。16路径未扩，只有既有app.go需要修改，其余15为新路径。

后端作者在 `/workspace/scratch/agenteam-central-spa-author-gfjizhr4` 已实际启动私有16路径实施，固定97f4551／web628612c并读取7a490ac卡；首阶段是bundle／handler与构建脚本的纯检查。独立验收私有计划也已实际启动并冻结plan／basis原件，未消费活动源码；计划不等于实现审查或运行。尚未运行发布构建、native或真实资源，没有任何这些能力的通过结论；正式单二进制／空CWD／真实登录深链接代表须后续验收。

本档18逻辑原件以16新SHA对象、正式卡Git原件及既有SMTP配置input04清单复用；33项必要Git引用不复制全树、依赖或dist。原独审只保存md／json，没有单独command／raw／环境／退出时长文件；不补造历史执行记录。旧36个dist／547593字节仅是原静审观察到的输入形状，不是D28发布证据。

SMTP投递UI的authority01失败已有静态产品归因，私有修复仍未验收，不能用旧§23的启动状态或本规格推成产品交付。完整D08–D28/E01未完成，E01未开始；Summary待决、Object/tools原停止、Artifact/Project与生产未绑定、ready503及其它未验边界保持。本次只是报告／证据与行政索引候选，不修改SPA技术正文、SMTP卡、前端README或任何产品文件。

## 25. Central SPA rev1.1 发布脚本边界与原失败归档

§1–24逐字保留。[卡rev1.1](../work-items/d28-central-spa-hosting.md)由主线程采纳并提交推送 `272c6c178462b4da72ac0eda69278e8dbde61f30`、远端一致；[报告追加§6](system-central-spa-hosting-spec-verification.md#6-rev11-发布脚本边界修订与原失败)绑定全文2cd71ed…17fbd8、技术cfa6d8…4056f8及原rev1 Git7a490ac。仅§2明确Linux启动任何工具前的/proc自有组PID/starttime能力门禁，及全部工具实际结束/可失败清理与锁操作先于最后一次binary rename的commit-last顺序；16路径、HTTP/API预算和其它技术原字节，普通无tag/Runner不变。

独立stageA固定两脚本与9源manifest。A-SCRIPT-01原child-tail01实际1/2.838s，direct退出后inherit/ignore两分支后代仍运行；探针补清理并等待原Promise，driver实际wait2个adopted。A-SCRIPT-02原publication01实际1/1.179s，受控lock inode变更在rename后触发错误，当前NEXT无匹配摘要；不是缺库/超时等前提失败。两轮共6个owned PID/starttime双扫空，输入未变，未运行监听/网络/真实构建/浏览器，不能将取证终局说成产品正常收尾。

新增独立rev1.1证据子目录，旧report正文、index、objects、checker均保留；新checker用6267717历史文档执行旧核验，再按272c6c1核修订和追加。只收两脚本、原manifest及必要probe/运行原件，26既有依赖用固定Git引用；其余7候选/原完整candidate.diff/作者检查及工具实体未收，故不声称旧全阶段可由此包直接重放。初rev1.1/v2/v3与各diff/check保留；据主线程最新调度通知，SPA stage02两脚本修复及原两probe作者重跑PASS已收到，尚待独立验收，修后产品未接受。

主线程另通知SMTP oldauthority02 actual0／59.566s、窗口释放，新五组和旧五组按版本通过；独立A/B仅六项离线检查进行中，仍未产品接受。以上是调度状态而非本档验收，本档未读取这些新增/活动原件。核心16产品、正式发布/native/两真实代表仍待验；完整D08–D28/E01未完成、E01未开始，Summary待决、Object/tools原停止、Artifact/Project及生产未绑定/ready503不变。这里是规格修订和原失败持久记录，不扩产品/资源授权。
