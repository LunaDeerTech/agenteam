# Runner write-file 本地库

基线 `7c874adc`；分支 `ai/runner-write-file-adapter-20261011`。本树由 variables_review 单写 Runner filesystem/workspace 及相邻测试；root 独占 Git。

目标与合同见[本片卡](../docs/development/work-items/d16-runner-write-file.md)。最终技术 `7ff8174a` 已完成本地 Root/Handle 原子 Write→Read 与实际 Stop/Drain：原 `basic01` whole FAIL 保留，旧 31 top PASS；窄修后 `basic02` 原 10 top / 13 sub race 与两包 vet PASS。两轮各 1,100 项实际输入前后相同，原工具 49447 / 37839 已分别实际退出 1 / 0；无本任务 Go/cache writer。

不接 Tool/WSS/DB、默认 executor、shell/edit-file 或生产绑定，也不构成恶意宿主隔离；全部旧 STOP 保留。下一步由 root 保存最终文档、完成有限主线交付，再单独安排原件保全与清理；目前未移动输出、未删除树或引用。[恢复说明](runner-write-file-adapter/README.md)记录原失败、历史源与保全清单，不重复运行已通过控制。

主线全局协调记录继续由 main 的 `.agent-state/current.md` 维护，本分支不覆盖。分叉时已核清理 30 WorkTree / 31 local / 62 remote，147 旧引用待核、1 个历史引用明确保留；SkillHTTP 热缓存、SkillsMilestoneResume 只读 modules/runtime、ObjectSafe 原件与 Move 未完树继续保护，既有 Object/tools HTTP/SPA/Jina/Image STOP 及生产未绑定范围不变。历史恢复入口与清理证明可从 main `7c874adc` 的原全局索引及其链接恢复。
