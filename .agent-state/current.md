# 当前检查点

- 目标：D10 Secret Variable Owner 最小完整后端 SPEC；分支 `ai/secret-variables-owner`，正式基线 main `3cea6076bb01693ead2755826826d189626aa3aa`。
- 当前获 root 授权 A 纯合同结果：本卡/current、新 `projectvariable/contract/secret_{types,commands,query,events,directory}.go` 及其测试、独立 `api/openapi/secret-variables.json`；负责人 `/root/knowledge`，root 负责 Git。禁止修改旧普通 DTO/CommandName、通用 Purpose.Valid、D04 共享实现、生产根/SQL/迁移及台账；无 Secret 真实资源授予。
- 已落可恢复草案 rev2：共享 VariableID/名称目录、Owner HTTP/安全 Lookup/引用删除 gate保持；新增明确D04 dedicated purpose/typed intent与authority stages、新kind3加密回执及rotation/canary/CleanupProject覆盖、单final Tx的D10私有Outbox discovery witness/真实final事实和D04 native Audit variant。原请求semantic排除server派生映射/版本/随机候选，防后续更新删除破坏原重放。Agent资源侧端口及F1实际提供方已列清。
- main 的 D04 Lookup 仅 Model，Purpose 无 ProjectVariable；旧 purpose==consumer 不直接满足已定多用途 Secret。草案不借 Model/Runner 假目的、不把普通变量冒成 Secret。Agent F1 尚无真实 canonical owner，目录资源侧实现不能冒作白名单实际绑定。
- 源码依据：main普通Variable Authority的DiscoverAppend要求持久command.Plan；D04 rotation的旧receiptOwner只反查旧secret_command_receipts，CleanupProject也只清旧表；旧Audit私有witness不能授权新typed receipt。因此rev2显式新增这些责任，不能说现成API可直用。仅静态设计，无Secret产品编译/动态通过结论。
- rev2 SPEC 已获 Model 独立有限接受（ee4095），本轮仅实施 A 纯合同；D04/D10 Service/Project/Audit/root 共享路径和新迁移号仍须 root 分配；00028仍是root给Skills后继预留，本卡不占号。限额已作为本SPEC工程选择写明，F1真实canonical/ref及MCP/Runner材料能力仍分别待后继。
- A 首段已落五个新合同源及 secret_types_test.go：request/metadata/receipt/lookup/event、F1 directory/reference opaque plans。首次仅编译检查 56776/324761 因 Secret event 中重复声明旧 Change enum 失败，已移除重复声明但尚未重编；新测试也尚未执行，commands/directory 测试与 OpenAPI 未完成，不宣称当前编译通过或 Owner/D04 能力已实现。B02 Process 50756 已完整 PASS，B02 源码仍冻结供独验；D13 固定数据语义审另行报告具体争议。无本树后台进程/PG/socket/browser。
