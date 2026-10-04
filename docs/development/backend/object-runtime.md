# 对象 Runtime 与 Runner transfer

本文说明 D05 当前组合与部署边界；正式接口、锁和验收规则以[对象实施规格修订 6](../work-items/d05-object-storage-design.md)为准。[后端入口](README.md)列出配置、命令和 fixture 来源，[Artifact 说明](artifact.md)说明浏览器下载。生产尚无对象/Artifact/下载/Runner transfer HTTP 路由，`object_authorization` 和 `runner_transfer_authorization` 保持 unbound。后续 D08/D15–D17 须绑定真实身份、Project、Runner 与 Operation 事实；不能用本说明中的测试适配器替代。

## 启动与存储身份

Central 必须配置预建私有 bucket、固定主 endpoint、显式凭据、独立下载 keyring 和 spool 绝对路径。`OBJECT_TRANSFER_ENDPOINT` 可选择同一存储的 Runner 可达 origin；它与主 endpoint 共用 bucket、region、凭据和 TLS 策略。没有环境代理、默认 AWS 凭据链、SDK 自动重试或动态业务 URL。可信部署通道可按配置使用私网/loopback；它与运行时出站 SSRF 分类是两种不同边界。

`NewRuntime(service, guard, transfers)` 只核绑定并构造。`Initialize` 在数据库阶段之后，与 Audit/Secret/出站初始化共用剩余 30s：检查 bucket 配置及 DB/control store identity，确认 ProcessGuard，再真实 PUT 32 个随机字节、经两个 origin 完整 GET 校验并 DELETE/核实 absence，然后执行恢复门禁。不同 identity、不同 probe 内容、配置查询失败或未收敛的未授权事实均拒绝；probe 从未完成不能算健康。初始化未确认时连 Stat/Discover 也拒绝，已准入的内部恢复沿原 context 工作。

每个 probe 先持久预留并确认提交；unknown 不靠 404 推测原 PUT 结束。恢复只处理该随机 key，未知写用永久空 marker，已确认并真正 join 的 probe 才可删除。probe checkpoint 不含业务字段。bucket 必须从未启用 versioning、没有 ObjectLock/lifecycle/bucket policy；配置权限失败不能视为 disabled。

当前生产组合没有业务 planner，只能在真实数据库无 object/upload/attempt/reference/lease/transfer/download 事实、spool 无未决正文的技术空态启动诊断。正式 `Service.Recover` 缺 planner 仍拒绝，不把缺授权包装成初始化成功。已有业务数据的部署须先绑定其正式领域端口，不能删除 checkpoint 来启动。

## ProcessGuard 与维护预算

spool 默认 `/var/lib/agenteam/object-spool`；其 sibling `.processes` 目录保存部署身份和每个随机 ProcessID 的技术 claim。目录 0700、常规单链接文件 0600，拒绝 symlink/owner/mode/inode 矛盾。claim 与 DB store/deployment/目录身份、实际 host/kernel boot ID 绑定，并持 lifetime flock；PID、TTL、心跳和网络失败都不是判死依据。

恢复旧本地实例须重读 exact DB/file claim，并实际取得旧 flock；同一可信 host 的历史 boot 记录可证明旧 kernel 实例终止，跨 host/不明身份不能。测试中的历史 boot 记录分支不是声称执行过主机 reboot。正常关闭先全部 I/O Close/join，再写 stopped 和释放 claim；Force 未 join 不释放，真实进程退出由 OS 释放锁。本地死亡从不释放外部 Runner lease。

Runtime 与旧 Service 共用唯一维护槽；任一已启动，另一入口都拒绝第二 worker。每 10s 处理各持久队列的公平有界批次，每批至多 100 条，单轮总预算 15s。单项受保护的活 lease 可以 pending，不能饿死其他可恢复项；后续真实权限/存储错误仍影响健康，不能把第一个 ResourceBusy 等同全轮成功。启动/worker 恢复的 cleanup I/O 保留外层取消/截止，不继承普通业务补偿路径的额外预算。

首停停止新领取，已登记的准备、reader/writer、probe、恢复和两条存储 Transport 的 I/O 继续 drain。HTTP、Secret batch、出站和对象都 drain 后才停止 DB 准入；Force 的 socket 关闭、join 和最后 DB 关闭共用额外 1s。初始化晚返回 Runtime 拒绝启动准入，但保留关闭所有权并加入原首停预算；Force 已开始则使用原 force context，即使其已过期也不续时。健康调度每 10s、同轮 2s，超过 20s 的旧结果失效；`ready` 仍为 false。

## Transfer 的持久身份与授权

