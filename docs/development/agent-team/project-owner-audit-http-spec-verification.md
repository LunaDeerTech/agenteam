# Project Owner Audit HTTP 规格验收归档

**rev2完整规格与正式归位均独立 STATIC PASS，root已采纳。** [正式卡](../work-items/d04-project-owner-audit-http.md)提交 `9eb167e4d7c7ae7ffb71030e587d8571fed3c4af` 已推送且root核远端一致。固定产品基线是凭据完整接受 `e4b1b891`；本次只接受规格，未授权 Audit 的14技术实施、2文档末件或Go/native/PG。实施仍须配置写入完整产品接受、root移交 `account.go` 并共同冻结实际输入；不将活动配置候选或一次测试通过冒为产品接受。

[前沿建议原件](project-owner-audit-http-spec-verification-evidence/originals/recommendation/recommendation.md)与77项固定来源支持此范围：当前Project Owner的Audit列表/详情GET与HEAD、同事务Session/Owner/锁与有界读取、安全typed DTO/schema、完整页/哨兵/游标、原物理Unknown cause/attempt、3s发布/I/O预算与实际尾部、默认根及兼容验收。具体契约仅以正式卡为准，不另建规则副本。

| 阶段 | 原结论与差量 |
| --- | --- |
| [rev1完整独审](project-owner-audit-http-spec-verification-evidence/originals/independent/rev1/review.md) | CHANGES_REQUIRED仅一项AUDIT-STATIC-01（B1）：`cmd/central`与`cmd/agent_server`在固定基线不存在。其他完整范围STATIC通过。 |
| [rev2差量/完整组合](project-owner-audit-http-spec-verification-evidence/originals/independent/rev2/review.md) | 改为`cmd/agenteam`与`cmd/agenteam-runner`，保留实际fixture/CGO变体及只build不启动边界；另两处仅状态/来源联动。原75来源不变，追加2个main来源；14技术＋2文档范围不变，B1关闭。 |
| [正式单卡末件](project-owner-audit-http-spec-verification-evidence/originals/independent/formal01/review.md) | 反向恢复五处行政/链接替换后等于已验rev2；77项表、81处链接/fragment及16项来源指纹通过。未接受产品或运行结果。 |

[budget-static脚本](project-owner-audit-http-spec-verification-evidence/originals/author/budget-static.py)及[原结果](project-owner-audit-http-spec-verification-evidence/originals/author/budget-static.json)保留：1,037,420 B是静态组合包络，低于1 MiB预算；它不是实际Go最大编码页、真实schema或资源验收。31动作正式构造极值、200条真实生产编码/第201哨兵、真实签名cursor与原响应标准解析仍待实施验证。本次只存档并做脚本AST解析，未重跑原脚本。

作者两次文档helper错误保留原说明和对应脚本版本：首次替换顺序断言在正式文件创建前失败；第二次误要求`git diff --no-index --check`差异退出值为0，实际exit1且无空白诊断，修为核0/1和空输出。独立末件最初将预算链接行号85误写、实际86的检查更正也在原checks中。均为文档处理历史，不是Go/产品失败。

[来源映射](project-owner-audit-http-spec-verification-evidence/source-map.json)保存33份原件及4项仅指纹定位。正式卡全文复用上述规格提交，按两份原始可逆diff已逐字节重建rev2、rev1并核同原SHA，不重复复制三个全文；77个固定快照逐项核SHA/字节/Git blob，未复制源码树、工具链或大图，也未读取活动配置源码。原文链接保持历史位置语义，由source-map定位副本。

归档自查核原件字节/指纹、JSON、脚本AST、当前新增链接及卡§1–8原字节。两份原diff有22条尾空白记录，逐行位置见source-map；已检查EOF空白分类，本批未见该类，保原字节、不ignore。该扫描不冒root的实际Git检查。本任务无Go、业务、资源、Git或原脚本执行。Summary统一管理员规则、生产Resolution/Invocations/D24等未绑定、ready503、E01未开始与三停止保持；本卡不表示D04/D08/D09整体完成。
