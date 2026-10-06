# System 邮件任务管理读口验证与接受报告

2026-10-06，[D07 管理事实读口 rev1.2](../work-items/d07-system-mail-job-management-reads.md)八路径已完成作者新三组／旧两组及独立风险验收，由主线程采纳提交推送 `819aba1b8f764328f1e2e67b53c274fad0db877d`，主线程核远端一致。本次接受两个后台管理 GET，不接受 SMTP 配置、测试／任务／重试 UI 或完整 D07/D27。

## 1. 交付与行为

新增 `GET /api/v1/system/mail-jobs/management` 和 `GET /api/v1/system/mail-jobs/{id}/management`，GET-only，HEAD／其他方法405、`Allow: GET`。新平坦对象九个 required 字段，reason 可省；kind 来自 intent，`attempt_channel`／`attempt_result` 必需但可 null，严格映射 exact current attempt。旧 `channel` 保持兼容摘要：无 attempt 时随当前 configured 映射，不能用作真实尝试或下一次发送保证。旧 MailJob、InvitationDelivery、写 receipt/schema 和原 GET／retry 契约保持。

列表单 statement 先 materialize limit+1 intent，再核双向 job 映射及 current pointer／job／fence；哨兵也必须通过严格扫描。每页默认25、最大100，Go 最多101个安全候选，专用 cursor 与旧资源分域。当前管理员授权在同一读事务内执行；Close/Err、坏映射、取消、签 cursor 或事务 Unknown 均零候选。局部预算从两条新 HTTP dispatch 的预认证之前开始，最长3s且继承更早 parent，实际调用与事务尾部 join 后才返回；已开始写出的 HTTP 字节不承诺撤回。

GET 仅报告当前完整事实，不是历史命令 receipt，也不授 retry 权限。POST retry 仍由原事务裁决 source version、终局／实际 join、同根忙与当前配置／材料；same-key 返回子周期当前 version，可已大于1。Account 没有公共 lookup，GET 的成功／失败都不能确认或否定旧 POST 是否接受；后继 UI 须保留原 key/body/上下文供用户明确重放，不能由本读口自动重试或轮询。

[八路径清单](system-mail-job-management-reads-verification-evidence/objects/a18068de3cf3a1384fcd970da88837fa26020d6b85081c1388e10026e502461b) SHA256 `a18068de3cf3a1384fcd970da88837fa26020d6b85081c1388e10026e502461b`；最终 `input04/manifest.json` 为 `4e3bee7144106e9b183977a8306314f08732a18ef54111997fbfb9a3a7ada5c4`。实际私有编译／运行基线固定 `3c79fd4069837c4cee0b1ed37e00480ea8f1909f`，八候选之外935文件、Go1.27.1的3501运行时文件指纹／452包／31锁定模块／9执行入口及固定 MinIO 均有原门禁。本归档核八源与接受 Git 对应，不用活动工作树代替受测基线。卡技术§1–6保持 `137e6a302a1f1a2b49076d6406ded7ff61a3c3ffccc939d4fb909640d8f176bc`。

## 2. 作者输入、原红与组合

[作者阶段报告](system-mail-job-management-reads-verification-evidence/objects/2af3123baed4dd8dfc9c3a06a4abfaa8a9bcf5f41503d59c6dae1dedf3792fe2)和[旧回归增补](system-mail-job-management-reads-verification-evidence/objects/f57e2281f458b3107139e9ccfff500b61647a7ef3b593c450f409150ac19f746)保留当时“独立／集成待验”的原文；当前最终接受由本报告及[独立终局报告](system-mail-job-management-reads-verification-evidence/objects/79c928efc63c5f033e2396540fe7c1486f23e62b71682fae83964e0229e552e2)接续。五个 API 阶段源（含纯测）冻结后同字节，后续三次 input delta 只改新增 integration 测试。

