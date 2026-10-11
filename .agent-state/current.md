# Runner write-file 本地库

基线 `7c874adc`；分支 `ai/runner-write-file-adapter-20261011`。本树由 variables_review 单写 Runner filesystem/workspace 及相邻测试；root 独占 Git。

目标与合同见[本片卡](../docs/development/work-items/d16-runner-write-file.md)。已落 Write 请求/结果、Root/Handle 调用登记、Linux 原子写及相邻真实临时目录和生命周期控制。尚未编译、测试或接受；Go/cache 窗口未授，无运行进程。

不接 Tool/WSS/DB、默认 executor、shell/edit-file 或生产绑定；全部旧 STOP 保留。下一步做限定格式/静态自查并交 root 固定源码；基础 race/vet 仍等待独占工具链窗口。

主线全局协调记录继续由 main 的 `.agent-state/current.md` 维护，本分支不覆盖。分叉时已核清理 30 WorkTree / 31 local / 62 remote，147 旧引用待核、1 个历史引用明确保留；SkillHTTP 热缓存、SkillsMilestoneResume 只读 modules/runtime、ObjectSafe 原件与 Move 未完树继续保护，既有 Object/tools HTTP/SPA/Jina/Image STOP 及生产未绑定范围不变。历史恢复入口与清理证明可从 main `7c874adc` 的原全局索引及其链接恢复。
