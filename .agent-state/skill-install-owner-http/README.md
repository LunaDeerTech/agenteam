# Human Owner Skill HTTP

本片是既有普通安装 Service 的浏览器适配。源码基线及当前验证状态见 [current](../current.md)；公开契约见 [Skill Owner OpenAPI](../../api/openapi/skill-owner.json) 与 [工作卡](../../docs/development/work-items/d10-skills-owner-read-http.md)。

- 原 `GET/HEAD /api/v1/projects/{project_id}/skills` 保持一项已发布 builtin；原详情读保留。
- 新 `GET/HEAD .../skills/catalog?limit=25&cursor=...` 返回 serving 的 protected/ordinary Skill 八字段及可选 `next_cursor`，最多100项。每页独立当前授权和原读提交确认，不是跨页快照。
- 新 `POST .../skills`，body 为 `{"request":{"skill_id":"UUIDv7","mode":"create","source":{"kind":"text_files","files":[{"path":"SKILL.md","utf8_text":"..."}]}}}`。只处理既有文本包合同，元数据来自严格 SKILL.md；命令身份为单一 `Idempotency-Key`，RequestID 来自 middleware。
- 新 `POST .../skills/commands/lookup` 使用同 key 和完整原 body，只读原安装命令。失联/Unknown 不自动再次调用 Install，NotFound 不证明先前回滚；调用方保留原 intent。

原 Account CSRF/Origin/Session 与 Service 当前 Owner/Project gate 全部保留。HTTP 不接 Actor/AgentRun/Project JSON、宿主路径、URL、Object 或 Runner 标识。总请求1 MiB，原文本包边界继续生效；响应只公开三字段 receipt，错误不输出 body/依赖原文。旧读2s、新写30s包含 body/build/事务/Object/写出/close/join，原调用不在后台继续。

当前新增 pure tests 仅控 HTTP投影/分派、错误、原deadline，以及 catalog 签名、当前授权先于有界SQL。受控 Store/HTTP ports 不冒真实数据库或 Account 权限结果。真实页/安装/重放与撤权/CSRF将在既有真实 fixture 上验证；不得 SQL 种普通 Skill 或用 fake install Backend 代替 Service.Install。

本片必要纯检查已完成分版本补集：pure-01 的8新top及2旧HTTP top（59sub）race通过，Schema因旧夹具遍历每条路径的HEAD而失败，vet未执行；原整体FAIL保留。仅修本包testdata/schema.py的精确路由集合后，pure-02 Schema单top的45向量与30个无body HEAD状态、三包vet通过。两轮输入首尾相同，原Wait与group/runtime双尾闭合；原件为本树 output/ai/skill-install-owner-http/pure-{01,02}/，稳定入口run-{01,02}.py复用已验core-checks方法。未重复已通过10top或安装库/cleanup全矩阵。真实HTTP/PG尚未运行。完整初始化/Agent/F1/Backend 仍按各域真实绑定状态，生产 Project initializer 保持 unbound。
