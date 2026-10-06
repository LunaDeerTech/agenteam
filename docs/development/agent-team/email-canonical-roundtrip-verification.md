# 邮箱 canonical 再输入与 SMTP wire 闭包修复验收

2026-10-06，[D07 修复卡 rev1](../work-items/d07-email-canonical-roundtrip.md)十路径经作者检查、独立风险验收及主线程采纳，提交推送 `f670cb1fe1f07ebd21bdb90a2b96cd565c33f05a`，主线程核远端一致。固定产品基线为 `819aba1b8f764328f1e2e67b53c274fad0db877d`；本报告归位已接受结果，不重新执行产品，也不代表 SMTP UI 或完整 D07/D27 接受。

## 1. 接受范围与固定输入

原 NormalizeEmail 成功分支及全小写输出保持，接受集合仅扩为原 `S ∪ lower(S)`：此前合法 IPv6 literal 的完整小写 canonical 可以再次输入，混合大小写非法 raw 不能先 lower 偷渡。SMTP worker 要求材料已是 canonical，仅在私有 wire 副本恢复 `IPv6:` 标记，生成同一地址对用于 MAIL/RCPT/From/To；普通地址、原 prefixed IPv4/mapped 文本、显示名编码、64KiB 消息上限、发送许可、TLS 和实际 join 保持。数据库身份、索引、命令 MAC、历史回执和公共 API/schema 未改，无新迁移、锁或写服务。

九候选为两生产源、两纯测试、三集成测试和两 SMTP fixture；第十路径为[后端 Account 说明](../backend/account.md)。精确路径和 SHA 以[索引](email-canonical-roundtrip-verification-evidence/index.json)的 `delivery` 及原 `author/author-delivery10.json` 为准，离线脚本逐项比对提交 `f670cb1`，不以活动工作树作产品输入。九源 `input01/manifest.json` SHA `e2882f0a9c6458127d09034309540ab04b7c7257f6d92d931c1d832a5ae1c26a`，六源 core 阶段与最终相关字节相同；末件 `author-delivery10.json` SHA `ef402fa05647af78ba7795ace5025ed85b7161bbe75ba8c9be127e8e05e151ec`，Account 说明 SHA `a3e6da5c99d5ba1564c9df88d88a907c8e53251015d9523dda9eacdeedc0ea22`。

阶段固定 Git 闭包为621件、随后886件；实际资源输入在886件中覆盖五个旧文件，留下881件只读依赖。作者冻结3501个运行时/工具文件指纹，独立增加私有 overlay 后为3512件、14个私有绑定；各轮均核前后不变。保留原锁、Go1.27.1、固定 MinIO、模块/工具及执行环境清单，不复制完整仓库、依赖、缓存或二进制。上述运行时指纹是当时门禁证据，不等于当前主机再验。

## 2. 作者原红与分阶段检查

原命令、cwd、环境、raw、实际退出和对应源在索引 `author/runs/<name>/`；[作者最终报告](email-canonical-roundtrip-verification-evidence/objects/98ac44617f52d4047ee20ccf1cb568719a83530acb68b4dbc32f986150008427)与[离线报告](email-canonical-roundtrip-verification-evidence/objects/3c3a03331324e742435bb9fed88df06fa8a66c51a3890833c8db94930aa23bb6)保留当时“待独立/第十文档”的原文，后续接受由本报告和固定提交绑定。

| 原检查 | 实际 exit / 秒 | 结论和复用边界 |
| --- | --- | --- |
| old01 | 1 / 32.183 | 原生产的 NormalizeEmail canonical 再输入、SMTP message 准备两个真实行为 RED。 |
| new-minimal01 | 0 / 7.361 | 同两 probe 函数精确原字节通过；保留 old01 源，不补造输入。 |
| pure01 → pure02 | 1 / 18.539 → 0 / 13.995 | 首轮 Account 缺七个固定图片/OpenAPI 资产，Accountmail 已通过；恢复资产后完整两个受影响包通过，未弱化断言。 |
| race01 / vet01 / build01 | 0 / 101.447；0 / 14.039；0 / 16.246 | 同两个完整包 race、相关 vet、Central/Runner 编译；cold race 编译计入耗时，未启动服务。 |
| integration-compile01 / integration-vet01 | 0 / 10.718；0 / 4.215 | 前者位于两个测试 SQL 类型比较修正之前，精确旧测试源保留；后者对应最终九源。 |
| runtime-discovery01 | 0 / 24.055 | 最终九源按正式脚本包列表完成 race 编译，发现三新顶层和原三模式顶层；不是运行这些场景。 |
| fixture-build01 / driver-gate01 | 0 / 1.124；0 / 1.441 | 五个 helper 编译、固定输入只读门禁；没有启动 fixture。 |

