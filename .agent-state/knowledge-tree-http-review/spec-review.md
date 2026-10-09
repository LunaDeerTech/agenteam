# Knowledge 树命令 HTTP SPEC rev1 独审

输入：`/workspace/agenteam-knowledge-tree-http` 正式基线 `ce65714aac6eb4995a43fc427a2c77e6497470a7`，作者停止写入的 `docs/development/work-items/d12-knowledge-owner-tree-http.md` 与 current。本人未参与 SPEC、路由协商或实现，只读对照真实 B02/Account 源码。结论：有限可执行合同足够开实现，无 must-fix；没有实现、编译、HTTP/PG/socket 动态接受结论。

## 正式接缝

- `knowledge/service.go` 的 UpdateDocument 真实签名支持原 actor/CommandMeta/Project/Document/UpdateRequest/source。title-only 用 Title 指针、ReplaceSource=false、source=nil，ExpectedVersion 为原正版本；`commands.go` 的 no-op 保原版本，真实 title 改变安全递增一次。Move/Delete 不接 ExpectedVersion。未偷偷添加创建、内容源或上传口。
- `contract/commands.go` 的 UpdateDigest/MoveDigest/DeleteDigest 校验正式 CommandMeta；语义绑定当前稳定 User、Project、目标、原 expected/version/parent 和 token 原字节摘要，不含新 Session 或 transport RequestID。CommandIdentity 仍为正式 knowledge/Project/command/key，不由客户端提交摘要。HTTP Foundation key 校验应直接使用正式 Parse/Validate（其字符集不是任意 ASCII）。
- `commands.go:LookupCommand` 实际取 Command EX 和完整 scope locks，在同 Tx 内 current readScope 后查原命令、原 User/digest，返回 committed/in_progress/not_observed；Lookup 不执行原命令、not_observed 不自动构成全部旧 work 已退休。卡已保持 Unknown 的原 Fault/CommitState/Cause 分类，无公开 raw cause/SQL 需求。
- `tree.go:DeleteSubtreeInTx` 当前身份/readScope 后，先按原 digest/User 处理 committed receipt，再进入未完成命令的 Mutate、签名/时效/范围检查。HTTP 只 ParseConfirmationToken 的传输形状，不能提前 Verify 或自动刷新 token；这使过期/已移除旧验证 key 的合法完成重放保持可行。PrepareDeleteSubtree 是当前 readScope 的完整树快照，不能当后继删除许可缓存。

## 投影与 I/O

`contract/tree.go` 的 DeletePreview.Validate 核真实节点 Validate、单根连通/无环/唯一/同 Project、scope digest 与 token claims 中 Project/root/digest/expiry；范围完整性仍由 B02 在树锁下完整 SQL 枚举负责。卡没有声称纯校验可证明数据库未漏节点。确认材料只在原授权 Human preview 中显式 ForHumanResponse，通用 Marshal 拒绝。DocumentRef 含私有 ObjectID，卡规定显式12字段且先验证隐藏字段，符合正式 metadata/receipt 安全边界。历史 receipt 可为当时 active metadata，不为旧内容或 ObjectID 提供读取许可。

Move 的 changed 等价于原 expected/target parent 是否不同，真实服务已经校验 expected 命中；内容版本不因 move 增长。rename 成功的版本只允许 expected 或不溢出的 expected+1；Lookup 的 changed 与原版本关系必须一并校验。Delete 原结果已验证有序无重 IDs 且含根；cleanup_pending 不被 HTTP 推成物理清理完成。

五路独立 commandhttp，公开构造仅真实 Service/HTTPBoundary；不依赖未验 read HTTP，不改 root 或 migration。Account CheckRequest/RequireHuman 在方法/参数错误之前，Project 当前 Owner/初始化/归档/Deleting语义由 B02 同 Tx 重验。所有五路2s从浏览器前置前开始，沿原 read/write deadline、body Close、取消 callback/Tx 实际尾及绝对 deadline；不能盲复制不同 mutation 预算。成功 mutation 后投影/编码/写出失败 abort，不能重新给 not_committed；预览/Lookup 的无本次 mutation 投影失败可安全报依赖不可用。5MiB完整表示/坏后项零发布要求可按有界私有编码实现。

## 实施和验收界限

卡中 pure、标准 Schema、原生 I/O 与真实 Account/B02/PG/Unknown 矩阵均为后继验收；本次仅验证接口和规则能落实，没有运行这些门。需要保持安全加法、严格 nullable parent presence、正式 key 字符集、body/原取消 callback 尾等已有合同落实点；不因此新增产品规则或扩展普通测试排列。原 Runtime 停项、生产 root/正文/下载/D13/UI未绑定状态不变。
