# Artifact 与浏览器业务下载

D05 B02 提供 `internal/central/artifact` 与 `object.Downloads` 库，行为契约以[对象实施规格](../work-items/d05-object-storage-design.md)为准。它们消费已实现的 Object、SourceReads、Audit、cursor 和事务端口。Central 已装配 D05 Object Runtime 及 D07 真实 Session、本人头像对象授权与账户 HTTP；Artifact 和通用下载 HTTP 本身仍未安装，Project、Agent、Execution、Operation 和 Tool 的相应生产授权仍待绑定，缺失依赖明确拒绝。头像读取不是 Artifact 下载适配，Runner transfer 授权仍未绑定。

## 创建、恢复与清理

`artifact.New` 接收实际 Store、OwnerProvider、Objects、Uploads、Cleaner、SourceReads、SourceResolver、Audit 和 cursor keyring。OwnerProvider 依赖正式 `artifact/contract.Authority`，先发现真实 Actor/Project/Execution/Operation 的锁依赖，在一次完整锁集合获取后重新核验映射和当前权限。Artifact/file ID 不代替 Project 或 Execution gate；规划信息不授予访问权。测试中的 SQL 身份适配器只用于 owned fixture，未进入生产代码。

| 路径 | 正式行为 |
| --- | --- |
| `CreateFromContent` | 接受最多 1 MiB 的有效 UTF-8，受限 spool 计算 SHA，保留命令与 upload attempt，完成实际存储校验后发布 |
| `BeginUpload` / `UploadPayload` / `CreateFromUpload` | 当前 Human 先获得本域持久 prospective intent；上传返回匹配原 actor、owner、cause 的 receipt；创建只消费该目标的已验证 receipt，不重复 PUT |
| `CreateFromSource` | 首次来源事实与 Process/source lease、命令同 Tx 持久化，提交确认后单次开流；复制到新 ObjectID/key，发布前再次检查原来源当前权限 |

Artifact 行、canonical reference、成功 Create Audit 与完成命令同 Tx 提交。幂等语义以明确业务 primitive 字段编码，包括原稳定主体、来源完整 ref/receipt、展示参数和 expected version；序列化失败直接拒绝，不计算缺失内容的摘要。Session/request trace/重试调用 attempt 不替代业务命令身份。完成态重放先检查当前目标权限及原输入，直接返回原结果，不调用来源解析、GET 或 PUT；源后来失权、删除或 receipt 被消费不使完成态变成新操作。

尚未完成的来源复制始终沿第一次持久事实恢复；当前来源无权读取、映射变化或旧实例的 lease 仍 active 时拒绝发布/接管。COMMIT unknown 不表示回滚：命令锁先等待原 writer 终局，读回原记录；source 获取和开流核实任一提交仍 unknown 时不发 GET。SourceLease 是 issuer/Process/Actor/来源绑定的私有句柄，不能从公开 ID 构造；原 Tx 尚 live 时禁止开流。读者 Close/join 之后才能释放 lease，未知释放保留 checkpoint。

`CancelUpload` 与 `CancelCreation` 是当前主体对未完成操作的收敛端口，先持久取消状态，再调用 Object 正式取消；已完成 Artifact 不存在任意删除 Core Tool。Project 清理要求真实生命周期 operation/version 与 Project gate，每次最多处理 100 个可见 Artifact 和 100 个未完成身份，再推进有界来源 checkpoint 与 Object 的公平物理清理批次。canonical 释放与业务删除同 Tx；外部或尚未 join 的 lease 保留 pending，不能仅因业务表已空宣告 payload 消失。物理对象与实际 source/reader 全部收敛前保留原 command/intent 恢复事实；最终在同一 Tx 经正式 DownloadCleanup 端口清除本 Project 的 grants/attempts，并物理删除 command/intent 整行，包括展示参数、来源 JSON 和内容事实。最终只保留 ProjectID/operation/version/state 的最小 cleanup 门禁回执，不转存历史副本或用占位字段伪装删除。当前授权后的终局门禁覆盖新旧 key、BeginUpload、Prepare 和迟到发布；原记录消失不允许重新创建目标。最终 Tx 回滚/unknown 不提前报告完成，后续从相同 operation/gate 核实或恢复。ObjectMaintenance 仅根据本域持久 cleanup cause 使用收敛权限，不因此获得普通读取权。

## 列表与预览

名称为 1–255 UTF-8 bytes，拒绝路径分隔及控制字符；描述最多 4 KiB。列表默认 50、最多 200，按 `created_at DESC,id DESC` 翻页，cursor 绑定 Artifact 查询域、Project、过滤和顺序，每页重新授权。`name_query` 按字面子串处理 `%`、`_` 和反斜线，不把用户输入当 SQL 通配符。

