# 4089 后 Project 创建前沿（仅固定源调查）

推荐先交付 **Project 初始化收敛授权库**：落实 D10 §10 已留出的 `InitializationConvergenceAuthority`，共5个新技术源，未来 root 另授README末件。完整 scratch 工程边界见 [draft-rev1](draft-rev1.md)，不是当前实施授权或独立接受。

真实依赖已经满足：D08 持久 Project/Creation、原成功 gate、PG 同 Store 活 Tx/held EX、ProjectInitialization 正式 Actor 已存在；新 gate 只读取 Project 自有事实并返回原事务内授权结论。它覆盖 accepted/initializing/failed/completed 一致状态，为既有元数据观察和失败收敛提供正式 Project 入口，不改成功 gate，不成为 Reserve/Send/Publish 或 Human Owner 凭据。request 只有三字段，owner 从 Creation 与当前 Project 两方事实交叉核对，completed 历史 result 不替代当前 metadata。

两处必须区别旧文档与实际：D10 P1 真实 builtin/不可变包已接受，不能重做；D10 旧卡所称 Object ProjectFactAuthority 未实现已过时，a716ae2a 的 checker/私有 sameTx witness 已接受。但 Skills 持久初始化、专用 Owner/Audit routing、生命周期参与者/根仍未绑定。下一完整 Skills 发布/Project 创建 HTTP 不能现在闭合：D10 §6 要求未 join work 保持共享 guard，Object Runtime 回归已证明上层 Drain/ProcessGuard 过早退休；四关键源从42e3到4089 blob未变，见 [stopped-source-check](stopped-source-check.json)。不得用新harness、stub或改名恢复该停止链。

本小结果没有共享 root/HTTP/迁移路径冲突，不消费活动UI。验收是精确四状态与请求/owner对应、同Store活Tx/EX/poison、零写与原成功gate兼容的 pure+真实PG。新fixture只设置明确的Project域授权输入，正式service/Store验证，不把它当Skills初始化或Account创建E2E。后续完整创建HTTP必须等真实Skills与停止依赖另行接受；本卡不是绕过它的捷径。

需独立审的工程提案只有active-only及严格事实/错误表：accepted/failed恢复为active未初始化；completed非active必须交现有LifecycleCause而非初始化服务，合法active改名不因历史快照失败。没有新的用户产品问题；管理员统一Meeting Summary规则不变。来源固定 `4089d13128da8680955005d9507c8a7da74af1f1`，最小57文件 blob/SHA清单 [inputs01](inputs01.json)。本轮无Go/数据库/网络资源、无repo或Git写入。
