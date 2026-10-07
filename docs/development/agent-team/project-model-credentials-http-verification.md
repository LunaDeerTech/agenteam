# Project Model credentials HTTP 完整限定验收

**完整23路径已独立 PASS 并由 root 接受**，产品 `e4b1b89197da0e9027018fdb41f6ab3e04050b9d` 已推送且 root 核远端一致。23范围中实际修改22项（21技术＋README）；`internal/central/app/project_usage_test.go` 原字节。正式行为见[rev2工作卡](../work-items/d09-project-model-credentials-http.md)，[完整末件结论](project-model-credentials-http-verification-evidence/originals/independent/final23/review.md)承接[技术结论](project-model-credentials-http-verification-evidence/originals/independent/final22/review.md)。源码复用此产品提交，不另复制产品树。

接受范围是当前 Human Owner 的 Project Model Credential Create/Rotate/Delete、安全 metadata GET/HEAD、被动 command lookup 和默认根绑定。当前 Read/Mutate 分开；查证沿原事务三把 SH 锁，历史 receipt 不绕过唯一原 Execute；完整材料校验、安全投影、400 KiB/65536 B 和1 KiB边界、30秒写与2秒读/lookup发布 I/O 预算及实际收尾有证据。发布预算不承诺 handler 硬返回期限，SecretMaterial destroy/byte clearing 不证明 Go 字符串擦除。

## 验收组合与原失败

有效组合为 production02＋unit02＋candidate03 的最小测试修订。作者普通/race/vet、自然30/2秒预算、schema及真实根均沿原版本索引；独立 controlled 普通/race 各1 top/18 nested。作者 PG 有效新5 top/旧14 top，含既有13旧组及已接受 Model Owner read 根；没有宣称所有结果都来自最终版本全量重跑。编译/list/输入门禁不冒真实运行。

| 原记录 | 修订与结论 |
| --- | --- |
| CRD-PROD-01：malformed nil-error Metadata 被 late receipt 掩盖 | production02 立即拒绝；这是 STATIC 反例，不伪称原轮动态复现。 |
| CRD-UNIT-02：失败 cleanup 只 release、没有实际 join | unit02 增加独立 done channel 与 cancel/release/wait；原 STATIC finding 保留。 |
| unused import、integration accessor/field/import 两类 compile 首红 | 原 command/raw 与 candidate02 适配差量保留；不称产品运行失败。 |
| input gate、错误 binary prefix、plan hash/metadata-object 比较首错 | 原前置失败与修正保留；未启动 Go/资源的步骤不冒 runtime。 |
| 作者 new2 final 检查点失败 | 原 raw 没有 SQLSTATE；candidate03 将 UUID/text SQL 参数分开并加安全诊断。静态归因与真实 rerun PASS 分列。 |
| 独立 A01 实际500/InternalError/not_committed | 私有探针错误嵌套事务污染父事务；只修第二事务独立两秒 context/断言顺序，A02 原503、拒绝与原子回滚条件通过；未到达断言保留。 |

独立 A02 核当前 Session、归档 Read/Mutate、三 SH 锁、Command EX 阻塞不误判缺失、Logout 与真实 Audit 拒绝/事务回滚。独立 B01 核默认 app.Run、65536字节材料重放、最后一字节冲突、HEAD长度和查证；唯一目标后端先 COMMIT＋idle，再丢 ACK，使 HTTP 返回 Unknown，后续单独公开 lookup 确认。作者 held-pending→放行→终态为另一时序。早期作者索引将后者预分给独立 A 的历史文字不改，实际归属以 final22 的 B 为准。

[四份独立原响应 schema 原件](project-model-credentials-http-verification-evidence/originals/independent/schema01/manifest.json)绑定104/88/131/131字节原 body、method/path/status/headers/candidate/run；标准 Draft202012Validator/FormatChecker 直接验证同一原字节。[实际命令结果](project-model-credentials-http-verification-evidence/originals/independent/schema01/runs/original-bodies01/result.json)为exit0、actual wait及双清；作者真实响应/schema沿各自PG原件，未以重新编码样本替代。

## 实际资源与边界

作者3轮 native 共3 top/13 sub/14 listener；direct3＋adopted3均实际 wait，所有 TCP（含 TIME_WAIT）与 owned PID 双空。9轮实际 PG 是作者6（含 new2 FAIL）＋独立 A01 FAIL/A02 PASS/B01 PASS。每轮7资源 actual wait/双清，按原 cleanup ID 实表去重得到63个不同ID，非仅用乘法推定；输入均一致。[逐轮原集合](project-model-credentials-http-verification-evidence/originals/independent/final22/pg-rounds-and-daemons.json)保留实际248→284及新增36个不同PID/starttime的daemon/PID1 shim，它们非owned、未task-wait，不称全机清零。资源窗口已结束。

forced root 返回不证明全部 inner join，代理/进程/资源终局另列。Project.Create 沿既有持久化测试 Skills fixture，不代表生产生命周期或初始化已绑定。Project配置写入/UI、production Resolution/Invocations、真实Provider调用和D24不在本次接受范围。系统管理员统一会议Summary initial/update（含首轮标题），Project不override/复制初值；compaction/Execution Summary不改。ready503、D08–D28/E01未完、E01未开始及Object runtime join/OpenAI tools独立验证/SPA concurrent-publication三停止保持。

## 保存范围与归档自查

[来源映射](project-model-credentials-http-verification-evidence/source-map.json)逐项绑定241个去重原件、173个同字节别名、171份元数据的5个明确派生束、27项仅指纹来源。重复大图/input map、完整初始源码差量和README全文复用固定提交/原索引，不复制树、binary、cache或私密runtime；不声称整个原依赖闭包已入仓。原始文档里的scratch链接保留其原位置语义，通过source-map定位已存副本。

逐件核来源/副本SHA与原字节、JSON/脚本AST及新文档链接；卡§1–8与四入口旧历史字节不变。原件14文件有367条尾空白/space-before-tab/空EOF扫描记录，另35份原件无末换行，逐行位置见source-map；均不改字节、不加ignore。这是归档字节扫描，Git差量检查由root执行。本任务未运行Go、业务、资源或证据脚本。