文本预览默认 8 KiB、最多 64 KiB，offset 为字节位置且必须是 UTF-8 边界；尾部仅返回完整 rune，并给出实际 next offset。无效 UTF-8 返回明确的 file 投影；支持的实际图片返回 image ref，PDF/Office 等文件不解析正文。列表/预览在输出前再次完成当前 Read 授权和同 Tx Audit；审计失败或 unknown 不返回结果。Audit 只含固定技术事实和关联 ID，不包含正文、展示名称、搜索词或签名 URL。

## 浏览器下载组合

`object.NewDownloads` 接收实际 Object service、独立 DownloadKeyring 和 typed DownloadProvider。`artifact.Sources` 是当前唯一业务绑定：读取真实 Artifact/file/object 事实，生成 `ArtifactDownload`、原 Artifact resource 和 producer 的审计。Uploaded ref 仅在实际 Artifact canonical 已绑定时可以下载；prospective receipt 不授予普通下载。Knowledge、Execution 和非 Artifact Uploaded provider 未绑定返回 `DEPENDENCY_UNBOUND`，不会伪造 ArtifactID 或用对象维护 Audit 替代业务事件。

`LoadDownloadKeyring` 使用独立随机 32-byte HMAC 材料、严格 canonical JSON/base64，支持 current/历史 kid；必须传入实际 cursor 和 Secret keyring，与两者全部当前/历史 key 做材料隔离比较。Central 已必填配置 `AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING`，Account keyring 载入时再核验四用途材料独立。签名固定 GET、原 User、完整业务 ref、对象内容事实、grant、模式、文件名和期限；默认 60s，最长 300s。`IssueDownload` 当前授权后将 grant 与 issued Audit 原子提交，未确认提交不返回 URL。私有 URL 只通过 `ForHuman` 输出给原 User；Agent、日志、模型结果和通用格式化不得获得 bearer。

`OpenDownload` 每次验证签名、期限、原 User、当前 Session/Owner、provider 映射及 grant，再确认 started Audit，取得真实 reader lease 后才 GET。支持单一 `bytes` range、前缀和后缀；不支持多区间。416 的总长度只通过已经授权的专用错误投影返回，不能从无效 token 探测对象大小。Range 不声称重算全文 SHA；完整响应沿 Object 的 64 KiB 尾部保留机制，实际 EOF/长度/SHA 通过才交付最后一段。

生产 HTTP adapter 必须由真实 Session 解析当前 Human，取得 `DownloadStream`，调用一次 `StreamHTTP` 并始终 Close。writer 必须支持 `http.ResponseController.SetWriteDeadline`；已写 headers 后发生错误应使用 `http.ErrAbortHandler` 中断，不能追加 Problem JSON、改写状态或重发。未来 adapter 还须避免访问日志、referer、错误或 trace 记录原 bearer path。当前真实 HTTP 测试 adapter 仅存在于 `tests/objects`。

响应包含 `nosniff`、`private, no-store`、`no-referrer`、sandbox CSP 和 RFC 5987 文件名；HTML/SVG/XML/PDF、二进制及 MIME 伪装强制 attachment。仅受限的实际图片与有效文本候选允许预览，文本按安全 plain text 输出。强制关闭通过原 operation context 的 socket deadline 中断实际 Write，reader 与 HTTP I/O join 及后置处理都算在 Object Drain/Force 内；首停后的已准入流继续使用原预算。

每次传输记录真实 accepted `Write` 字节数，部分写入后失败也保留精确计数；这不证明浏览器收齐。尾部失败先持久技术 checkpoint，再将 sent/failed 与业务 Audit 同 Tx 确认。后置 Audit 失败或 unknown 返回 `AuditPending`，只可调用 `ConfirmDownloadOutcome` 在当前权限下核实同一 attempt，绝不重发 payload。grant 到期或逻辑撤销只拒绝新请求，不把已经准入的 reader 伪报为停止。Project 永久清理则在实际对象收敛后清除 grants 及其 attempts；checkpoint 的 Project SH 与最终清理的 Project EX 互斥，迟到终局发现绑定已删除只能拒绝，不能重新插入 grant/attempt 或把清理回滚。

## 验证入口

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-objects.sh -run '^(TestArtifact|TestDownload|TestSource)'
```

真实测试使用固定 PG 与 SHA 核验的源码构建 MinIO、临时凭据、内部网络和精确 nonce 清理；不连接现有部署。协议与构建来源见[固定研究报告](../work-items/d05-object-storage-research.md)，库/fixture配置见[后端说明](README.md)。阶段的实际通过、失败修复及未验证范围由[D05 主卡](../work-items/d05-object-storage-artifact.md)记录；当前 D07 Session/头像与 MinIO 的生产组合另见[D07 主卡](../work-items/d07-account-session-smtp.md)，不把这些结果当作 Artifact/下载 HTTP 或 Runner 集成已完成。
