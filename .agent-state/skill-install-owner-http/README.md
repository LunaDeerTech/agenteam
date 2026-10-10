# Human Owner Skill HTTP

本片是既有普通安装 Service 的浏览器适配。源码基线及当前验证状态见 [current](../current.md)；公开契约见 [Skill Owner OpenAPI](../../api/openapi/skill-owner.json) 与 [工作卡](../../docs/development/work-items/d10-skills-owner-read-http.md)。

- 原 `GET/HEAD /api/v1/projects/{project_id}/skills` 保持一项已发布 builtin；原详情读保留。
- 新 `GET/HEAD .../skills/catalog?limit=25&cursor=...` 返回 serving 的 protected/ordinary Skill 八字段及可选 `next_cursor`，最多100项。每页独立当前授权和原读提交确认，不是跨页快照。
- 新 `POST .../skills`，body 为 `{"request":{"skill_id":"UUIDv7","mode":"create","source":{"kind":"text_files","files":[{"path":"SKILL.md","utf8_text":"..."}]}}}`。只处理既有文本包合同，元数据来自严格 SKILL.md；命令身份为单一 `Idempotency-Key`，RequestID 来自 middleware。
- 新 `POST .../skills/commands/lookup` 使用同 key 和完整原 body，只读原安装命令。失联/Unknown 不自动再次调用 Install，NotFound 不证明先前回滚；调用方保留原 intent。

原 Account CSRF/Origin/Session 与 Service 当前 Owner/Project gate 全部保留。HTTP 不接 Actor/AgentRun/Project JSON、宿主路径、URL、Object 或 Runner 标识。总请求1 MiB，原文本包边界继续生效；响应只公开三字段 receipt，错误不输出 body/依赖原文。旧读2s、新写30s包含 body/build/事务/Object/写出/close/join，原调用不在后台继续。

当前新增 pure tests 仅控 HTTP投影/分派、错误、原deadline，以及 catalog 签名、当前授权先于有界SQL。受控 Store/HTTP ports 不冒真实数据库或 Account 权限结果。真实安装链候选见下文；不得 SQL 种普通 Skill 或用 fake install Backend 代替 Service.Install。

本片必要纯检查已完成分版本补集：pure-01 的8新top及2旧HTTP top（59sub）race通过，Schema因旧夹具遍历每条路径的HEAD而失败，vet未执行；原整体FAIL保留。仅修本包testdata/schema.py的精确路由集合后，pure-02 Schema单top的45向量与30个无body HEAD状态、三包vet通过。两轮输入首尾相同，原Wait与group/runtime双尾闭合；原件为本树 output/ai/skill-install-owner-http/pure-{01,02}/，稳定入口run-{01,02}.py复用已验core-checks方法。未重复已通过10top或安装库/cleanup全矩阵。首次真实 HTTP/PG 组合已整体 FAIL，原结果见末段。完整初始化/Agent/F1/Backend 仍按各域真实绑定状态，生产 Project initializer 保持 unbound。

## 首条真实 HTTP 方法

唯一新增 [skill_installation_http_test.go](../../tests/projectvariable/skill_installation_http_test.go)，selector `^TestSkillInstallationOwnerHTTP$`，1 top / 2 sub：`install-lookup-catalog-and-read`、`current-owner-and-csrf`。只读复用 root 导入的 [domain fixture](../../tests/projectvariable/skill_installation_test.go)，实际 Account Bootstrap/Invite/Redeem/Login 与目标 Project 的真实 Skill/Object/P2 初始化均沿原方法；本 HTTP 文件不写业务 SQL、不调用阶段清理夹具。外人访问按真实 Project authority 隐藏为 NotFound。原组合使用 `7cf8cd15`，其后 root 已导入 Audit 查询修正 `7439e881`。domain 已修原 Project 名称、外人期望码和 CRLF 素材；原失败分别保留，不由本片改写结果。

两个正式 HTTP Handler 保留同一个 Skill Service、真实 Account HTTPBoundary 与 httpapi 中间件，用公开 `HandlesManagementRequest` 作精确分派。app 私有 router 仍由已通过的定向 pure test 覆盖；本候选证明适配器与真实持久服务组合，不证明完整 app root、原生 TCP、浏览器或生产 Project Create。原 ResponseRecorder 的 deadline/flush 是明确测试边界。

第一 sub 只提交一次 Human POST，核原 published installation/attempt/revision/Object reference/Audit 及无活 work/lease；同 body/key Lookup 取原 receipt；catalog 以 limit=1 连续两页精确消费 builtin+ordinary 两条，旧 builtin-only collection 与 ordinary detail 的 GET/HEAD 保持一致，读取前后项目事实不增。第二 sub 验缺 CSRF、非 Owner 拒绝，再以真实新登录先正向读 catalog；在 RequireHuman 后原 body 首读调用正式 Logout，原请求同步退回 401/清 cookie/闭集安全 Problem，所有安装事实不增且新 target 不存在。无重试、无安装成功替身、无从纯控外推 PG 通过。

HTTP 测试方法已由非作者有限独审接受。HTTP 自用单文件 SKILL.md 素材已按真实 NewTextFiles/BuildPackage 合同核对：固定两行 frontmatter、非空正文、UTF-8/LF、无 CR/NUL；没有另造受控安装成功。编译与首次组合运行事实见末段，方法接受不代动态通过。

## 复用的组合入口

