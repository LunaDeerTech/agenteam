# System Model 默认生产根验收

**System Model 同步配置默认根已独立 PASS，主线程采纳并提交推送 `457b1979c9d6563740543b2011eedc06cce34c71`，远端同 SHA 已由主线程确认。** 交付 8 路径：3 生产、3 新测试、2 验收后的能力文档。真实 Central 现提供 System Provider/Model CRUD、平台 selector、Model credential 独立写入及两类原命令查证；完整 D09、Project 根、Model Invocation/Usage 和实际 Provider 调用未交付。

固定源码基线 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`，规格提交 `2eb1e633f604aa3078db156c5d5b1a65ab08c2c3`。作者为 `recovery_handoff`，独立验收为 `restore_test_dependencies`。[最终 8 路径清单](evidence/system-model-root-verification/author/final-freeze-03/manifest.json) SHA 为 `726c697245d540edc52f96f1d28323065405c82b7847b4af95e2f6b6852144ba`；六份源码保持 [candidate02](evidence/system-model-root-verification/author/candidate-freeze-02/manifest.json) 原字节，SHA `8eb080f9b51a62cbca9d6045f72f0a1789fbc0bc1b7a5dc0daec0e9cf27b8cab`。三生产始终未返修，独立[生产静审](evidence/system-model-root-verification/static/production-review01/review.md.txt)通过。[独立原报告](evidence/system-model-root-verification/verification/review.md.txt) SHA `e65c5036bf4ce3da683712b545ddcc9ce9ec75f3ee8af7e7c5f6e0971753a55a`，[交付索引](evidence/system-model-root-verification/verification/delivery-index.json) SHA `14c1227473a96c43179863169ae1cd23b28322b5ed778608f60ad3f25a4e575d`。原报告中“文档待更新”等语句保留当时事实；后续两能力文档已经纳入上述提交。

## 作者有效覆盖与原失败

| 实际组 | 原结果与最终有效范围 | driver 秒 |
| --- | --- | --- |
| new-init-01，candidate01 | 2 顶层/7 子例通过；仅 app 初始化 1/5 按未变语义复用，binary 初始化后来复验 | 55.902 |
| new-http-01，candidate01 | 3 顶层失败、force 顶层通过；整轮原红保留，不用其漏采结果证明 force 底层状态 | 38.006 |
| new-http-02，candidate02 | 完整 4 顶层/6 子例通过 | 38.772 |
| new-init-02，candidate02 | 受阶段断言修正影响的 binary 初始化 1 顶层/2 子例通过 | 39.087 |
| old-app-01，candidate02 | 原 Secret/SMTP/DB-last 3 顶层/4 子例通过 | 43.958 |
| old-binary-01，candidate02 | 原 Account/Secret/Outbox/信号退出 6 顶层/4 子例通过 | 52.020 |

共 **15 个不同顶层/21 个子例**：新组 6/13、旧组 9/8；candidate02 直接 14 顶层，另 1 顶层/5 子例明确复用。原 driver 保持 `-race -count=1 -timeout=6m`，未把 no-tests 包或 compile-only 算动态通过，也不称最终输入一次整组全绿。实际 argv/env/exit、逐组日志、输入和清零见[作者矩阵](evidence/system-model-root-verification/author/handoff/summary.json)及[原报告](evidence/system-model-root-verification/author/handoff/report.md.txt)。纯 app/process race 在 pure-02 通过（26.955s）；最终 integration/race compile（5.029s）、vet（0.295s）通过，两 cmd 已实际构建。纯检查和 build 只复用实际参与编译且未变的输入。

首个 pure-01 因选择性快照漏带原 Account embedded 资产而 setup 失败，补精确 baseline 资产后通过，原日志与恢复元数据保留。new-http-01 的错误 observer namespace `model` 使真实 `model.system` 事务漏采；两个 live stdout 读取与旧进程 helper 的 buffer 写入竞争；匿名响应依法清 Session Cookie 后，新测试共享 Cookie map 污染后续 CSRF 反例。独立静审另发现阶段字面量 `secret_maintaining` 无法命中真实 `secret_maintenance_starting`。四处仅修两份新测试，保留 receipt/canonical、精确权限、日志和预算强断言；[原归因](evidence/system-model-root-verification/author/failure-review-01/triage.md.txt)、[精确差量](evidence/system-model-root-verification/author/candidate-freeze-02/from-candidate01.patch)、原两测试和受影响复验均持久保存，生产未因此改动。

修后正常 drain 同时观察 HTTP 200 receipt、canonical Provider、实际 `[committed]` 与 DB-last。两个 force 分支实际记录 `[not_committed]`、`INTERNAL_ERROR`、unreturned=0、原取消及有界 ForceClose 发起。这些是命中事务返回的观察，不能由任一状态列表推导所有业务阶段、原 writer/锁终局或 Object 全局 join。

## 独立实际结果与探针修正

独立输入是 ac5 加仅三冻结生产源和自有 probe，没有叠加作者新测试。有效覆盖 **2 顶层/4 子例**：首轮初始化 top 的 release/cancel 两子例通过；修后定向根/当前身份 top 的两个子例通过（driver 33.990s）。初始化正文及 guard/helpers 不变，明确复用首轮 1/2；不是修后全套一次运行。两轮离线 race compile/vet、真实 selector/env/exit 见[首轮结果](evidence/system-model-root-verification/verification/fixture-runs/independent-01/result.json)和[修后结果](evidence/system-model-root-verification/verification/fixture-runs/independent-02/result.json)。

真实默认 binary 完成 bootstrap/login/session，创建两项 credential、Provider 创建/切换引用，核 Model/Secret receipts、完整 SQL 关系、typed Audit、Outbox events、零 Model delivery、唯一原 mail 订阅及 HTTP canonical/lookup。实际 logout 后，用同一原 issued Session 分别重放 update 和两类 lookup，均精确 `401/SESSION_REVOKED`，完整相关旧事实不变。初始化 probe 使用正式 SystemConfig 锁和精确 PG blocking 事实：Secret 已初始化、Model selection 尚未落库且无 maintenance/listen；持锁取消在 holder 仍活着时退出，release 分支只建立 technical empty selection 后监听。未增加网络故障代理。

independent-01 原 revoked 子例失败，原 raw 没有 actual code，不追认其动态错误码。固定 Go Client 按请求 `Host` 管理 CookieJar，而原 probe 按连接 IP 取样，漏取真实 issued Session；不是后一 401 清 Cookie 污染。经授权仅改有效 Host 取样、单一非空原 Session 和每次实际发送前验证、safe-code 诊断，`Jar=nil`、精确 `SESSION_REVOKED`、完整事实和预算断言不变。[诊断](evidence/system-model-root-verification/verification/probe-freeze-02/diagnosis.md.txt)、[原差量](evidence/system-model-root-verification/verification/probe-freeze-02/from-probe01.patch)、两版 probe 与首红完整保留。

## 输入、资源与归档范围

Go 1.27.1、离线 readonly modules、自有 workspace cache/TMPDIR、固定 PG17/PG16 digest 与 MinIO SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 沿原输入。作者 692 源码/资产末检精确，独立初始 840 个 baseline 文件加 Model helper/probe 的输入及末检记录保留。归档逐 SHA 核原作者 114 payload、最终冻结索引及独立 60 payload/7 外部定位；已提交最终 8 路径逐项等于冻结字节。重复源码从精确 Git 对象恢复，不复制完整仓库、cache、binary 或受限凭据日志。

作者六轮累计 25 容器/19 网络，包括 old-app 原 SMTP 的额外 1/1；560 个跟踪进程实例末检无残留。独立两轮各 4 容器/3 网络，分别跟踪 173/82 个进程。所有实际轮都保存存活 ID/name/nonce-label（含网络），随后两遍 exact-ID absent，原 2 容器/4 网络的 ID/name/labels 不变，owned 进程组和 runtime/gotmp 空；窗口已交回。资源结论仅描述原检查时点，本次归档未运行 Go/Docker/网络或 Git 写操作。

[持久入口](evidence/system-model-root-verification/README.md)、[原件映射](evidence/system-model-root-verification/source-map.json)和[全量指纹](evidence/system-model-root-verification/SHA256SUMS.json)覆盖原始字节、重建和检查结果；[空白例外](evidence/system-model-root-verification/whitespace-exceptions.json)仅保留实际检测到的原 patch/log 空白，不改字节消除历史警告。归档核验见[检查记录](evidence/system-model-root-verification/archive-checks.json)。

本结果只有同步 System 配置生产根。单一 Outbox mail handler、无 Model consumer、`ready=false`/503 保持；Project 根、Resolver/Invocation/Usage、实际 Provider/MCP 调用及 Summary 产品初值仍未交付。Object 原受阻修复保持停止，Artifact 16 源仍未提交，共享 guard/完整卡与领域绑定阻断未解除；本报告不宣称完整 D08/D09/D28/E01 完成。