独立 core 两代表普通纯测试实际0/32.680s，覆盖原函数 `S` oracle、9个 golden/70个边界及完整 envelope/header/message golden、非法材料拒绝；不是 race 或正式发送。依赖 graph 门禁实际0/0.313s、357个模块内依赖。原函数比对首次把后续注释切入的静态提取错误保留，随后按函数列首闭合括号重新核定；它不是产品 RED。独立 harness 为静审，正式资源结果另列如下。

## 3. 五轮真实结果与独立组合

所有真实轮使用正式脚本、race/count1/package6m 与原资源所有权规则。作者 facade/wire 和独立私有场景2m、HTTP helper 每请求20s分别记录；没有把 HTTP 包装成新聚合2m期限。原始命令及实际 wait 在各轮 `command.json`，顶层和子例 raw 均保留。

| 原轮 | actual exit / 秒 | 实际到达结果 | exact ID / 所属 PID |
| --- | --- | --- | --- |
| author/new-account01 | 0 / 71.344 | Account service PASS5.40s、HTTP PASS3.35s | 7 / 73 |
| author/new-wire01 | 0 / 67.151 | 新 wire 顶层 PASS15.60s，四地址子例均通过 | 9 / 78 |
| author/old-smtp01 | 0 / 57.460 | 原 none/STARTTLS/TLS 真实 socket 顶层 PASS8.71s | 9 / 75 |
| independent/independent01 | 1 / 123.034 | 两顶层均 FAIL；两个子例完整 PASS，两个受前提/磁盘错误阻断 | 9 / 120 |
| independent/independent02 | 0 / 72.577 | 仅复验两个受影响子例并通过 | 9 / 74 |

作者 service 实际覆盖 raw/canonical TestSMTP 二次规范化、原 key/MAC/job/intent/event/Audit 唯一事实、邀请兑换、登录/lookup、实际 challenge 消费和 reset 公开形状/语义重放。HTTP 覆盖首次 raw 保存、canonical GET→只改 host→新 PUT、原首 key/body 的历史 Settings 与当前第二版分离、非法输入和当前授权/CSRF拒绝。wire 四例覆盖 IPv6、prefixed IPv4、mapped 文本及普通域名，完整 MAIL/RCPT 行与完整已知消息按独立预期字节比较，实际连接关闭、sent/terminal/io_joined、credential lease释放和registry join均到达；日志只保留匹配布尔/计数，fixture 不输出原地址或材料哈希。

[独立最终报告](email-canonical-roundtrip-verification-evidence/objects/9cee70e035a2bdcc59d01e3e0f69d1a7d6d5d784b7feebd7daeab83d98310c74)按以下四子例组合接受，未宣称最终整轮四例重跑：

| 子例 | 通过原轮 / 秒 | 原失败或复用依据 |
| --- | --- | --- |
| formal-test-intent | independent01 / 2.80 | 完整原 key/MAC/command/Audit/intent/event/job 验证通过，相关源码字节未变，直接复用。 |
| prefixed-ipv4-and-mapped | independent01 / 2.08 | 完整报文、canonical 存储、关闭/terminal/join/lease通过；wire probe 原字节未变。 |
| http-original-history | independent02 / 2.27 | 首轮最后 guest.bootstrap 私有 helper 强求 backend_log，与当前 smtp 配置冲突；仅改为真实 GET 200＋非空匿名CSRF＋smtp，原401/403和尾部断言保留后完整通过。 |
| expanded-ipv6 | independent02 / 2.23 | 首轮 TempDir ENOSPC 在本子例发送前失败；释放可重建空间后，原 wire 断言完整通过。 |

