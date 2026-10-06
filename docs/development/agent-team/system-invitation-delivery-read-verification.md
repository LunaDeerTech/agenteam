# System 邀请最近投递读口验收记录

结论：**PASS，仅限 [D27 邀请读口 rev1](../work-items/d27-system-invitation-delivery-read.md)的八路径后端与迁移 00019**。主线程已采纳并提交推送 `9b3201547f9b7b61fd9716a6ba6540084961496c`，主线程核实远端一致。最终 input04 SHA-256 为 `41382f9f482ddad686d76ae1492361031fe6a2485fa012c2a10708ee09b9fe12`；本次归档逐固定 Git 核八源与两锁相同。邀请 UI 与完整 D27 不属于本结果。

当前管理员的 `GET /api/v1/system/invitations` 在原五字段之外返回闭合 `latest_delivery`：按最终接受 command 的时间与 JobID，跨同一邀请的全部重发及人工重试周期选择最近任务，并报告该任务 current attempt 的真实渠道与结果。有效邀请、分页与投递投影来自同一 SQL statement；当前授权在同一事务核实。无效最新来源或任一坏行均返回零候选，不回退旧成功。GET 不清理数据、不发送邮件；原写 receipt、User/MailJob 与邀请 HEAD405 保持。

## 1. 输入、职责与证据复用

产品基线为 `7ef3e30cf06b5df6d19516f24c34855a8308ef05`。`directory_backend` 实现和自测，未参与实现的 `recovery_verification` 独立审查及真实验证，`recovery_documentation` 只归档冻结原件，没有重跑业务或启动资源。

| 输入 | 实际变化与证据 |
| --- | --- |
| [input01](system-invitation-delivery-read-verification-evidence/author/inputs/input01.json) | 八源初版与两锁；author01 的六个通过顶层及 Selection 首红绑定此版 |
| [input02](system-invitation-delivery-read-verification-evidence/author/inputs/input02.json) | 仅 Selection 测试文件：修 planned-command gate 装配，foreign current fixture 从 jobID 改为另一真实 attemptID，并增加实际 advisory 锁等待观察 |
| [input03](system-invitation-delivery-read-verification-evidence/author/inputs/input03.json) | 两个测试文件调整：独立证明实际局部三秒/尾部 join，真实 PG 用例尊重原一秒锁超时 |
| [input04](system-invitation-delivery-read-verification-evidence/author/inputs/input04.json) | 仅 Selection 测试文件：修原 PG 取消映射假设，补自有 reader/晚规划命令的 cancel 与实际 join 清理，改为八个并发 reader 并逐页核目标恰一次，见锁等待且确认 live 后再设 expiry。八源正式交付；四版生产字节和另外两个新测试文件均相同 |

[原作者报告](system-invitation-delivery-read-verification-evidence/author/author-report.md.txt)、[独立原报告](system-invitation-delivery-read-verification-evidence/verification/verification-report.md.txt)及其[结构化记录](system-invitation-delivery-read-verification-evidence/verification/verification-report.json)完整保留命令、输入、结果与限制。[去重索引](system-invitation-delivery-read-verification-evidence/archive-index.json)将每个原绝对路径映射到精确归档字节；用固定 Git 基线加对应八源即可重建四版，不复制整棵源码树。

## 2. 作者实际检查、原三红与七组组合

pure02 的六条 full Account ordinary/race/vet、两集成包筛空运行/vet、Central/Runner 构建均 exit0；生产与 mail/HTTP 测试未变，按原范围复用。pure03 两条、pure04 五条、pure05 两条受影响检查也均 exit0。精确 argv 在各原 `result.json`；`go test -tags=integration -run '^$'` 保留其真实写法，`[no tests to run]` 不计动态通过。历史 pure03 没记录 env，未补造；最终编译/vet另有 pure04/pure05 与独立 compile01 的完整记录。

| 真实轮次 | 实际退出 / 墙钟 | 原结果与归因 |
| --- | --- | --- |
| [author01](system-invitation-delivery-read-verification-evidence/author/runs/author01/raw.log) | 1 / 118.320s | 六个顶层 PASS；Selection FAIL，测试将 Authority 装到 raw Store，绕过自有 planned-command gate |
| [author02](system-invitation-delivery-read-verification-evidence/author/runs/author02/raw.log) | 1 / 58.519s | Selection FAIL：新断言误要求至少三秒，实际原 fixture 的 DB lock timeout 为一秒 |
| [author03](system-invitation-delivery-read-verification-evidence/author/runs/author03/raw.log) | 1 / 48.290s | Selection FAIL：新断言误要求所有 PostgreSQL 取消错误都满足 `errors.Is(DeadlineExceeded)` |
| [author04](system-invitation-delivery-read-verification-evidence/author/runs/author04/raw.log) | 0 / 53.908s | 原完整 Selection 顶层 PASS 3.65s；修正测试假设，没有修改生产取消/错误映射或放宽预算 |

