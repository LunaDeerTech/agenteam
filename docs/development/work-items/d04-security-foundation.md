# D04 Secret、出站访问与 Audit 基础

- 修订：1；状态：B01实施中；唯一活动模块D04；基线 `main@9beaa7f`，已推送origin/main，开工工作区干净。
- 前置：[D03](d03-postgresql-foundation.md) B01/B02真实数据库及进程独立验收完成；[D01契约](d01-contracts/README.md)已固定。
- 目标：按[计划D04](../development-plan.md#d04-secret-出站与-audit)依次完成Secret envelope encryption/版本化环境密钥环/可恢复数据密钥重保护、数据库权威动态出站策略与受控HTTP、append-oriented Audit写入及分页查询基础。

## 任务与所有权

| 卡 | 依赖 | 角色 | 独占范围 | 状态 |
| --- | --- | --- | --- | --- |
| S01 实施规格 | D03完成 | architecture_worker | 新增 `d04-security-design.md`；现有代码/根规格只读 | 已确认 |
| B01 Audit/签名cursor与正式授权端口 | S01确认 | backend_worker | 实施规格§1与本卡补充范围 | 进行中 |
| B02 Secret与密钥轮换 | B01及S01确认 | backend_worker | 具体加密/lease/轮换/测试范围由S01固定 | 待开始 |
| B03 动态出站与入口整合 | 前置已验收小块 | backend_worker | 具体策略/受控网络/入口与完整验收范围由S01固定 | 待开始 |
| V01–V03 独立验证 | 对应冻结范围 | verification_worker | 只读实现，独立临时探针与隔离资源 | 待开始 |

任务拆分可由S01按完整结果的真实依赖调整，不机械分函数；安全日志/Audit写入需要避免以stub解决循环。root独占本规格、台账和开发计划；作者/验证者只在冻结的明确范围交接。遵守AGENTS、团队流程和对应design/go-development/verification技能，子agent不得再委派或Git写操作。

## 已授权边界与规格要求

读取正式[部署密钥](../../architecture/platform-infrastructure/deployment-runtime.md)、[出站策略](../../architecture/platform-infrastructure/outbound-network-policy.md)、[Audit](../../architecture/security-governance/audit.md)及D01对应Secret lease/Audit/权限/事务契约。既定产品规则不重问；工程编码与实现细节由主线程确认，真有产品歧义报告来源及影响。

Secret采用标准库AES-256-GCM envelope encryption，版本化主密钥只从环境注入；DB仅存受保护密钥与非敏感恢复元数据。明确AAD绑定、nonce生命周期、格式验证、密钥引用/轮换并发、旧版本缺失启动拒绝、分批重保护与中断恢复、lease安全生命周期和日志/DTO边界。自动迁移不能生成或回写部署主密钥。

出站DB策略为权威，保存后新请求/重试/每跳redirect立即用新规则，在途继续；连接复用不得绕过检查。明确URL/DNS全结果分类与pinning、固定禁止网段/metadata、private CIDR/端口/显式HTTP、TLS验证、凭据origin、响应/时间上限及取消。DB基础连接是可信部署通道，不套运行时出站策略；SMTP非HTTP协议适配留D07但本次正式安全端口需明确。真实网络测试只用任务专属隔离服务/网络，不能放宽生产loopback禁令来跑成功测试。

Audit只保存安全结构化摘要/ID，不复制ToolOperation/Attempt/Transcript或Secret；append-oriented不允许普通修改/删除，无自动过期。Project清理经正式端口，System记录不能复制项目正文绕过删除。明确typed event/action/metadata白名单、同事务写入、unknown、cursor签名/过滤范围和稳定排序。身份/系统授权D07、Owner查询授权D08才绑定，当前不开放无鉴权生产业务端点或以admin代Owner；测试替身只验证正式授权端口，无生产stub。

复用D03 Tx/锁/迁移/隔离PG17.8/vector0.8.1与精确Go1.27.1/local，新增迁移全局顺序明确唯一作者，不改已验收B01库除非先报告具体缺陷。优先标准库，新增依赖须有明确必要性/精确版本。补Central密钥mandatory初始化与健康/关闭，保留其余未来依赖unbound，不能伪报产品ready。

验收须实际覆盖密钥无效/解密篡改/轮换并发和中断恢复、敏感输出、出站策略并发更新/复用连接/DNS rebinding/redirect/TLS/凭据边界、Audit事务回滚与分页权限。每小块冻结后独立验证、小块提交推送；D04完整通过后才D05。当前无已知用户待定或环境阻塞，未开始产品实现。

## S01 依赖调整

主线程确认按真实依赖将实施顺序改为Audit/签名cursor及正式授权端口→Secret/轮换→动态出站/入口整合，使Secret和策略修改直接使用已验收的同事务Audit，避免stub或跨卡未绑定写入。Audit先提供服务/存储；身份D07、OwnerD08未真实绑定前不装业务HTTP。最小identity contract类型按D01消费需要建立，不能默认授予身份或替代后续授权实现。此为工程顺序调整，不改变D04范围与整体完成门槛。

## S01 静态审查

175行实施规格初稿已冻结，SHA-256 `bab770e7efaca71078866e1f3ef1621ed726941e5a958ed383e29cb8755906b2`；作者13本地链接/fragment和格式检查通过，19来源manifest `d0a7ce07aeb1a9074182ee1c85453488a2e7a37b09d6eceffa8996f8463de159`。独立静态审查与主线程走查采纳4处明确化：Audit去重须排除首次生成ID/时间与传输Session/trace并提供安全按键核实；密钥迁移失败明示组件unavailable/ready503，不能以degraded跳过启动门禁；固定Go Transport已发送GET也可能内部重试，必须跨借连接保留sent状态阻止二次发送并以服务端计数验证；旧策略命令重放不得发布旧规则覆盖新镜像。

独立审查已停止，无其他确定的nonce/epoch/lease/锁层序阻塞；仅解冻设计作者的实施规格集中修正，不开始实现。该静态结论不证明任何D04能力已运行。

## S01 确认

实施规格修订2已集中修正4项，主线程逐项复核：Audit首次生成ID/时间、稳定摘要/授权LookupAppend；运行中Secret失败明确unavailable/ready503且不放宽启动门禁；HTTP发送状态跨Transport重试保留，sent后不再发送；旧策略receipt不能回退镜像。独立初审无其他确定阻塞，修正文档仍只属静态设计。主线程确认其余数据/接口/权限/锁/nonce/轮换与真实验收方案，下一步B01实现Audit与cursor，不提前实现B02/B03。按已授权小块提交推送设计基线后开工。

## B01 开工

S01设计提交 `e34dfaf` 已成功推送origin/main（9beaa7f→e34dfaf），开工工作区干净；实施规格修订2最终179行SHA-256 `869f1fd0179dea0ecf2fe2099a47088af17e3fc90cd6514de121e171d1d55b98`。主线程4文档117链接/格式检查通过，设计只属静态证据。

backend_worker接管实施规格§1 B01新增及共享范围；补授权 `deploy/central.env.example`、新增 `docs/development/backend/audit.md`、`scripts/test-postgres.sh` 的必要suite接入。root独占本卡/实施规格/台账/计划。B01按§4完成真实Audit/cursor与必要配置诊断，不提前Secret/出站；既有postgres/foundation生产与00001冻结。全局新增00002若影响旧测试目标版本，先报告精确必要测试适配范围。无新运行依赖，沿既定Go/PG隔离机制，其他权限/业务端口未绑定，不能生产stub或未授权HTTP。

B01追加明确所有权：`cmd/agenteam/main.go`、`main_test.go` 的scope=d04/help/version与mandatory配置适配；`tests/database/migration_test.go`、`recovery_test.go`、`migration_cancel_test.go` 仅为追加正式00002所需，fixture复制完整embedded并在Target()+1追加测试迁移，替换固定2/Manifest()[1]及对应目标/洞断言，保留原恢复/历史/取消覆盖。生产postgres/foundation、00001、embed.go不变；backend_worker独占以上测试适配。

## V01a identity/cursor分段审查

作者冻结identity/cursor共6文件，manifest `/tmp/agenteam-d04-b01-identity-cursor.sha256` SHA-256 `10cee382712e8e31597ea00b2db4da53815fe697cd61c6b5196efc4e97c20b52`。依赖仅未变foundation/标准库；作者局部race（cursor1.058s/identity1.015s）与vet通过，使用独立Python生成的完整签名向量。Audit其余范围继续作者独占写入，不能将此子集当B01完成。

主线程与独立验证确认一项编码契约修正：order_generation虽Go语义类型int64，作为排序修订跨JSON仍需D01正十进制字符串，当前wire为number。不是Go内部精度或授权绕过问题；须改wire并增加超过2^53/MaxInt64向量及number/前导零/负数拒绝。等待独立探针结束后集中移交，当前冻结范围未解冻。

V01a独立探针已结束停读，仅上述generation编码需返修：超过2^53及MaxInt64实际失败exit1；其余逐字节token篡改、跨scope、typed bounds、身份反序列化/非法Service、嵌套fmt/JSON/slog安全race通过（1.187s），6指纹末次未变。临时副本 `/tmp/agenteam-d04-v01a-ye7hiv3g`。已解冻cursor.go与对应tests给作者修复，其他4文件继续冻结，修后定向独立复验；无容器/Git操作，无B01完整验收结论。

## B01a 公共identity/cursor小块完成

原6文件重新冻结，manifest `/tmp/agenteam-d04-b01-identity-cursor-v2.sha256` SHA-256 `18234b86451a5f843643cc25cbf0486bbe21232b4e40e26021bf6778e4d77bdc`。只cursor.go/test变化，wire generation改规范正十进制字符串，Go int64接口不变；作者race1.059s/vet通过。独立V01a复用原失败probe与Python MAC重算，两组>2^53/MaxInt64向量及number/前导零/0/负数/溢出/null/+1/1.0拒绝全部通过，定向race1.050s、格式通过；其余4文件指纹未变，复用前轮安全边界结论。

验证者已停读/命令，作者停止本6文件写入，主线程已读全部实现并核对证据。将此完整类型与签名库作为B01a小块精确提交推送；不包含仍在写入的Audit，也不改变CLI/数据库启动配置。当前签名不代替授权，D07/D08仍未绑定；B01真实Audit存储/配置诊断与整体验收仍在实施，D04未完成。
