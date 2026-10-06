# System 用户目录读口验收记录

结论：**PASS，仅限 [D27 读口 rev2](../work-items/d27-system-user-directory-read.md)的六路径后端结果**。主线程已采纳并提交推送 `3affc0194214101cfa1e6fdc583afa5d60005db8`，实际远端一致。固定最终 input02 SHA-256 为 `004ced3247661feca93ef7899dbc539f9f638a17daa824c30692881f26622c96`；六源与交付 Git 字节匹配。系统设置 UI 与完整 D27 尚未交付。

`GET /api/v1/system/users` 现在以专用平坦九字段 item 返回用户行的 canonical 注册时间；原登录、Session、profile 的八字段 User 保持。users 的显式 HEAD 路由复用当前管理员授权、查询、时间校验与编码，成功和错误均无 body；没有将其他 Account GET 泛化为 HEAD。

## 1. 固定输入与职责

业务基线为 `125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b`。`directory_backend` 负责实现和纯检查，未参与实现的 `recovery_verification` 负责静审、协调执行作者真实用例及独立探针；`recovery_documentation` 只归档已确认结果，没有重跑业务。

| 输入 | 精确范围与保留方式 |
| --- | --- |
| [input01](system-user-directory-read-verification-evidence/inputs/input01.json)，SHA `e14894df898003c8da7bfcaab464f9c65251cc7fe7383e2c77beeb10ab4dd733` | 五路径初版；其余源码取固定 Git 基线。原作者报告及 HEAD405 首红完整保留 |
| [input02](system-user-directory-read-verification-evidence/inputs/input02.json)，SHA `004ced3247661feca93ef7899dbc539f9f638a17daa824c30692881f26622c96` | 六路径最终版；新增授权 `http.go`，修改 OpenAPI 与两项新测试，`http_facade.go` / `http_system.go` 相对 input01 原字节不变 |
| [原件与源码复用索引](system-user-directory-read-verification-evidence/archive-index.json) | 六份最终源加三份不同的历史版本即可重建两版；input01 两份未变源复用最终副本。锁文件及五项既有边界均与原基线和交付 Git 匹配 |

两份原作者报告分别见 [input01 报告](system-user-directory-read-verification-evidence/author/author-report01.md.txt)与 [input02 返修报告](system-user-directory-read-verification-evidence/author/author-report02.md.txt)。最终独立[原报告](system-user-directory-read-verification-evidence/verification/verification-report.md.txt)及[结构化结果](system-user-directory-read-verification-evidence/verification/verification-report.json)保留输入、原命令、结论和限制。

## 2. 实际检查与原始失败

| 检查 | 实际结果 |
| --- | --- |
| input01 作者六条 Go 检查 | account 全范围 unit/race/vet、Account integration 仅编译、Central/Runner 构建均 exit 0；构建产物没有执行 |
| input02 作者五条受影响检查 | Directory/B04HTTP/B04CSRF 筛选 unit/race、account vet、Account integration 仅编译、Central 构建均 exit 0。未变扫描/contract 及 Runner 构建按原证据复用，不称最终六源完整重跑所有包 |
| [author01 原日志](system-user-directory-read-verification-evidence/runs/author01/raw.log) | 旧管理员顶层 PASS 4.14s；新目录顶层 FAIL 3.95s，admin HEAD 实际 405、原断言要求 200。driver exit 1，墙钟 214.048s |
| [author02 修后原日志](system-user-directory-read-verification-evidence/runs/author02/raw.log) | 旧管理员 PASS 4.24s、新目录 PASS 4.19s；两个指定顶层全部通过，driver exit 0，墙钟 82.139s |
| [independent01 原日志](system-user-directory-read-verification-evidence/runs/independent01/raw.log) | `TestAccountHTTPSystemUserDirectoryIndependent` PASS 3.36s；driver exit 0，墙钟 52.439s |

作者真实两轮使用同一 selector；独立轮使用单独 overlay 探针。精确 argv/env、开始结束时间和 actual wait 分别在 [author01](system-user-directory-read-verification-evidence/runs/author01/command.json)、[author02](system-user-directory-read-verification-evidence/runs/author02/command.json)、[independent01](system-user-directory-read-verification-evidence/runs/independent01/command.json) 的原命令记录：

```sh
sh scripts/test-security.sh -run '^(TestAccountHTTPSystemUserDirectory|TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority)$'
sh scripts/test-security.sh -run '^TestAccountHTTPSystemUserDirectoryIndependent$'
```