沿已接受的 generic metadata/schema/Installer root-chain，新增 HTTP 单独 profile 与 `^TestSkillInstallation(PersistentObject|OwnerHTTP)$` 组合 profile，后者精确2 top/4 sub。两项各自构造原独立数据库、随机 bucket/Project/命令身份，未 Parallel；每项原清理完成才进入下一项。保留旧 metadata 默认、schema、Installer 单项和其他 namespace；只增加必需两 Go 路径、精确 cases 与安装族 root-only 模式拒绝，不更改6m/540+60/七资源/Wait/完整 TCP 双尾。

本域 [entry-controls.py](entry-controls.py) 去除本次唯一已知增量后，两个 shared 全字节恢复到 `f6ebeb84`；旧 Installer→schema→metadata 逆投影链只增加明确的 HTTP 剥离入口。新4方法和受影响旧逆投影2方法实际0：调用真实 family dispatch/result parser、实际 input collector 的初尾枚举及真实 observe_root_chain，外部资源观测为明确受控 double。缺失/重复/FAIL/SKIP/错误或缺 Wait、新输入/缺源/软链、资源/private/runtime残留、未知hunk/预算或Wait弱化均拒绝。这些纯控制不证明真实业务。

`output/ai/skill-install-owner-http/compile-01-launcher.py` 复用 schema compile03 已验的 original Wait/desc/group/runtime 方法，只替任务路径与精确目标；固定新 artifact `installation-combined-race-01.test`。一次 race-c 后 exact list 两项，然后同 binary 仅执行无资源 `TestSkillInstallationPackageInput`，要求真实 RUN/PASS/Wait0，成功才可进入组合 PG。文档/纯控制不加入新运行 manifest。原 domain PG 与 HTTP pure 的不同版本结果分别保留，不重复旧矩阵。

## 首次组合实际结果

- source `c1b80b88`，compile01、exact两top list、原素材preflight均实际PASS；outer805501、compile805502、list805612、preflight805639的原Wait0和group/runtime/desc尾齐，545编译输入不变。候选47,980,133B，SHA256 `72d07f3f71d9b70ad45e496b27968a73a696d6ddf68cf40c42fb158f999f7cc2`。这仅是编译及素材正常结果。
- combined01 **wholeFAIL**，2026-10-10 13:52:29–13:54:16 UTC；outer806424、supervisor806492、driver806493、Go808336均原Wait1。HTTP top7.68s/首sub0.19s，domain top6.75s/首sub0.30s均FAIL，各自第二sub未RUN；exacttop/actualWait存在不代完整cases通过。
- 原7IDs两轮absence/private/runtime/desc/HOST_TCP双空、outer退出尾和1227输入首末一致均闭合，supervisor105.778s。原log `/tmp/sih01/pg-adffd04d79a8413bbba40f4d574f54c6.log`；outer原件 `output/ai/skill-install-owner-http/combined-01-control/result.json`，编译原件 `compile-01/`。热cache/资源窗口已释放，无自动retry。
- 首错是HTTP:142/domain:359的只读复合SQL后验。原domain安全记录为err=nil、installations/attempts/revisions/references/uploads/liveWork/leases=`1/1/1/1/0/0/0`；两个谓词均误写不存在的 `object.upload_complete`，正式动作是 `object.upload.complete`。HTTP原日志没有各分项，不能补认；只能按实际先行断言确认POST receipt、canonical Get、published Object绑定已越过。Lookup、分页、拒绝及domain cleanup仍未获本轮证明。修后两处查询均参数化使用正式 `ac.ObjectUploadComplete`；HTTP 仅附加 `%T` 错误类型与六项计数诊断，不输出错误文本或请求材料。domain 修理由原作者完成、root 导入；产品与断言门槛未变。该修正在后继 combined02 原正常链中实际越过，不回填原 FAIL；原候选及输出保留。

## 组合02与会话拒绝夹具修正

source `71e6231b` 的compile02、exact两top list、同binary素材preflight原Wait0/全部退出尾通过，545编译输入相同；新候选47,983,349B，SHA256 `49d85fb7682819d42c3536c90c90e1715ce2687dc5e7811c6b4694e383a5cb36`。原件 `output/ai/skill-install-owner-http/compile-02/`。

combined02原 **wholeFAIL**（2026-10-10 13:59:29–14:01:18 UTC），outer814944/sup814990/driver815012/Go816731原Wait1，七ID14absence/private/runtime/desc/HOST_TCP双尾与outer原尾齐，1227输入相同，supervisor107.575s。HTTP安装/Lookup/catalog/旧GET-HEAD sub PASS0.34s，当前Owner/CSRF sub FAIL0.75s，HTTP top8.81s；domain安装/读取重放及普通清理两sub全PASS（0.30/1.06s，top8.37s）。原件 `output/ai/skill-install-owner-http/combined-02-control/result.json` 与 `/tmp/sih02/pg-a81ae562adbc49e8bb33aac838d0b634.log`。这些有限通过不改变组合整体FAIL。

第二首错是HTTP:229的复合拒绝断言。正式 `Account.HTTPBoundary.WriteProblem` / `httpProblem` 两处固定 `/api/v1`，不得回显原路径；夹具却期待原collection，独立源码复核确认该期待必错。原SessionRevoked与严格Problem解码已过，其余复合分量未逐项记录，不补认；最终目标零事实查询尚未到达。只修该instance literal，并把同一复合门拆成安全布尔与前后八计数诊断，仍要求reached/401/clear-cookie/固定type-title-detail/无字段或retry/事实不增，不输出Cookie、Problem原文或请求材料。已静读正式Writer、清Cookie及Problem合同，未见其它确定静态偏差；这不代运行结果。修后尚未编译或运行，产品/预算/所有其余断言保持，下一候选与组合03另用新编号。
