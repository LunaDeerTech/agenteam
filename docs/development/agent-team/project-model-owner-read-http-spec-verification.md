# D09 Project Model Owner 只读 HTTP 规格验证

2026-10-07：[正式 rev2](../work-items/d09-project-model-owner-read-http.md)已通过完整限定独立 STATIC，root 已采纳；规格 `f68c5c958404cdaf69231b8c1c7918efbb931382` 已推送、root 核远端一致。固定已接受产品基线为 Owner-read `901eb54605d293d4308caadd278c2c3a7ae1b824`。这是规格接受，未授权本卡实施、Go 检查或资源；D08 Update 仅规格 `03d1c107` 已接受，其实施尚未接受，必须先完成产品验收及共享 `account.go` 独占交接。

## 1. 接受范围与原问题

规格覆盖 Project Provider/Model 列表与详情、可用 chat 安全目录五个 GET/HEAD 资源及默认 root，13 技术路径（12 新文件、1 既有）加另授的 README 末件。沿用已接受五查询、当前 Session/Owner、原只读事务及分页，目录只含既定七字段；不开放配置写、credential 材料、lookup、UI 或 Resolver。8 MiB 完整表示 admission、原 Read Unknown 优先和实际 HTTP Write/Flush/Close/取消回调终局属于后继实现与动态验收契约，不是本次已执行结果。

[rev1 完整独审](project-model-owner-read-http-spec-verification-evidence/independent-rev1-review.md)原结论为 NEEDS_REVISION，唯一 B1 是“过期 token”误暗示时间 TTL。[原 cursor 核对](project-model-owner-read-http-spec-verification-evidence/cursor-finding-note.md)与[正式 rev2 差量](project-model-owner-read-http-spec-verification-evidence/draft01-to-formal-rev2.patch)保留修正过程：cursor 无 TTL；当前加载 keyring 不含 token kid 时拒绝，保留旧 kid/key 仍可验签，并继续逐页重核当前权限与查询绑定。没有新增在线热轮换或公共库行为。

[rev2 完整限定 PASS](project-model-owner-read-http-spec-verification-evidence/independent-rev2-review.md)复用原完整审查并独立核全部差量；[原结果](project-model-owner-read-http-spec-verification-evidence/independent-rev2-result.json)确认八处变换可逐字逆向还原 draft01，唯一技术变化是 cursor 措辞，其余为标题、状态、四个正式链接和结尾归位。原 rev1 结果保留，没有覆盖成 PASS。后继重点包括业务前检查 Flush 能力而非提前 Flush、发布预编码同一完整 bytes、admission 先于新增 Validate/clone/Marshal、不外推前序库/DB/RSS 上界，以及真实撤权 Tx 屏障与默认 root/辅助 fixture 来源区分。

## 2. 固定来源与接受页

[作者 inputs02](project-model-owner-read-http-spec-verification-evidence/author-inputs02.json)保留 46 个固定映射输入；作者原检查记录 45 项接受指纹及 1 项仅指纹佐证的历史卡。独审逐项核同一来源并补核 cursor，未读活动 Update 18 技术源。原[自查命令](project-model-owner-read-http-spec-verification-evidence/author-check-command.json)和[输出](project-model-owner-read-http-spec-verification-evidence/author-check.raw)均保留；12,582,913 B 的合法大 efforts 示例为静态算术论证，不是产品容量测试。三项 native、四个新 PG top、七个旧 top 及将来 Update 最终联合检查仍是待实施的验收要求。

正式受审卡 SHA 为 `97b70dfde863fa73b88c25dd14c6a7d95476282eee3ea91972b426c41b61cc36`；[行政接受差量](project-model-owner-read-http-spec-verification-evidence/accepted-administrative.patch)和[原记录](project-model-owner-read-http-spec-verification-evidence/accepted-administrative-result.json)只更新页首与末尾当前状态句，接受卡 SHA 为 `28fb448189e3845c6be2a169f42cd1445d731a2383f29c78d592c59cb1debdce`，其余技术字节保持。原 rev2 landing 命令仅记录当时 tool stdout 位置，没有补造独立 raw。

## 3. 保存范围与边界

[来源映射](project-model-owner-read-http-spec-verification-evidence/source-map.json)保存 21 个小原件，共 132,316 B。`check-result.json` 与 `check.raw` 原字节相同，仅存一份并保留来源别名；前序 inputs01 和完整受审卡前像仅保指纹及可逆依据，接受卡复用规格提交。46 个产品映射源、既有归档树、重图、cache、binary 和私密 runtime 均未复制。原 Markdown 内引用保持原来源位置语义；新正文链接单独检查。三份原 patch 的空白上下文行按原字节保留，不增加忽略规则。

归档自查仅核来源/复制字节与哈希、JSON、脚本语法、UTF-8/LF、正文链接及新增格式；未执行原脚本、Git、Go、产品或资源。系统管理员统一会议 initial/update（含首轮标题）、Project 无 override/复制默认、compaction/Execution Summary 规则不改；production Resolution/Invocations 与 D24 未绑定，ready503、完整 D08–D28/E01 未完、E01 未开始和 Object runtime join/OpenAI tools 独立验证/SPA concurrent-publication 三停止保持。
