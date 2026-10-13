# 当前任务：Knowledge Move 主线兼容恢复

工作树 `/workspace/agenteam-knowledge-move-recovery`，分支 `ai/knowledge-move-recovery-20261013`，恢复基线 main `21ce3268`。主线程已明确恢复保存产品的兼容工作；旧 `/workspace/agenteam-knowledge-move-ui` detached `0646b6bf` 及原 01–05 FAIL 保持不动，原第五轮 wholeFAIL 不追认为通过。具体范围及历史事实见 [Move 卡](../docs/development/work-items/d12-knowledge-move-ui.md)。

尚未交付的 Move 产品和必要测试已移入：当前 Delete/Rename/Text/ArtifactCreation 共存、页面导航确认/current-read/dispose、Session 双向未决意图互斥与同步 Session 退休守卫。旧 rename 定位修复主线已有，未重复移入。新增六个兼容用例，现有 Move 四 spec 继续保留。Node observer 增加默认关闭的 Move 闭集，原响应/cut/consumer/Lookup 判据保持。

Go fixture 与 thin 由 next_backend_delivery 独立写域迁到当前默认 Project/Skill/Object；两原业务场景、120 秒/sub、45 秒浏览器、零重试和自有退出门不扩大。产品/Node 作者 knowledge_move_recovery；Git 由 repo_recovery、实际检查由 validation_runner，独审 delivery_review。

当前源码首稿停写待精确格式和轻检查，尚未声明新检查或真实浏览器通过。root 尚未放行新重窗口；先审查兼容实现和执行判据，吸收状态筛选最新 main 后再按 root 排期。没有旧 driver 重跑、外部模型调用、STOP 树变更或共享大资源复制。
