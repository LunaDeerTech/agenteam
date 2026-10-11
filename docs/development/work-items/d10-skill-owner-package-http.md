# 当前 Owner 固定 revision 技能包 HTTP

状态：后端有限结果已验证；[详情下载 UI 后继](d10-skill-package-ui.md)的正常浏览器链也已有限独立接受。本有限后继消费[Skills 正式设计 §5](d10-skills-initialization-design.md#5-失败读流与项目清理)中的 `OwnerReader.OpenPackage` 和现有真实 Skill/Object 服务，为已有 initialized Project 的当前 Human Owner 提供原包读取。公开字段以[独立 OpenAPI](../../../api/openapi/skill-package.json)为准。

## 固定版本与响应

`GET/HEAD /api/v1/projects/{project_id}/skills/{skill_id}/revisions/{revision}/package`，ID 为规范 UUIDv7，revision 为规范正 int64 十进制，从既有 Skill metadata 的 `current_revision` 或已知历史版本选择；一次请求不隐式跟随最新版本。每次沿当前 Account/Session、Project Owner 与原 `OpenPackage` 权限，不从 metadata 或历史安装 receipt 取得读取授权。

成功为 200、`application/zip`、`Content-Disposition: attachment; filename="skill-package.zip"`、`Cache-Control: no-store`、`X-Content-Type-Options: nosniff` 与准确 `Content-Length`。GET 返回完整原 canonical `skill-zip-v1` 字节；HEAD 执行同一权限、读取、完整性与关闭流程，保表示头而不发正文。无 query（含裸问号）、请求 body、Content-Encoding、Range 或 If-Range；不支持部分表示或条件 304。

原包上限沿 canonical `MaxArchiveBytes=33832982`；不能只按响应 Content-Length 信任数据。adapter 先验证正式 RevisionMetadata 与目标 Project/Skill/revision，再有界完整读取，核实际大小、原 SHA256、canonical ZIP 编码、manifest 与入口名称/描述；任何矛盾均不发布包或部分成功。不投影 ObjectID、对象地址、租约、内部 cause 或任意文件路径，固定附件名不使用未信任标题。

## 生命周期与既有边界

所有动作继承相邻读取 adapter 的同一 2s 总预算及更早 parent deadline，涵盖 Account、原服务/SQL、Object 读取、canonical 校验、原 reader Close、HTTP 写入/Flush 与取消 callback 实际 join。成功头必须晚于原 reader `Close` 返回和本地 `Joined`；Close/持久释放 Unknown 保留原错误，不改成成功。原服务继续负责实际读取取消及 work/lease 责任，HTTP 不制造第二个授权、reader registry 或修复队列。本地 Joined 不证明 D05 持久 lease 已释放。

默认 App 已通过 `skillhttp.HandlesPath` 分派同一 Skill Service，本片只扩展该包的结构路由，不改 Knowledge 生产代码或重复创建服务。生产 Project initializer 仍 unbound，Object Runtime join、OpenAI tools、SPA concurrent-publication、Jina/Image 等原 STOP 保持；不赋予 Agent/Execution/Runner 权限，不开启安装更新、分配或包内脚本执行。

## 验证与接续

基础 race 检查覆盖路由/HEAD、安全错误、当前身份入口、原版本参数、大小/摘要/manifest/ZIP矛盾、原 Close 失败、Unknown 与 held Close 取消。独审提出的原 Skill reader 取消竞态已用实际 `Service.OpenPackage` 与既有私有 state getter 的 barrier 复现：原取消 callback 尚未完成记账时，公开 `Close` 提前返回 nil。最小修复等待同一 callback 实际退出，保留原记账错误；修后 reader 与 HTTP 受影响 race 检查通过，活跃 Read 和持久 lease 仍按原规则判断完成。该局部控制使用受控 Object，不替代真实持久取消故障验收。

真实 Account/PG/MinIO 的 `TestSkillOwnerPackageHTTPPublication`（builtin/installed）及 `TestSkillOwnerPackageHTTPCurrentAuthority` 在修后源码通过：正式 Human 安装 `text_files` → metadata 固定 revision → GET/HEAD 原 ZIP，核大小/摘要/manifest/文件字节、当前 Owner/Session 拒绝、reader work/lease 与未改变的持久业务事实。测试使用真实 `Project.CreateProject` 和显式测试专用 initializer 发布目标 Project；复用的旧 Account 基础 fixture 日志中的 controlled Object/SQL Project 指其另一个准备对象，不是本次目标链。

默认 App 正常链在修复前源码通过 `TestSkillPackageHTTPDefaultRootComposition`（builtin/installed）：测试进程内运行默认 `run`、真实 listener、正式登录/安装/metadata/GET/HEAD，并沿 signal channel 的 SIGTERM 收尾。它不是外部 Central 子进程的 OS 信号测试。PG 与 App 原 test/driver Wait、七资源双 absence、descendant/private/runtime/TCP 双尾及输入未变检查均有实际成功终态；这些是明确版本的组合证据，不声称一次当前 HEAD 全矩阵通过。

原 PG01 业务通过，但共享主机外部代理 TIME_WAIT 导致 whole FAIL，原件保留；reader 修复后的 PG02 whole PASS 同时补足新源码验证。本后端矩阵未覆盖真实网络取消/持久释放 Unknown、Owner 撤权竞争或浏览器下载；后继 [UI 正常下载链](d10-skill-package-ui.md)另以真实默认 App 与实际保存文件完成有限验收，不补称上述取消与竞争场景通过。精确命令、版本与原始终态位置见[本树 current](../../../.agent-state/current.md)。