最终作者结论是 **author01 六组 PASS + author04 Selection 一组 PASS**，不是一次七组全绿。author01 六组分别为旧管理员页面 6.20s、旧邀请/邮件管理 4.95s、新邀请 HTTP 6.59s、已接受用户目录 HTTP 7.34s、迁移与规模计划 10.97s、实际 SMTP/受限日志 10.93s。完整名称与选择器见[原命令](system-invitation-delivery-read-verification-evidence/author/runs/author01/command.json)和独立复核原件；未改的迁移/计划完整代码段 SHA 为 `c71803406c7e0fc7d9087e303e4ac13627d31a3b6e3cfdb2351895cfd5ebdef2`，离线再核 author01 与 input04 字节相同。

author04 实际走完早规划/晚接受排序、旧任务晚完成、跨 root 的人工重试及 retry-of-retry、八个并发完整页、同 key 重放/时间并列、坏时间/缺失或外来 current attempt/错误 fence 零候选、锁后到期、同邮箱新 ID、当前权限与正式撤销。前面失败轮尚未到达的分支不计通过。HTTP 与 mail 组另覆盖分页/cursor、重发不移动邀请页界、真实兑换/撤销、原 HEAD405，以及日志→SMTP 配置切换、unknown/retry_wait、后续 claim 替换真实尝试事实和配置失败不兜底日志。

## 3. 三秒、PG 一秒与实际调用尾部

ListInvitations 的最大三秒只覆盖该 facade 的事务、锁、查询与扫描，继承更早 parent；不包含之前 HTTP 预认证或之后编码。pure04 的普通/race 原日志实际等待原 query context，局部到期分别为 3.002108959s / 3.001275155s，更早 parent 为 75.268703ms / 75.382592ms。held query tail 未释放时 facade 不返回、operation 仍在；释放后实际 join，结果 NotCommitted、保留 DeadlineExceeded、零 items/cursor，最终无遗留 operation。

真实 PG author04 的较早 parent 在 68.108ms 后实际 join，SQLSTATE57014，原映射为 InternalError/NotCommitted/LockFailed；caller 已 DeadlineExceeded，但 `error_is_deadline=false`。真实 DB 锁超时为 1.002681674s、SQLSTATE55P03，caller 仍 live。**这两个实库结果与 pure 的实际三秒证据分开成立**；一秒实库超时不能冒充三秒实库等待，context 到期也不等于调用已经 join。

## 4. 迁移与真实规模计划

00019 仅增加 `agenteam_account.delivery_intents(link_id,id) WHERE kind='invitation'` 的 `account_invitation_delivery_link` 索引，使用原事务型迁移；迁移 00001–00018 与 Go 锁逐字节不变。规模为确定性 SQL fixture：30 个 live 邀请、100000 个无关 intent，以及一个邀请的 2048 个历史周期加 live root，共 2049 条匹配历史。正常 limit26 包含 limit+1 哨兵；这些直接播种不代表业务发送。

索引实施前的 [plan01 原日志](system-invitation-delivery-read-verification-evidence/author/runs/plan01/raw.log)记录决策事实：正常页 216.105→0.392ms，偏斜页 11.990→4.066ms。**plan01 两份旧测试源码原件没有定位到**，其当时输入指纹和 raw 保留，不从当前文件反造旧源码、不宣称该轮可完整重建。

最终可重建规模结论来自 author01 冻结源码与完整原 raw：包括实际 job/attempt fixture 后，正常页 224.587→0.701ms，偏斜页 12.614→3.728ms；正常页原 26 次 intent Seq Scan、每次过滤 102077 行、43524 hit buffers，索引后 26 次 link scan、101 hit + 3 read。11 份原 EXPLAIN JSON 均嵌在 raw 中，离线重新解析；仅保留轻量摘要，不另复制完整计划。正常/后续/空 cursor 页、时间并列、偏斜及 all-expired 零行计划均保留；未强制 planner、关闭 seqscan 或缩减规模。

fresh1–19、populated18→19、历史行指纹、精确索引定义、私有故障迁移回滚/journal，以及修复自有失败依赖后使用同一 pending 文件字节恢复均实际通过。索引避免逐页面行扫描无关历史，但偏斜邀请仍扫描自己的 2049 条历史；没有常数复杂度或无限规模 SLA 承诺。

## 5. 独立两项重点与真实终局

