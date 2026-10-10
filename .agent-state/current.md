# Human Owner Skill 安装 HTTP：当前检查点

- 工作树 `/workspace/agenteam-skill-install-owner-http`，分支 `ai/skill-install-owner-http`，基线 `46a95480`。coordination 唯一写本片源码/测试/记录；root 执行 Git 与资源调度。借入的普通安装/读取/清理 Service、00036 及其原失败/验证记录不由本片修改。
- 首片已实现附加 catalog GET/HEAD、安装 POST 与同原完整输入的 lookup POST；已完成本片定向 race 与三包 vet 的有效补集；HTTP PG 与 browser 尚未运行。旧 builtin-only GET/HEAD 与详情仍走原 handler。
- catalog 使用原 Service admission/Owner read transaction，当前 User/Project/view/limit 签名游标、SkillID 升序 keyset、limit 1–100（默认25）、SQL limit+1；原 published 初始化与每个 ordinary publication 原映射重验。没有 OFFSET/count/快照承诺。
- 两 POST 经原 Account Origin/CSRF/Session boundary，Actor 仅真实 Human；1 MiB 严格 JSON 包装原 text_files/build/install 输入；Lookup 只调用同一个 Service.LookupInstall，不重发或换 key。公开成功仅 skill_id/revision/version，原 Unknown/当前撤权安全 Problem 保留。
- app 精确追加三个请求分派，catalog/lookup 在原泛详情前；创建同一 Service 的 catalog，使用现 cursor keyring，不改配置/DDL/initializer/lifecycle。旧读2s、新写/lookup30s均包括原HTTP I/O尾并保更早截止期。
- 必要纯检查已闭：pure-01 的8新top＋2旧读取兼容top（59sub）PASS；原Schema夹具假定所有路由均有HEAD而FAIL、vet未跑。只修testdata/schema.py后，pure-02的Schema单top（45向量/30个bodyless HEAD状态）race和Skill/HTTP/app三包vet全部PASS；两轮原Wait/group/runtime双尾完整、输入不变，原pure-01整体FAIL保留。详细原件在 output/ai/skill-install-owner-http/pure-{01,02}/，没有重复已通过10top。
- 源码与Unicode标量窄修已获非作者有限静审；新增isolated/mismatched surrogate在原bytes拒绝，合法pair/U+FFFD保值。本轮无Go/cache/资源在途，热cache已归还root。下一步保存本次最小夹具/记录，进入真实Human HTTP最小调用链。Skill 安装原 PG36 有独立作者/资源队列，不重复审其已接受方法。完整 F1、AgentRun、Registry callable Backend、assignment、update、Runner source、包流与旧 STOP 均不因本片升级。
