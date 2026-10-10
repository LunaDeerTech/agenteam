# D27 Project Secret Owner UI

状态：实施中，尚未验收。仅消费 main 已交付的 Secret Owner HTTP；未修改后端、公共 OpenAPI、依赖或迁移。

## 范围与固定合同

依据 [Secret HTTP](d10-secret-variables-owner-http.md)、[Secret Owner](d10-secret-variables-owner.md)、[项目设置布局](../../frontend-design/layouts/project-settings.md)、[设置壳](../../frontend-design/layouts/settings-shell.md)。普通 Human Owner 进入 `/:username/:project_name/settings/secrets`，查看安全列表与详情，创建、修改名称/描述、显式替换值及删除。默认 Project initializer 的既有未绑定边界保持；不创建项目、配置 Agent 授权或实现材料消费。

现有 HTTP 仅三个路径模板：collection GET/POST、item GET/PATCH/DELETE、commands/lookup POST；所有成功均 200。列表 5 MiB，其余输入与成功输出 1 MiB。安全 metadata 恰八字段，值 1–65536 UTF-8 字节、不修补 Unicode/不 trim/不允许 NUL；名称与描述按原 Schema 校验。未知、重复字段和返回的材料字段拒绝。归档项目可读，新增写仅当前 active Owner。

Lookup 仅原 command、target_id、update/delete 原 expected_version；沿原 key/CSRF，不含 value/request/semantic_digest。只 committed/not_observed，后者不证明回滚。无自动重试、原材料重放、轮询或换 key。历史回执不等于当前 metadata，确认后失效旧列表/详情并重读；读取失败不推翻已确认提交。版本冲突保留安全草稿，须完整新 GET 后明确采用版本，重新输入需要替换的值。

## 输入、身份与退出

值只在编辑器本地和一次执行闭包中存活；点击提交即清 DOM/响应式输入，离页、身份/项目变化或销毁也清除。不进入 Session 公开状态、长期意图、日志、storage、URL、摘要或返回展示。JS 字符串及浏览器/GC 副本不可承诺物理擦除。Session 恢复仅保存身份、目标、原版本、key/CSRF 和安全进度，不保存原材料请求。

本域 read/write/lookup 全部经过原 Session 唯一 Human Cookie owner。当前 401/CSRF 失效清身份；局部 403/404 清受保护展示，不设置 System 拒绝。真实身份变化丢弃旧意图；同 Session 临时 checking 保留不确定命令身份并禁止发布。原请求 body/cancel/release 实际 finally 前不释放 busy。取消/传输/解析或取消失败按不确定处理，不视为回滚。

页面发布绑定当前 Session 身份、Project generation/readGeneration 与当前 route。导航确认在原全局 Session restore 前，仅已注册本页才进入新 await；明确放弃不宣称服务端取消。共享 UI 组件接口、原路由拒绝及其他域逻辑保持。

## 文件与方法

新 API `project-secrets.ts`、controller `useProjectSecrets.ts`、页面与 SecretEditor；共享 client/Session/router/Settings/ProjectNav 仅本域闭合接线。普通 Variables donor `60dbcee7` 只参考严格校验与列表结构，未覆盖旧 snapshot 的 shared 文件，也不复用普通值长期缓存或其原失败验收。

定向三 spec 使用真实 API/Session/Workspace 与 Vue Router/页面，只有 Fetch/环境受控：校验合同、输入清除、Unknown identity-only 查证、same-session/真正换身份、当前拒权、原取消尾、归档只读、迟到读抑制、显式冲突采用与导航确认。它们不是实际浏览器或真实 Cookie/PG 证明。后继实际窗口由 root 分配，SPA concurrent-publication STOP 不变。
