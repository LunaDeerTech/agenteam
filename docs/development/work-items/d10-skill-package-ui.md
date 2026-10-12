# Skill 固定修订包下载 UI

状态：2026-10-11 当前 Human Owner 的详情下载正常链、浅深两图与资源全尾已有限独立接受，随本次变更交付。产品与测试由 `skills_milestone_review` 实施，`skill_package_review` 未参与实现并独立核过产品、方法、原件与图片；其 outer 终态结论引用作者亲见原工具退出。

## 2026-10-12：Owner 导入已有单文件包

后继已完成真实默认 App 的导入闭环：先真实创建并初始化新 Project，Owner 从磁盘选择已有 `SKILL.md`，取消零安装请求，确认后按原文件字节唯一安装，经目录、详情下载固定 revision 的 ZIP。浏览器保存包、Go 原安装 key 的 Lookup、Skill/Revision/Object 引用与包内原文件字节均核对；不提供手工元数据/正文编辑器，不生成或静默规范化 frontmatter，不增加 Agent 分配、脚本执行或 ZIP/Git 上传能力。支持范围仅为现 `text_files` 协议下的单文件标准包；下文 2026-10-11 的未安装/initializer 未绑定等表述保留为当时历史，当前默认创建能力沿已交付的 Project 创建与恢复闭环。

前端新增 7 项及原目录/下载 24 项共 31/31 通过；首次 tsc 的 nullable 控制器错误及后继单行修复保留，修后 tsc/build 通过。真实 [browser04](../../../output/ai/skill-package-import-delivery/browser04/result.json) 为 `TestSkillPackageImportWeb` 1 top / 1 sub、32.08s PASS，原工具 `43432/d11704` 实际 0；[outer 原 Wait](../../../output/ai/skill-package-import-delivery/browser04-outer.json) 为 0 / 67.577s。Node、Go、四个 Chromium 原 Wait 成功，六个本轮资源及私有目录实际退出；1440 浅色与 390 深色图由非作者亲看接受。

原 [browser01](../../../output/ai/skill-package-import-delivery/browser01/result.json)、[browser02](../../../output/ai/skill-package-import-delivery/browser02/result.json)、[browser03](../../../output/ai/skill-package-import-delivery/browser03/result.json) FAIL 保留：01 为 required 标签定位不匹配，尚未上传；02 安装 POST 被旧 GET 终态观察门拒绝，其未保存的具体浏览器失败类别仍未知；03 固定修订包 GET 的真实 `ERR_ABORTED` 已有原落盘及 Go 收尾，但缺原 binary native/public 返回联合证明。两次观察补差均只限本片精确请求：04 安装与固定修订下载必须同时绑定原请求、原 EOF/关闭、实际公共 receipt/Blob 返回、Go 同一响应和保存字节，才接受精确已知终态；旧默认元数据与下载规则未放宽。不回填前三轮缺失事实，不宣称真实丢包恢复、旧 STOP 或完整 D10 生命周期通过。

## 可见行为与边界