原 independent01 的 http 子例已到历史重放/current/MAC/audit及非法输入，却没有到匿名401、错误CSRF403与末尾核验；raw没有 bootstrap DTO，不能把源码归因追填成现场字段。原两顶层 FAIL 和exit1不改。independent02只换私有 bootstrap 调用和精确两个子例选择，未修改候选产品或削减否例；最终两个新PASS和两个原PASS共同覆盖所选风险。

五轮均 actual wait 完成，逐nonce exact ID两扫 absent，观测PID/starttime在两次清理均无所属残留；原2容器/4网络逐项不变，monitor0、runtime空、adopted wait均为0。历史PPID1 Z在原result中单列，不归本轮所有权、不称已回收。最终独立两次清理为16:56:09.413060及16:56:10.218804 UTC，窗口已释放。

## 4. 准备失败、原件缺口与边界

- 作者旧真实回归首次误用已存在的 `old01` 名称，`run.mkdir` 实际exit1，未覆盖原纯测、未启动资源；改用 `old-smtp01`，原 `old-start01-tool-record.json`保留。
- 独立首次误传 `--run`，argparse实际exit2，无run目录/资源、未记录PID；独立原工具记录和准备报告勘误保留。第二轮之前主线程只清六个已归档、非活动的可重建GOCACHE，恢复约9.3GiB；没有删除源、原件或活动缓存，不以空间恢复替代行为验收。
- 第十文档检查器首次把 `git diff --no-index --check` 的exit1当作格式错误，实际无诊断且候选未改；修正检查前提后完成10个既有链接/fragment及1个当时待落报告链接检查，原记录保留。本次归位后该报告链接存在。
- 被审候选规格全文SHA `c48e3a5e71b9f454cdf32786d9e864480bf9fa91ea7128d565022ac711e71c63`没有精确全文副本：复制前页首已获授权变化，首次full-SHA断言失败且未写文件。只保留当时报告、技术§1–7和接受页首全文 `73c7d5fb8b1a503cb7c27929e717397228973833564d3de5fc895db8a5ed81f4`，后者可由规格提交 `7e2a7d6`恢复；不反推缺失旧页首。当前技术SHA仍为 `f62bf7a195d9bf501d33cebf48e43680e5f58db112d00b7dc417583f757aaf15`。
- 历史命令回执由候选创建，未做跨二进制升级实验。旧TestSMTP失败可能已持久planned command；旧worker也可能已连接/EHLO/TLS/AUTH，不称零写或零网络。受控SMTP收件不代表外部邮箱送达；未重跑完整Account/SMTP矩阵、浏览器、SMTP UI、生产SPA或Runtime。

本结果不重试Object/tools原停止任务，不变更Summary待决及Artifact/Project等未绑定端口；SMTP UI、出站HTTP卡及完整D07/D27仍各自待验，D08–D28/E01未完成、E01未开始，生产`ready=false`、`/readyz`503保持。

## 5. 持久原件与离线复核

[证据说明](email-canonical-roundtrip-verification-evidence/README.md)和[index.json](email-canonical-roundtrip-verification-evidence/index.json)映射394个逻辑原件到194个SHA对象与9个Git复用引用，去重对象共9,661,988字节。原绝对路径只作来源标识；复核不依赖scratch，不执行其中脚本、SQL或测试。原raw/diff/历史报告逐字节保存，原先待验状态不被后写覆盖。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 GIT_OPTIONAL_LOCKS=0 python3 docs/development/agent-team/email-canonical-roundtrip-verification-evidence/verify_archive.py
```

本次实际离线PASS：原件SHA/字节数、十交付Git路径、阶段闭包/881依赖、逐轮冻结运行指纹、23个原离线检查及五轮真实退出/输入不变/双清理、四子例组合和规格技术原字节均核对。Markdown链接/fragment、UTF-8/LF/尾换行及限定差量检查另行完成；原日志/补丁对象不做格式清理。文档归档检查不产生新的产品PASS。