| 输入／失败 | 原事实与修订边界 |
| --- | --- |
| api-pure01 | 测试符号和 unused import 编译错误，actual1/32.165s，没有行为通过结论；精确旧 source、command/raw/result 保留，后续 api-pure02/03 通过。 |
| input01→02 | 静审 P-MAIL-TEST-01：completed_at 与正式 schedule 为两次时钟，原10–11s硬上界错误，未标成真实运行首红。仅测试采用同次 DB 观察间隔，保留10s下界、原12s due期限及后续强断言，不改 next_at／策略。 |
| new-read01／input02→03 | HTTP 原400错误码预期为 INVALID_ARGUMENT，实际 CURSOR_INVALID；ReadBudget 同轮完整通过。原 raw 没有逐 query 标记，8193B定位来自固定循环及旧 httpListQuery 分支。仅该期望改正，new-http02 完整执行原未到的后段。 |
| new-attempt01／input03→04 | 原卡／测试期待撤销后 worker ResourceDeleted，实际正式 Revoke 已将 pending job 置 cancelled，worker 按原规则 ResourceBusy。旧轮后三子例未到，不能补记通过。rev1.2 与唯一测试 delta 改为先读 cancelled/null，再硬验 Busy、完整 DTO／DB五计数／SMTP计数不变，生产写服务未改。 |

new-attempt02 完整通过三个 kind、日志／受控 SMTP sent、failed、unknown终局及 retry_wait/unknown、真实活跃 attempt/result=null、下一 claim 更换 current attempt 与实际 join。配置切换不改历史真实渠道，无 attempt 的旧摘要可以变；正式 revoke、manual retry、同根忙和 same-key 当前版本原规则保持。真实 backoff 差值10.472498s、观察间隔0.025851s，在原12s期限内自然到期，没有 SQL 改造 phase/result/next_at/attempt/fence。

作者全 Account pure、race、vet、integration 编译／vet与 Central/Runner 构建通过；api-pure03 含新增九个纯顶层及原 OpenAPI 路由双向检查。`-run ^$`、`-list` 和 driver gate 只计编译／发现／门禁。新三组由 new-read01 的 ReadBudget、new-http02 的 HTTP、new-attempt02 的 AttemptFacts组合，旧两组由 old01通过；不是一次最终全绿重跑。

## 3. 独立预算、权限与真实尝试

独立 pure01 的候选退役组两例通过：合法页实际 Close 未结束时取消，以及坏 exact fence 哨兵，均实际 Close 后零页／cursor。另一预算组因私有断言误将完整 HTTP 三事务当一事务失败，整轮仍exit1。pure02 仅复验该预算组两例，actual0/13.792s：真实 context 分别自然3s与更早150ms到期，受控尾部屏障未放行时仍持 operation／零响应，实际 join 后安全503、auth1/tx3/单读/active0。原 compile-list01 的私有 Database.Connect 签名错误和精确源也保留；修后编译／发现不是动态验收。

独立 A 在 independent01 完整PASS3.05s：真实150ms父期限调用154.411383ms后返回 typed NotCommitted/LockFailed、SQLSTATE57014和零候选；两正式读实在自有 PG advisory 锁等待时通过正式 Logout 撤销 Session，释放后 list/detail 都 SessionRevoked，每个原事务当前授权1次、候选SQL0、零读取副作用及实际 join。A直调正式 facade／PG，不称 HTTP 端到端；HTTP预认证预算由上述纯 handler及作者正式HTTP组补足。

独立 B 在 independent01 已走过真实 SMTP 活跃/null、丢 accepted 后 unknown与worker/attempt实际 join、正式 unconfigure/manual retry及子周期 log DTO，随后私有 DB 断言错误地期望公开 `backend_log`，原整轮exit1。原综合条件没有逐字段日志；[原归因记录](system-mail-job-management-reads-verification-evidence/objects/9505c7f188afd1c9772ee22cc2268caded1000eb4b8cff62b91e87887a2f0c7d)据固定 `RecoveryLogChannel="log"`、正式 writer、scanner 和私有 SQL 说明错误，不追填该轮 DB 字段。修正仅改为正式常量，公开 backend_log／exact ID／result／terminal／io_joined及后半强断言保持。

independent02 只复验 B，完整PASS4.58s。真实 unknown旧 exact pointer/fence/终局不漂移，实际 join 后才正式 retry；源version增加1、新周期无attempt时null，真实受限日志完成后sent，原 same-key 返回子周期当前version。重复读／重放没有新增 DB或SMTP工作。A 原源同字节复用；未把首轮失败或其未到达后段改记通过。B 使用自有受控SMTP和受限日志，不证明外部邮箱送达。

## 4. 七轮真实命令与资源终局

