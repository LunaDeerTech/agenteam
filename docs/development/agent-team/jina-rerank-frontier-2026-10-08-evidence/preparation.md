# Jina rerank 下一规格准备稿（未冻结）

本稿只整理来源补齐后的工程顺序，不定义 Jina 请求/响应格式、URL、认证头或型号能力。没有一手字段证据时，不把平台 profile `jina-rerank-v1` 或字段候选名称当作供应商承诺。

## 先决证据矩阵

| 待证主题 | 在正式规格前必须补足 |
| --- | --- |
| query / 文本 documents | 精确原生键名、类型、必填/空值/Unicode语义、原顺序与是否隐式拆分；本文不确认 `documents` 是已证字段 |
| top_n / return_documents | 精确类型、缺省、省略/null/越界语义、数量与截断规则；返回文档是否可省略及结构 |
| results index / score | 根与逐项字段，原 index 的对应关系、唯一性/范围、score 数值/范围、排序与 ties；是否允许少于TopN或空结果 |
| usage | 可缺失/null/字段单位与累计口径，计费token与input/total语义；不能从文档数或query+候选本地估算 |
| error | HTTP与原生error形状、合法诊断字段、重试事实、request-id；不把404无条件解释为模型不存在 |
| caps / endpoint / auth | 官方 API base/相对路径语义、认证形式/作用origin、型号上下文与候选数/字节/请求上限；工程上限必须与供应商上限分别标识 |
| provenance | 有效官方来源URL、获取日期、可固定的版本/commit、原件hash、许可/适用性；若文档未声明许可如实写未知，不能发明SDK许可 |

以上本轮全部未证。后继获准取证时只需取得能闭合矩阵的少量官方文档/OpenAPI/SDK固定文件；不必安装SDK、不必发送模型请求。公开静态来源仍不能证明真实账号可用或全部兼容实现一致。

## 可继承的工程方向

完整结果候选是一个独立、有界、非流式 wire exchange，经正式 D04 单次发送后得到与原候选关联的有限排名和实际供应商 usage，或安全失败；成功要等真实 body/parser/transport join。具体公开 Go DTO、revision、闭集和文件白名单均待字段来源与独审后冻结，当前没有写入授权。

沿已验 embeddings 的同一 Budget/exact handle、取消和材料生命期；不复制配额，不从 ctx 返回或 EOF 单独推出 join，不创建新 InvocationID/receipt/consumer grant。平台已定索引/有限score/≤TopN规则保留，不发明[0,1]范围，也不拿返回document替换原输入。缺usage保持unknown，可靠usage与失败尾部的关系按正式D09保留；具体供应商字段映射仍未确定。

来源补齐后须明确所有内存/文本/数量/深度/数字上限与分配前检查，将工程限制和实际供应商限制分开；不得因无法证据化就静默丢弃请求参数、支持任意JSON透传，或复制OpenAI响应parser后改名。endpoint与认证尚未冻结，不能直接照抄OpenAI的suffix/Bearer。

## 后继验收要求与责任

1. root 冻结官方原件、确定真实能力范围、共享文件唯一写者及完整私有规格；独立验证者审查来源、字段、错误、取消/尾部和预算。
2. 唯一作者在另授范围内实施。先验证准入、深拷贝、严格完整JSON、重复键/非法Unicode/索引/非有限score/usage/上限与安全输出；不得修改旧断言来容纳新协议。
3. 再冻结实际 compiler/runtime/fixture 闭包；复用已验 D04 与现有受控 TLS server 能力的前提必须逐项核对。动态资源另由root交窗，不因依赖已恢复而自行启动。
4. 获准后用完整正反受控协议代表证明精确请求/认证/路径/单次请求、原索引/数量、usage与错误、真实EOF/Close/Drain/取消、共用64/8预算及旧text/structured/embeddings兼容。原始失败、实际wait与owned资源终局各自保留。
5. 无真实供应商调用只能称相应schema/受控fixture验证；来源研究、实现验收、完整业务Nonchat和生产root是不同结论。Object runtime join、OpenAI tools独立验证、SPA并发发布的三项停止保持。

现在的可交付物是缺口清楚的准备材料。尚不能生成含未经证实Jina字段的正式实施卡；业务Runtime/Invocation/consumer绑定继续由对应后续工作承担。
