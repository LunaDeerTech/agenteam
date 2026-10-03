# D05 对象存储与 Artifact

- 修订：1；状态：S01规格中；唯一活动模块D05；基线 `main@abf5c37`，开工工作区干净。
- 前置：[D04](d04-security-foundation.md)全部独立验收和真实完整suite通过，入口本地提交abf5c37；GitHub认证失效，ee8ddb7起5个本地提交待恢复后补推，不冒称远端同步。
- 目标：按[计划D05](../development-plan.md#d05-对象存储与-artifact)实现StoredObject/引用与lease、流式MinIO读写、跨DB/对象存储的一致性和恢复、Artifact服务、受控预览/下载及短期传输授权。

## 任务与所有权

| 卡 | 依赖 | 角色 | 独占范围 | 状态 |
| --- | --- | --- | --- | --- |
| S01 工程规格 | D04完成 | architecture_worker | 新增d05-object-storage-design.md；已验收代码/契约只读 | 进行中 |
| R01 依赖与隔离环境核验 | D04完成 | research_worker | 有界依赖/镜像资料及任务owned临时探针，不写仓库源码 | 待开始 |
| B01–B03 实现分块 | S01确认 | backend_worker | 规格确认后另列完整结果/迁移/共享文件所有权 | 未开始 |
| V01–V03 独立验证 | 对应冻结范围 | verification_worker | 停写实现的独立副本及任务owned资源 | 未开始 |

root独占本卡、开发计划和任务台账；架构作者仅新建实施规格。角色遵循AGENTS与agenteam-design/go-development/verification/documentation技能，禁止子agent再委派和Git写操作。常态至多两个活动子任务，Docker和测试资源顺序明确移交。设计稳定且root确认后才写实现，不提前D06+。

## 已确认规则与设计任务

读取[对象存储](../../architecture/platform-infrastructure/object-storage.md)、[Artifact](../../architecture/tool-system/artifact-tools.md)、[D01资源契约](d01-contracts/resources-skills.md)、[生命周期](d01-contracts/domain-lifecycle.md)及[基础事务/幂等](d01-contracts/foundation.md)。此前已确认决定不重复询问。

StoredObject只拥有payload存储事实，Attachment/Avatar/Knowledge/Skill/MCP/Execution/Meeting/Artifact保留各自业务身份。显式owner reference和active lease必须真实保护删除；不能仅凭object_id授权。业务模块不能访问MinIO SDK/bucket/key，Agent不取得签名URL、存储凭据或无业务引用对象枚举。Artifact from source首版沿既有复制行为，D01引用保护不是擅自改为共享payload的许可。

上传先持久pending，外部stream写入/校验在DB Tx外，只有payload与metadata可信后发布available/reference；中断、payload已写但事务失败、COMMIT unknown、缺失payload、引用/删除竞争、崩溃恢复均落实为可观察领域事实，不造万能outcome表。长度/大小/digest/媒体类型、range、取消和内存边界须有明确平台上限。未引用对象清理为显式可恢复流程，不能因TTL猜测无活跃传输或业务引用。

明确短期Signed URL和TransferGrant的单对象/单操作/runner/完整性绑定、过期/重放/撤销实际可实现边界及安全投影。Runner直连部署声明的可达受保护端点，不可达明确失败，不暗中Central中转；D17绑定真实Runner通道，D07/D08绑定身份/Owner，D21绑定Artifact Tools。当前正式授权端口未绑定应拒绝，不开放匿名业务HTTP或成功stub。

复用已验收Go1.27.1/local、D03 Store/Tx/全局锁序、D04identity/Audit/cursor/Secret/安全配置与生命周期。设计须核对真实代码端口，新增Audit action/metadata等公共契约若必要，先列明确改动与所有权，不直接修改冻结D04。迁移只追加00005起全局序列，禁止改00001–00004。MinIO SDK/镜像/协议选择须固定精确版本与可验证来源；存储是可信部署通道，区分运行时受控出站规则，不引入业务任意endpoint开关。

S01需明确表与约束、Go接口/安全闭包、状态机、授权和Project gate、幂等/unknown/锁序、引用与lease、上传及远程直传完成校验、签名授权、Artifact复制/列表/读取/preview服务、清理/重启恢复、配置启动健康关闭和真实fixture验收。范围大的内部步骤按完整结果分块，核心能力不得留后续模块补齐。

## 环境与验收

Linux/amd64、Docker28.4.0、精确Go1.27.1/local、既有PG17.8/vector0.8.1正例与PG16.12拒绝fixture继续复用。MinIO版本/镜像及SDK仍待R01核验；当前未创建对象存储资源，不访问现有服务或真实凭据。

验收至少实际覆盖中断上传、外部已写DB失败/unknown、缺payload明确错误、引用/lease阻止删除、跨scope拒绝、stream不全量缓冲、签名材料安全及有效期/完整性、恢复/清理幂等与Central资源关闭。只用nonce/标签/exact ID任务owned PG/MinIO/临时目录，缺真实fixture在专用套件中失败，不能skip充当通过。

下一步S01+R01，冻结后独立静态审查并由root采纳；当前无新产品待定。D05及D06–D28/E01均未完成。