原 [DIR-HEAD-01](system-user-directory-read-verification-evidence/verification/finding-head01.json)确认：Account `httpHandler` 严格匹配显式 method，GET 没有隐式 HEAD。**rev1 规格、独立静审和作者把 serializer 的 HEAD 空 body 能力误当成正式路由支持，这一遗漏由真实首红纠正。** rev2 补 users 的显式 route、精确 query 分类与 OpenAPI HEAD；原 HEAD200 断言保留，其他路由与旧测试断言没有放宽。[rev2 静审](system-user-directory-read-verification-evidence/verification/spec-rev2-review.json)和[最终输入静审](system-user-directory-read-verification-evidence/verification/static-input02-review.json)不覆盖或改写原失败。

原临时 schema checker 还曾错误选中 User 之前的 Session security scheme，造成空切片断言失败；[准备错误记录](system-user-directory-read-verification-evidence/author/logs/schema-static01-tool-failure.txt)与修正后的检查源码/输出均保留。该错误不是产品测试失败。两次 `-run '^$'` 及真实 driver 中其他包的 `[no tests to run]` 仅表示编译或无匹配用例，不计动态通过。

## 3. 已验证行为与环境

作者真实目录用例覆盖初始化管理员和正式邀请兑换用户的 DB 注册时间、资料更新不改变注册时间、同时间的稳定分页及合法 limit 变化；补验 GET/HEAD 的合法 cursor、非法 limit、篡改/跨资源 cursor、普通用户/匿名/真实撤销 Session，以及自有数据库 year10000 fixture 下安全 503、零候选与空 HEAD。fixture 随后恢复。

独立[原探针](system-user-directory-read-verification-evidence/verification/directory_independent_test.go.txt)另建真实身份，验证新九字段与登录/Session/profile 旧八字段隔离、数据库精确 canonical 时间、HEAD 编码长度、合法 cursor 变 limit、非法 query 同状态、精确 `Allow: GET, HEAD`、其他列表 HEAD405，以及当前权限和已撤销 Cookie。探针 SHA-256 为 `92d56052930c0624fa58b2dc9e46466f69ba2e4eabfd6d9ec482dc73cd9b9a3d`，仅由 Go overlay 加载，没有写入仓库产品测试路径。

受控 rows 单元测试覆盖首行、中间行和 limit+1 的 ID/version/role/theme/时间/行错误，失败均零候选；这类替身证据与真实 HTTP/PG 分开记录。`httpRead` 原事务、取消与 Unknown 语义未变，本卡复用已接受实现及证据，没有新增该类探针。

实际环境为 Go1.27.1、readonly/offline 锁依赖、PostgreSQL17.8/pgvector0.8.1，以及既定 source-built MinIO，binary SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。PG16.12 由原 fixture 同时准备，本次选定目录用例没有对其另作验收。原脚本 race/count1/每包6m及 fixture 预算保持；环境获取/构建与固定镜像的小型原件可从归档索引核查，没有归档二进制或缓存。

## 4. 资源终局

| 运行 | 实际观察及双清理 UTC |
| --- | --- |
| author01 | 7 资源、357 PID/starttime；04:20:22.539836 与 04:20:23.184644 |
| author02 | 7 资源、95 PID/starttime；04:27:53.746768 与 04:27:54.416375 |
| independent01 | 7 资源、74 PID/starttime；04:28:53.567403 与 04:28:54.226200 |

三轮日期均为 2026-10-06，每轮实际资源为四容器/三网络。原命令均已实际 wait；[首轮](system-user-directory-read-verification-evidence/runs/author01/cleanup.json)、[修后轮](system-user-directory-read-verification-evidence/runs/author02/cleanup.json)、[独立轮](system-user-directory-read-verification-evidence/runs/independent01/cleanup.json)均记录全部 exact ID 两次 absent、所属进程为空、runtime 为空、monitor 错误为零。各轮基线也与[最初两容器/四网络记录](system-user-directory-read-verification-evidence/verification/docker-baseline.json)逐项匹配，ID/name/labels 不变；产品与对应验证输入末检不变，窗口已归还。

## 5. 归档检查与保持的边界

[归档说明](system-user-directory-read-verification-evidence/README.md)与只读脚本保存并核对 118 项逻辑原件、103 个去重后的原件文件，原件约 620KB。离线复核已检查两版输入、六个交付源、锁与五项边界、11 条作者检查、三轮原日志中的指定顶层及资源双清记录；只读取本地证据与固定 Git blob，不执行保存的 driver、Go、数据库、网络或 Docker 命令。

本结果满足系统用户目录 UI 的后端前置，UI 实施、浏览器、邀请投递补口及完整 D27 仍有独立门槛。Summary 待决、生产未绑定、ready503、完整 D08–D28/E01 未完成及 E01 未开始保持；Object 原修复和 tools 原独立任务没有恢复、改派或重建。本次文档同步不扩大以上产品接受范围。