独立两份私有 Go overlay 首轮实际通过；[原命令](system-invitation-delivery-read-verification-evidence/verification/runs/independent01/command.json)与[raw](system-invitation-delivery-read-verification-evidence/verification/runs/independent01/raw.log)均保留，raw SHA 为 `fc39453f682e209294f9fab5e30071c823f0cc975c2ae1d3eedf2501690bda75`。原 `test-security.sh` 的 race/count1/6m 及 fixture 预算未变；独立 compile01 exit0/89.223s 只算编译，其他包的 no-tests 不算兼容通过。

| 独立顶层 | 实际重点与结果 |
| --- | --- |
| `TestInvitationIndependentHTTPAtomicProjection` | PASS 6.74s。三条正式邀请；未返回的 limit+1 哨兵绑外来 attempt 时503零页；正式 resend 新 root/cursor 稳定；最新坏 initiator 不回退旧成功；投影逐 DB 等值；普通用户/降级 Session 携旧 cursor 拒绝；正式 revoke204 实际空体仅移除目标行 |
| `TestInvitationIndependentMailOriginsAndLifecycle` | PASS 10.34s。真实日志成功后切 SMTP，SMTP 接受后丢回复得到 retry_wait/smtp/unknown，另一邀请保持 backend_log/sent；再正式重试较旧 root，接受时间较新的 child 胜出且 attempt 字段为 null，同 key 同 JobID；捕获 Actor 后降级仍 Forbidden/零候选；GET 排除到期行但保留 DB，正规同邮箱新建隔离新 ID/旧历史 |

实际环境为 Go1.27.1、readonly/offline 锁依赖、PostgreSQL17.8/vector0.8.1、原固定镜像及 MinIO binary SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。PG16 是原组合 fixture，本轮没有重新声称最低版本拒绝验证。直接 SQL 修改仅用于自有坏映射、冷却/到期和成本 fixture；创建、重发、重试、同 key、日志/SMTP 与撤销均走正式服务。

| 运行 | 实际 exit / 秒 | exact 资源 / PID-starttime | 终局 |
| --- | --- | --- | --- |
| plan01 | 0 / 192.986 | 7 / 277 | 实际 wait、双清 |
| author01 | 1 / 118.320 | 9 / 94 | 实际 wait、双清 |
| author02 | 1 / 58.519 | 7 / 77 | 实际 wait、双清 |
| author03 | 1 / 48.290 | 7 / 74 | 实际 wait、双清 |
| author04 | 0 / 53.908 | 7 / 71 | 实际 wait、双清 |
| independent01 | 0 / 145.337 | 9 / 212 | 实际 wait、双清，窗口交还 |

每轮全部 exact ID 两次 absent，所属 PID/starttime 均消失、原两容器/四网络 baseline 不变、runtime 空、monitor 错误0。独立命令结束于 2026-10-06 06:11:41.145984 UTC，双清为 06:11:41.347722 与 06:11:42.036120 UTC。各 driver 启用 subreaper；本包每轮 adopted-waits 均为空数组，没有虚报额外 wait。源与验证输入前后指纹不变；author01 期间记录的 HEAD 因文档提交变化，源码哈希保持相同。

## 6. 归档检查与保留边界

[最小证据说明与离线脚本](system-invitation-delivery-read-verification-evidence/README.md)保存 168 项逻辑原件、101 份去重物理原件，共 1,578,315 字节。只读核原字节、四输入、交付 Git、两锁与迁移1–18、00019精确索引、15条作者检查、独立编译、六轮原结果/实际退出/资源终局及21份 raw 内计划；不执行历史脚本、Go、网络或 Docker。原 raw/diff 的空白按原字节保留。

独立离线分析曾将多行 EXPLAIN 误作单行 JSON、将 helper 边界名拼错；原 review 记录保留两次准备错误，纠正后从同一原件复核，没有新增业务轮。跨卡历史归因也保留：旧 producer 只产生 pending/cancelled，B03 后先绑定 exact current attempt/fence 再写终态；SQL CHECK 或 retryEligible 的宽松准入不证明正式存在 sent/failed 无 attempt。下游 UI 规格已收窄这句话，读口 scanner 没有放宽；约定的旧 processing/unknown 兼容保持。

本结果不包含邀请 UI、浏览器、生产部署、邮箱实际到达承诺或新投递写能力。旧用户目录 UI 的迁移1–18验收不自动覆盖本次00019；邀请 UI 后续须固定自己的实际组合。Summary 待决、Object/tools 原停止任务、Artifact/Project 阻塞、生产未绑定和 ready503 保持；完整 D08–D28/E01 未完成，E01 未开始。原 Account test-security 的必要 Object fixture 不等于恢复被停止的 Object 原任务。