在已有 initialized Project 的 Skills 目录进入详情，用户下载当前详情所示的固定 revision。契约沿[包 HTTP](d10-skill-owner-package-http.md)、[OpenAPI](../../../api/openapi/skill-package.json)和[项目设置设计](../../frontend-design/layouts/project-settings.md#31-项目-skills-库与分配)。revision 保持正 int64 原字符串，单次 GET 使用当前 Session/Owner 授权；不先 HEAD，不自动重试或调用命令 Lookup。页面提供进度、取消和安全失败反馈，重新下载须由用户明确操作。

前端复用唯一 client/Session 请求 owner。响应必须为规范 ZIP 附件头、准确正 Content-Length 且不超过 33832982 B；完整读取、EOF 与魔数检查之后才交浏览器保存。canonical ZIP/manifest/摘要完整性由后端负责，前端魔数不代表完整 ZIP 结构验证。取消或身份、Project、详情、revision 换代退役旧请求，原 read/cancel 实际退出前不释放 owner，清理临时 buffer/Object URL，旧包不再发布。

本片不提供安装、Agent 分配、历史版本选择器或执行能力。生产 Project initializer 仍 unbound；Object Runtime join、SPA concurrent-publication 及其他原 STOP 不变。真实正常链不代下载中取消、撤权竞争、持久释放 Unknown 或完整 D10 权限/生命周期矩阵。

## 验证与原失败

产品固定 `197296bc`：客户端/状态/页面 4 文件 39/39、共享 Session/Account/Knowledge/Milestone 回归 5 文件 60/60、vue-tsc 与 Vite 构建通过，原命令和实际终态在 `output/ai/skill-package-ui/checks-01/`。原作者语法、测试环境及切换断言失败保留，后继定向复验通过，不写成首轮全绿。

真实入口为 [TestSkillPackageWeb](../../../internal/central/app/skill_package_web_test.go) 与 [Playwright spec](../../../tests/account-captcha-web/e2e/skill-package.spec.ts)。隔离的正式 Project/Skill 服务准备已有 Project 和已安装 Skill，默认 App、Account/Session、PG、MinIO 与原 package HTTP 实际运行；测试专用 initializer 不改变生产未绑定状态。浏览器从目录进入详情，键盘触发下载；实际保存文件长度/SHA 与原 HTTP 字节、后端 canonical 包三方一致。每条 API 响应以同 requestID 绑定真实 handler host、EOF、Close 和返回；六条元数据还绑定原 native reader 与公共 consumer 的当前 typed digest，包 GET 强制原 requestfinished。

browser01 原整体 FAIL 保留：Project GET 200 但原 requestfailed，初版严格观察拒绝，尚未到详情或包。原件未记录 failure.errorText，不能回填为 ERR_ABORTED。最小测试补差只复用现成 metadata reader/consumer 观察，对闭集元数据的真实 `net::ERR_ABORTED` 要求完整联合消费证明并保留 failed；包下载仍不接受该分支。九项离线观察控制、三文件语法和唯一 selector 列举通过；引入旧共享 helper 后 strict TypeScript 仍有两个既有诊断（2345、2322），固定基线对照新增零，不称整个共享测试程序 strict 通过。

browser02 固定 `0a6adca4`，复用未改 Go binary 与 fresh `dist-01`，原 Go 17.91s PASS，outer 会话 95837 实际 exit 0 / 117.435s。七条 GET 全部 200/finished，实际包为 365 B，SHA256 为 `baaa8fe3ca5e87ff17a2481aaa7c60e4daf3d3bfcdaa51d57b6fd9420d38ead2`。两图分别为 1440 浅色详情与 390 深色下载完成状态，品牌解码、键盘焦点与横向溢出断言通过；未将 metadata aborted 补证分支宣称为本轮实际执行。

Node、Go、driver、supervisor 与四个 Chromium 的原 Wait 全部成功，七资源 14 次 absence、private/runtime/descendants/HOST_TCP 双尾与输入未变全部闭合。业务/视觉/资源限定独验接受，窗口已交回 root。原件在 `output/ai/skill-package-ui/browser-native02/`、`browser-evidence-02/TestSkillPackageWeb/`，原 supervisor log 为 `/tmp/spw02/ui-31ce433b411848be.log`；browser01 原 FAIL 与完整失败尾保留在相邻 `browser-native01/`。

## 复用入口

完整 argv/env 在 `output/ai/skill-package-ui/browser-native02/invocation.json`，实际结果为同目录 `result.json` 与 `tool-terminal.json`；invocation 原 preparation note 不代表最终运行状态。输入使用原 `root_chain_driver.py` 的精确 `^TestSkillPackageWeb$` profile，effective target 为 `internal/central/app`。编译使用 Go 1.27.1、`-c -race -tags=integration`、离线只读模块和单一 cache writer；前端资产必须构建到全新目录并确认唯一 Session bundle。

重建沿已保存的原监督链，分配自有 fresh 输出和证据目录，传 `AGENTEAM_SKILL_PACKAGE_WEB_DIST`、`AGENTEAM_SKILL_PACKAGE_WEB_EVIDENCE`，以 `pg_only_supervisor.py --driver .agent-state/work-owner-http/root_chain_driver.py --binary <race-test-binary> --run '^TestSkillPackageWeb$' --output <fresh-short-directory> --root-chain` 执行。实际依赖/缓存路径先按环境确认；需要共享 hostTCP 或 cache 时由 root 协调原全尾交接，不另造替代服务或扩大旧门。
