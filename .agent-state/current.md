# 当前任务：Knowledge Move 主线兼容恢复

工作树 `/workspace/agenteam-knowledge-move-recovery`，分支 `ai/knowledge-move-recovery-20261013`。保存候选 `778adc62` 基于 main `21ce3268`，当前普通合入最新已交主线 `c82cfad3`；保留用户 `708a8f54` 的远程 HTTP/登录与安全随机 UUID、状态筛选及 E01 数据规则候选。旧 `/workspace/agenteam-knowledge-move-ui` detached `0646b6bf` 和 01–05 失败原件保持不动，原第五轮 wholeFAIL 不追认为通过。产品范围和历史见 [Move 卡](../docs/development/work-items/d12-knowledge-move-ui.md)。

Move 产品和必要测试已移入：保留当前 Delete/Rename/Text/ArtifactCreation，补页面导航确认/current-read/dispose、Session 双向未决意图互斥与同步 Session 退休守卫；主线已有 rename 定位修复未重做。Node observer 的 Move 参数追加末尾，独审已核旧 33 形参和旧外部调用映射保持。Go fixture 使用当前默认 Project/Skill/Object；thin 的 state 与 Move 两个独立 opt-in 均保留，Move 两原场景、120 秒/sub、45 秒浏览器、零重试、自有资源退出硬门不扩大。

原候选的 [轻验](../output/ai/knowledge-move-recovery/light-summary.json)已全部实际退出：8 spec/57 项（Move 37、Delete 12、Rename 3、ArtifactCreation 5），完整类型与构建通过；新 Node helper/spec 零诊断，旧导入的 5 项诊断保留，不能称整个浏览器 harness 类型通过；两模式 list 各命中 1 项。产品和适用方法已获有限源码独审接受；本次合并精确解决 current、Session import 和 thin 两 scope 并存，合并差额尚待审查与必要补验，不把原轻验冒成新组合验收。

root 尚未放行新的真实集成窗口。下一步由 delivery_review 核三路径差额、validation_runner 统一完成必要补验及 App 唯一编译/list，再按 root 明确排期；repo_recovery 负责保存。无旧 driver 重跑、外部模型调用、STOP 树变更或共享大资源复制。

继承主线的状态筛选及补验结果见 [规划卡 §27](../docs/development/work-items/d11-work-owner-planning-ui.md#27-所选-sprint-的状态筛选2026-10-13)。E01 仅交付固定版本部分来源目录与数据规则，权重/分母/coverage 未冻结、完整系统与真实平台试玩未验，见 [E01 候选](../docs/development/work-items/e01-minecraft-java-1.21.1/README.md)；整个平台长期目标未完成。
