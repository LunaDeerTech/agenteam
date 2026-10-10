# Human Owner Skill 安装与发现

本片提供当前 Project Owner 的普通文本包安装、原命令 Lookup 和分页目录。契约见 [Skill Owner OpenAPI](../../api/openapi/skill-owner.json)，交付范围见 [工作卡](../../docs/development/work-items/d10-skills-owner-read-http.md)。不依赖任务 current 或 ignored 日志才能理解及运行本入口。

## 接口与组合边界

- `POST /api/v1/projects/{project_id}/skills`：body 为 `{"request":{"skill_id":"UUIDv7","mode":"create","source":{"kind":"text_files","files":[{"path":"SKILL.md","utf8_text":"..."}]}}}`。总请求最多 1 MiB，SKILL.md/UTF-8/LF 等包规则由正式构造器核验；`Idempotency-Key` 标识原命令。
- `POST .../skills/commands/lookup`：同 key、完整原 body 只读原安装命令；Unknown 不自动重发，NotFound 不证明回滚。
- `GET/HEAD .../skills/catalog?limit=25&cursor=...`：protected 与 ordinary Skill 的安全八字段、可选 next_cursor，最多 100 项。每页独立当前授权，不承诺跨页快照。
- 原 `GET/HEAD .../skills` 仍只返回已发布 builtin，原详情读保持。Account Session/Origin/CSRF、当前 Owner/Project gate 与同实例 root 分派不变；HTTP 不接 Actor、宿主路径、URL、Object 或 Runner 标识。

正常安装/Lookup/catalog、拒权及会话撤销经真实 Account、Project、Skill、Object、PostgreSQL/MinIO 联调。HTTP 使用 ResponseRecorder；默认 app 路由有定向纯控，未声称新增 app 进程/TCP/浏览器验收。测试目标 Project 用显式真实 Skill initializer fixture 创建，生产 Project initializer 仍 unbound。AgentRun 安装明确 DependencyUnbound；本片不启用 F1、可执行 Install Backend、Agent assignment 或旧 Object STOP。

## 复现

两个已交付测试源为 [domain](../../tests/projectvariable/skill_installation_test.go) 和 [HTTP](../../tests/projectvariable/skill_installation_http_test.go)。先在任务私有配置内关闭 Go telemetry 并去除三个旁路变量，使用固定 Go 1.27.1、离线只读 modules、独占可再生 GOCACHE，编译 `./tests/projectvariable` 的 integration/race 候选。实际编译命令为：

```sh
"$AGENTEAM_GO" test -tags=integration -race -c -o "$CANDIDATE" ./tests/projectvariable
"$CANDIDATE" -test.list '^TestSkillInstallation(PersistentObject|OwnerHTTP)$'
"$CANDIDATE" -test.run '^TestSkillInstallationPackageInput$' -test.count=1 -test.timeout=60s -test.v
```

列举必须恰两 top，素材检查必须实际通过，再进入真实 PG。不要以纯控或列举替代测试。真实运行使用已有 supervisor 与 driver，无第二监督器：

```sh
python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver .agent-state/work-owner-http/root_chain_driver.py \
  --binary "$CANDIDATE" \
  --run '^TestSkillInstallation(PersistentObject|OwnerHTTP)$' \
  --output "$OUTPUT" --root-chain
```

`CANDIDATE` 与 `OUTPUT` 必须为绝对路径，输出目录全新。启动同进程先核可用磁盘至少 5 GiB、原任务资源/私有目录已退役，使用新空 Docker config；保留原进程实际 Wait。driver 固定 MinIO 普通任务私有文件为 `output/ai/deps-minio/bin/minio`，SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。原 Go 6m、root 540+60+3s、七资源、输入初末重枚举及 private/runtime/desc/HOST_TCP 双尾保持。

也支持单独 `^TestSkillInstallationPersistentObject$`（1 top / 2 sub）或 `^TestSkillInstallationOwnerHTTP$`（1 top / 2 sub）；组合为 2 top / 4 sub，各自真实清理后串行进入下一项。所有模式都要求精确 selector 和 root-chain。旧 metadata 及主线其他模式保留；本交付未包含 Agent schema fixture，因此不登记该未交付 profile。

入口仅复用通用 required-input 与 expected-test-set。交付收敛后 metadata 6、Installer 4、HTTP 4 个离线方法控全部通过：剥离有限 profile 差异后逐字回到 main 的旧 shared 入口，完整 case/Wait、缺源/软链/新输入、资源/private/runtime 残留拒绝保持。外部观测在这些纯控中是受控替身，不代真实 PG。

## 实际结果及限制

| 范围 | 有效结果 |
| --- | --- |
| 安装库基础 | pure-03：10 top / 27 sub race 与本包 vet 通过；原失败不回填。 |
| HTTP 基础 | 8 新 top＋2 旧读 top（59 sub）race 通过；修正 Schema 夹具路由后，45 Schema 向量、30 HEAD 状态与三包 vet 通过。 |
| 连续迁移 | 32–35 与 schema02 已验来源 `e028467b` 一致；36 经安装/HTTP 真链执行。只交付必要 DDL，不外推 Agent/Registry/Mount 服务已完成。 |
| domain 真链 | combined02：安装/读取/原 key 重放及普通清理 2 sub 通过（top 8.37s）；真实包 EOF/Close/Joined、Object 删除、SQL 锚点和审计核验通过。该轮 HTTP 拒绝夹具仍失败，wholeFAIL 保留。 |
| HTTP 真链 | source `f020d2f7`，HTTP03：1 top / 2 sub 全通过（13.35s，正常 0.65s、当前 Owner/CSRF 1.19s）；真实 Logout→401/清 Cookie/安全 Problem/零新增事实成立。 |

HTTP03 为 2026-10-10 14:04:42–14:06:28 UTC，原 Go/driver/supervisor/outer Wait 均 0；七 ID 的 14 次 absence、private/runtime/desc/HOST_TCP 双尾、无 adopted/survivor 与 1227 输入首末一致全部闭合。候选 47,985,269 bytes，SHA256 `a05913b60e94e830db675f7773705fce2b82d8b77cd8ac909b84b96e534fd27d`。该候选/运行输入属于作者来源，交付树裁掉未交付 profile 后仅做必要离线入口兼容控，不冒作一次新的真实运行。

历史失败仍为事实：最初 domain 请求名含空格、外人错误码期待及 CRLF 素材分别按正式合同修复；combined01 的只读审计查询错写 action，改用正式 `ObjectUploadComplete` 常量；combined02 的 Problem.instance 期待原路径，改为正式固定 `/api/v1`。HTTP03 只验证修后版本，未改产品或放宽断言。历史过程保留在作者 topic 的 [原恢复说明](https://github.com/LunaDeerTech/agenteam/blob/f020d2f75cc755d49b553d6065a7df06797ca3e8/.agent-state/skill-install-owner-http/README.md)，不将大日志或旧 current 复制到本交付。
