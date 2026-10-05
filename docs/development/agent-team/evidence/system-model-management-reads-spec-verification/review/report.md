# System Model 管理读口 rev1：独立规格静审

结论：**STATIC PASS，建议采纳规格**。未发现必须修订的规格阻断；13 路径在固定基线上具备完整可实现接缝。本结论不是实现、动态验收或后继 UI 通过。

固定业务为 `c54f73f3324caa11608d84e5d207141985eb6074`；卡为 `ddcb7225d809787f0987bf24ab13c5a3e6ea398d6f483c1f0de910c156da7d6b`，作者 47 Git 输入索引为 `8a44abd6b86134df2cb938387cd71bb462fa38b38d729b83e6a87d7ff6a8ae39`。47 对象均从该 commit 读取并逐字节/SHA 核合；另补读同 commit 的 Secret `contract/write_lookup.go`，端口签名确已存在。设计/Go 技能已读，复用 verification 流程；未消费活动 ledger、公开入口或 Artifact 源。

| 已核边界 | 依据与结论 |
| --- | --- |
| Secret 当前权限与兼容 | `secret/write.go:458`、`service.go:143`、`storage.go:74`；Account `authority.go:32`、Project `authority.go:64/108` 已接受同 Store 的真实 Tx。完整 User/Credential SH（Project 加 Project SH）后在同 Tx 授权、再查完整 scope/ref 可执行，既有 helper 不读 payload 密文。Human/current Session/scope 权限先于存在性，System 与 Project 依赖分别检查；不引入 Service Subject、Mutate、Audit 或 material 读取。nil/typed-nil 安全与验证顺序须由限定 Metadata/helper 实现，不要求扩大构造器。 |
| 旧调用者 | 固定 Git 搜索确认唯一生产 `.Metadata` 调用为 `model/http_credentials.go:164` 的写前置；该处不持 caller Tx，历史 receipt 命中仍跳过 Metadata，版本竞态仍由真正写入验证。旧 Project/Secret 真实调用也是独立调用，不形成新增嵌套 Tx。原签名与跨 scope 错误保持；直接 Metadata 的 3 秒上限是卡内明确变化。 |
| 完整锁与稳定读取 | PG `transaction.go:191/254/278` 核同 Store 活 Tx、实际 held locks，并一次排序/拒绝升级。Model 普通配置/selector 写的 refs SH、删除 EX（`configuration.go:25`）说明新读使用 refs EX 必要；selection SH 与现写方 EX 相容。不能照搬旧 `readScope` 的 refs SH。现 App 六根前缀已覆盖两个新增子路由，无 root 改动。 |
| 本域事实与有界聚合 | `references.go` 的 canonical `selectionReferences`、`store.go:267` 与 migration 00015 的七组 CHECK/反向索引支持平台 LIMIT 5、目标 LIMIT 10001 再聚合。10000 为精确上限，10001 整体拒绝；不以无界 COUNT 后 LIMIT 代替。外域只报告登记边数及未绑 blocker，不读外域私表、去重成 owner 数或授予替换能力。 |
| 预览与后继删除 | `configuration.go:293` 会在写锁内再次比较全部目标引用，平台 canonical 另核；Model.version 不能充当引用版本。卡要求读后改变事实再执行原删除，且保持原准备/执行权限；这关闭“预览即可执行”的错误解释，未绑定 adapter 保持拒绝。 |
| 结果/预算/安全投影 | 两新 HTTP 的 3 秒预算从 RequireSystem 前开始，库调用不延长更早 deadline；只在 Committed 且返回前 ctx 未取消时发布 DTO。Unknown 保留原 attempt/cause 的既有错误，不自动重读、不宣称 writer 终局；D03 poison/fault 优先和真实清理责任保持。safe DTO、七组/decimal string、HEAD、查询/body 拒绝与无材料日志规则已闭合。 |
| 路由、路径与真实门槛 | 固定 OpenAPI 实数为 22（含 5 HEAD），新增 2 GET+2 HEAD 为 26；旧 `http_test.go` 双向覆盖只需改总数。13 路径新旧分类及 11 本地链接均已核。既有 systemHTTPStore 按 cause/真实 callback 和最终事实定位，不依赖盲目第 N 次事务；新文件同包 helper 可完成五新真实顶层及指定十二旧回归，无需改旧 fixture。 |

实施后仍须用真实 Account/Secret/Project/Model/PG 验证当前 Session 撤销窗口、完整 union 的普通锁竞争、scope 兼容、10000/10001 与损坏 canonical、读后变更再删除、真实读取后 Unknown 装饰和提交后取消。静态不能证明运行预算或测试结果；装饰 Unknown 必须同时记录底层实际 Committed，不是网络/ROLLBACK 反例。原卡五新组及限定旧回归不可省略，独立增量按固定作者证据去重。

实际只执行固定 Git 读取/搜索、文件读取、Python 字节/JSON/链接/路径分类核对。机械 argv/exit 见 `read-commands.json`；核对结果见 `review-checks.json`，旧调用点原输出见 `metadata-callers.txt`。没有 Go/npm/browser/Docker/SQL/网络、共享写、Git 写或测试资源；未验证前端、完整 D09、生产调用/Usage、未决 Summary 或暂停的 Object 修复。全部私有产物冻结后停止写入。