`NewTransferService` 消费同 Service/Store/Process、正式 `RunnerTransferAuthority` 和固定 transfer endpoint。所有方法先真实规划/一次合并锁，再验证完整当前 Actor、owner 父 gate、Runner 认证代次、Operation/Execution 与用途。TransferAuthority 不代替 Resource/ObjectRead/Lease/Project/Audit 端口；缺必需端口不能交 URL。owner/Project 当前授权先于幂等、版本和已完成/已取消历史结果。

Issue command 与 PUT 的稳定 output/upload command 分开。业务语义摘要排除 trace/request/session，绑定原稳定 Actor、Runner/Operation、方向、owner/object、manifest 和请求期限；同 key 异义拒绝。同义重放不换 TransferID/key/deadline。unknown 必须等待原 command writer 终局再核事实；规划时未见、锁后出现的对象映射变化明确回滚并要求调用方重采集，不自动重试。

签发最多 32 个未收敛 grant。PUT staging 是原 upload_attempt 的 `runner_staging` 分支，没有伪 Process/spool 身份，计入同一全局 64、每命令 2 个未收敛 attempt；旧 `private_candidate` 约束保持。staging 永不成为 available locator，旧通用 writer/恢复逻辑不能把它验证发布或据 Central 死亡释放其外部 lease。

默认有效期 60s，允许 1–300 整秒。DB deadline 一次确定；SDK 签名后再解析实际 `X-Amz-Date/Expires`，实际 wire 截止不得晚于原 deadline。PUT 必签 host、Content-Type、Content-Length、全文 SHA-256 checksum 和 `If-None-Match:*`。GET 只选择当前可读的 available immutable candidate；transfer lease 本身不能授予旧版本读取。返回 material 前另一个短 Tx 再验当前权限、撤销和有效期，提交 unknown 不公开 URL。

URL 是 bearer，无法密码学绑定网络调用者为该 Runner；`ForRunner` 只是受信 D17 适配器的显式投影口。Inspect/fmt/JSON/slog 不包含签名 URL、bucket/key 或原凭据。只有 D17 的真实 Runner 适配验收才能证明远程网络可达，不 fallback Central。

## 完成、终止与清理

PUT Complete 先验证真实持久 completed evidence，同 Tx 取得 source lease/checkpoint，确认提交后只读一次完整 staging 流到 sealed spool。用该同一流确认长度/SHA，再在原 ObjectID/upload 下创建新的 private candidate，复用 UploadPrepared/verify/publish，同 Tx 写 transfer complete 和 Audit。已有 verified/published candidate 的重放不再次取 staging 或发送 payload。candidate 预留 unknown 未发送时仍持有精确本地 join checkpoint，等原命令锁终局后释放 writer，不能因暂时读不到行而遗忘它。

GET Complete 记录可信接收事实，不由 Central 重新 GET 猜测 Runner 是否收到。归档拒新签发和新 payload 发布，当前正式 Converge 可记录精确终局。`completed` 与 `lease_retirement` 独立；前者可先完成业务，后者要求该 grant 所有远端请求已 join、存储侧新准入已关闭、本地 source/writer 已 join，PUT 还须 staging 清零。HTTP 200、TTL、Central 死亡、停止索引或部分证明都不足以释放 lease；维护每次重新核 current proof，撤回/错用途不释放。

精确 stopped retirement 可结束尚未完成的 PUT grant：本地 I/O join 后只 gate 该 staging/标 failed，保留原 output command/ObjectID。marker 与 lease 收敛后，新 issue key 可在当前授权和原配额内重试；旧 grant 不能迟到发布。仅有 completed 证据但尚待发布的 PUT 不会被此分支擦除。

Cancel 先同 Tx revoke/Audit/gate，阻止后续 material/发布；可能已发送的 bearer 无法召回。cleanup 对 exact staging 用无条件零字节 marker，确认成功后完整读回空 SHA，首版永不自动 DELETE marker。真实慢 PUT 与 marker 可能按存储锁串行：必须等有界操作实际结果，不能宣称 marker 一定先完成。marker 只证明 payload 消除；即使旧条件 URL 已被 412 拒绝，仍不替代独立退休证据。活动本地 source reader 保护 staging，不让 marker 抢先擦除待校验源。

Project 删除持原 Project EX/完整计划，撤销 grant 后等待正式 external lease 终局，再在删对象 metadata 的同一 Tx 先清 00007 的绑定、manifest、证据等业务事实。迟到 Complete 不能重建行；永久随机空 marker 不含 Project/owner/名称/正文。当前正式迁移 00007 仅 Up；不能加 Down 或改历史 00001–00006。