每轮原 command 保存私有 cwd、`sh scripts/test-security.sh -run` 的精确选择器及环境，Go1.27.1／race／count1／原6m及fixture内部预算保持。`[no tests to run]` 不计真实通过。

| 真实轮次／输入 | actual exit／秒 | 原顶层结果 | exact资源／所属PID |
| --- | --- | --- | --- |
| author new-read01／input02 | 1／116.618 | HTTP FAIL5.23s；ReadBudget PASS4.12s | 7／113 |
| author new-http02／input03 | 0／55.529 | HTTP PASS5.45s | 7／69 |
| author new-attempt01／input03 | 1／60.533 | AttemptFacts FAIL10.01s | 9／80 |
| author new-attempt02／input04 | 0／71.448 | AttemptFacts PASS16.65s | 9／74 |
| author old01／input04 | 0／61.503 | 原管理员HTTP4.49s／邀请邮件HTTP4.74s PASS | 7／70 |
| independent independent01／input04 | 1／55.476 | A PASS3.05s；B FAIL5.02s | 9／75 |
| independent independent02／input04 | 0／56.339 | 仅 B PASS4.58s | 9／72 |

七轮 command 均记录实际父进程 wait完成；每轮 exact-ID 两次absent、原2容器／4网络基线不变、所属PID/starttime两扫空、monitor0/runtime空及源／运行时／私有输入前后门禁通过。adopted waits均为0，不虚构额外回收。历史PPID1 Z未触及，不是全宿主进程清零声明；全部资源窗口已释放。

独立 B 最终 raw SHA256 `fe9d354d0f0dc84fbde11d2ad3ce7650a5425d694d3051420fa4c1ee4903dcd1`，command `4271dd4f33dff079152065492e7d10db2646dd3dee54a281183d1967e7beac7c`，result `4ed3cf6420736e6948195e658a8ae1e54a464b331ea8c7e3532d54788e8ad929`，cleanup `59d61fc1d3bf70aee7bf64ade6338f9479975dd6320bc5fb5a9bf588935a729a`。独立01／02各自 original overlay和driver保留，运行时发现仅私有 overlay／TMPDIR不同，3501文件指纹与锁不变。

## 5. 查询规模与证据限制

作者真实 PG 同时核75ms父期限（实际约82.46／83.02ms，SQLSTATE57014）及原 DB 1s锁等待（约1.015／1.023s，55P03且caller仍live），既有 mapper 下 `errorIsDeadline=false`；不把 DB1s说成本地3s自然到期。独立受控 Store 的真实context／尾部证明与真实PG错误契约分列。

实际 EXPLAIN 使用三个正式 intent和原 SQL，normal／next／empty返回3／2／0行，执行0.108／0.082／0.032ms，原计划完整保存在 new-read01 raw。没有新增索引或迁移；现有 intent历史排序没有伪称已有复合页索引，materialized page只限制后续关联量，小表计划不证明规模SLA。纯扫描覆盖的旧 processing/unknown/null例外不冒充本轮 worker生成过的状态。

本结果没有浏览器／Node、SMTP UI、外部邮箱、生产 SPA或Runtime验收，也不新增写资格、发送／补偿或lookup。完整D07/D27及D08–D28/E01未完成，E01未开始；Summary待决、停止的Object/tools、Artifact/Project及生产未绑定、ready503边界保持。

## 6. 不可变档案与离线复核

[证据说明](system-mail-job-management-reads-verification-evidence/README.md)和[索引](system-mail-job-management-reads-verification-evidence/index.json)保存529逻辑原件／237去重对象（12,562,356字节）、38项原检查含七轮真实执行。原raw/diff不作格式清理；作者四版源、早期编译失败源、独立纯／真实各版probe／overlay、静审修订链、命令及实际终局均有明确逻辑位置。固定935基线用Git重建，3501运行时只保留原指纹／门禁，不复制完整树、依赖、dist、缓存或二进制。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-mail-job-management-reads-verification-evidence/verify_archive.py
```

本次实际离线PASS：全部对象、接受Git八源、935固定依赖、3501运行时指纹及38原检查／七轮退出与双清一致，旧OpenAPI schema和卡技术原字节保持。脚本只读保存原件与固定Git，不访问原scratch、不执行原件代码／产品测试或启动资源；档案自身检查不增加业务通过项。
