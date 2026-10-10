# Human Owner Skill 安装 HTTP：当前检查点

- 工作树 `/workspace/agenteam-skill-install-owner-http`，分支 `ai/skill-install-owner-http`，基线 `46a95480`。coordination 唯一写本片源码/测试/记录；root 执行 Git 与资源调度。借入的普通安装/读取/清理 Service、00036 及其原失败/验证记录不由本片修改。
- 首片已实现附加 catalog GET/HEAD、安装 POST 与同原完整输入的 lookup POST；当前源码已格式/JSON/whitespace 检查，尚未 Go、HTTP PG 或 browser。旧 builtin-only GET/HEAD 与详情仍走原 handler。
- catalog 使用原 Service admission/Owner read transaction，当前 User/Project/view/limit 签名游标、SkillID 升序 keyset、limit 1–100（默认25）、SQL limit+1；原 published 初始化与每个 ordinary publication 原映射重验。没有 OFFSET/count/快照承诺。
- 两 POST 经原 Account Origin/CSRF/Session boundary，Actor 仅真实 Human；1 MiB 严格 JSON 包装原 text_files/build/install 输入；Lookup 只调用同一个 Service.LookupInstall，不重发或换 key。公开成功仅 skill_id/revision/version，原 Unknown/当前撤权安全 Problem 保留。
- app 精确追加三个请求分派，catalog/lookup 在原泛详情前；创建同一 Service 的 catalog，使用现 cursor keyring，不改配置/DDL/initializer/lifecycle。旧读2s、新写/lookup30s均包括原HTTP I/O尾并保更早截止期。
- 下一步保存首片并做新增定向 race/vet＋Schema，非作者实际源码审后尽早真实 Human HTTP 最小调用链。Skill 安装原 PG36 有独立作者/资源队列，不重复审其已接受方法。完整 F1、AgentRun、Registry callable Backend、assignment、update、Runner source、包流与旧 STOP 均不因本片升级。
