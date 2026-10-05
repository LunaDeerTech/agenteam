# D26 个人设置 rev1 独立规格静审

结论：**rev1 待一处窄修（F1）；其余本轮范围未发现规格阻断。** 这是固定来源的静态设计审查，不是业务验收或实施授权。认证前端尚未最终通过；必须先固定其最终验收提交，再完成卡 §2 对真实导出、Cookie 队列完整生命周期、主题接缝、guard、fixture helper、后端差量及 21 路径所有权的独立核验。

固定输入：原卡 `/workspace/agenteam-personal-settings-card-78yf7ay1/rev1.md`，SHA-256 `0243bd0ce7a1f334dc5601ec40c8b07728f00d7797cc14bfa21f166bda5ef2e9`；后端与设计提交 `ccf498d61152178c5b44994d4b9e8b8f4eb6813b`。本目录 `rev1.md` 保留原字节，`index.json` 记录相关固定 Git 输入。未读取活动认证 UI/core；审查期间的卡修改不计入本结论。

## F1：头像下载最小长度单位须明确

卡第 88 行要求“1–5MiB Content-Length”，容易落实为最小 1 MiB，误拒正常小头像。固定 `internal/central/account/http_profile.go:235` 的合法范围为 byte size > 0 且 ≤ `5 << 20`，第 260 行按字节输出 Content-Length；OpenAPI `/api/v1/me/avatar` GET 200 的 Content-Length schema `minimum=1` 同样是字节。固定 `tests/account/http_avatar_test.go:23` 的 40×24 正常头像及第 45–65 行真实上传/重编码/下载断言也不以 1 MiB 为下界（本次仅读源码，没有重跑）。

最小修订：明确“1 byte ≤ Content-Length ≤ 5 × 1024 × 1024 bytes”，验收增加小于 1 MiB 的合法头像成功例，继续要求声明/实际长度一致、超限和截断失败。主线程已采纳该窄修，原 rev1 保留；最终规格通过须对新冻结卡核差量。本项是规格歧义，未声称已复现前端产品故障。

## 其余有界审查结论

- 八 typed 调用的 method/path/body/成功状态与固定 Account HTTP/OpenAPI 一致；raw File + quoted If-Match、DELETE 204 无 JSON、完整 User/metadata、int64 字符串、Problem 提交状态保真均有明确边界，无后端 API 或迁移缺口。
- `http.go:175–178` 的失效响应可清 Session Cookie，故所有新增读写（含头像 GET）加入原同一队列是必要约束。卡没有把 abort 当终局，也没有用图片 URL、第二 store 或第二队列绕开它。真正承接方式仍受 §2 限制。
- Profile/偏好/头像共享 User version。固定 profile mutation 先当前 Session 授权，再查幂等；成功/重放可返回当前 Profile。卡保留原 expectedVersion 和完整意图、不自动重基，不把重放响应当原版本历史快照；DELETE 后读失败不反写已确认命令失败，均与后端相容。
- 改密最终 Tx 撤销旧 Sessions、插入新 Session、推进 User version 并清初始密码建议；200 只给 completed/next_path，Cookie 归响应所有、没有新 CSRF。卡要求紧接同一协调器内 GET Session（同 User、新 Session ID、完整 CSRF）后再开放写，明确 200 后读失败不撤销已确认命令；未知时不将当前身份当 receipt、不把原密码/key 移到新 Session、不假造 lookup。这些与 `password_change.go`、`http_auth.go` 一致。
- 主题 saved/draft 分离，取消恢复同身份最新已确认值，迟到 Session 不覆盖 dirty preview；dirty 路由/back/logout 在动作前确认、在途责任继续由原协调器承担。必要 useTheme 接缝列入实施前核验，没有假定未验认证实现已具备。
- 三叶子、两级设置框架、字段和头像独立保存、username 旧链接提示、允许空/空格 display name、密码原文/输入保真、Blob 生命周期与就近反馈与固定产品设计相符。真实验收要求正常普通用户/管理员、本域持久后态、新旧 Session/CSRF、主题 preview 零写及无溢出/焦点；未用 DOM 文案替代后端事实。

## 限制与可复查方式

仅只读 Git/文件和生成本私有报告；没有 Go/npm/browser/Docker/网络运行，没有仓库或 Git 写入，没有新增委派。21 路径仅候选范围；认证实际导出/helper 未验，不能承诺无需规格差量即可实施。生产 SPA 托管、完整 D26/D27、D25、Project/Artifact/Object 既有阻断保持；不恢复暂停的方法。

可复查命令：`sha256sum /workspace/agenteam-personal-settings-card-78yf7ay1/rev1.md`；在 `/workspace/agenteam` 对 `index.json` 每项执行 `git show '<commit>:<path>'` 后计算 SHA-256；针对 F1 读取固定 `http_profile.go` 第 233–263 行及 OpenAPI 上述 GET 响应 schema。只需核未来 rev1.1 的明确单位/小图成功例差量，不需因此重开全卡或运行测试。
