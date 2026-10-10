# Knowledge tree-command HTTP 主线装配窄审

装配目标 `/workspace/agenteam-knowledge-tree-http-delivery`，基线 main `3b7ed9da35844e3a367cc5e9da0cf424ab36499a`。消费作者既有 `delivery-plan.md` 的 19 路径，不另建交付清单或 manifest。

8a234e actual0：17 个技术路径在 main 基线均不存在；目标 working tree 与 index 逐字来自固定 `663c5ca384c3837f507445df8cff1bdc566f31f0`。当时全部 diff 恰 18 个新增路径（17 技术与卡），无既有 main 技术文件变更，因此 SQL、root、共享业务、锁文件和已有 read HTTP 未被覆盖。新增 integration 源的 build tag 与同包顶层 func/type 名称静核无冲突；此检查不替代 Go 编译。508140 只读核 standalone Schema helper 按自身路径定位新 Schema 和既有 common，所需 fixture/依赖沿 main 同包消费。

只有限接受源装配一致性。独立新 top 首次真实执行在 helper 误把标准 Problem 的公开 `title` 作为禁止字段处失败；Owner B 目标尚未执行，原整轮结果以执行者完整原尾为准。原 663 的该 test 尚不能作为修后候选。后续仅需对其 helper 差异独审并同步目标树这一文件，其余 16 技术路径的来源结论可复用。

独立 helper 返修静态窄审有限接受：d2d4bd 读取实际 common Problem 的 required string `title` 与正式 `WriteProblem` 的 `kind.title` 来源；b4e02a actual0 核逆去新增 summary 校验、恢复原 title 禁项后，整业务文件逐字 663。仅改为接受非空公开 summary；原 Problem code/content-type/aborted 门、全文原 title canary 与 receipt/document/object/upload/digest/key 六禁项保持，权限与 SQL 场景未改。作者原 29379→8ebaef wholeFAIL/Owner B 未到保持；本结论不是修后实际 PG 通过。目标树需同步唯一修正后的 test，再由唯一执行者取得编译与真实补验结果。

root 同源迁入修正 test 与原三个入口源后，b9d24e actual0 有限核搬迁无误：四文件逐字保存 `b23df1fce84ccad020115bfe47443449843fa1b7`；实际 import target supervisor→adapter 从本 `__file__` 定位 delivery，输入并集无旧作者树路径，确含本 main Reader 4、Skill 21、Runner 13/protocol 8、cmd 2 个生产 Go、全部 21 个 tests/knowledge Go、迁移编号恰 1..27 和模块文件。现有三类 embed（迁移 SQL、内置 Skill、弱密码表）均入闭包；除候选/MinIO 两个待就位产物外，源输入均实际存在且非 symlink。固定工具字节未变，原 7 资源、预算、Wait 与全部尾门复用，不需参数适配或重复 24 控。`--artifacts` 继续从 target 自身位置定位候选。

最终卡/tasks 文义与必要同包编译仍待 Work/Skills 的冻结输入及实际结果；无业务重跑、Go 构建、PG/socket 或目标树写入。
