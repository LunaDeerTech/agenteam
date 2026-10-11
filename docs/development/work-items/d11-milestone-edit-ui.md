# D11 Milestone 标题与描述编辑 UI

2026-10-11，有限独立接受。已有 initialized Project 的当前 Human Owner 可在任务页面选择真实 Milestone，编辑标题、描述并明确保存。产品源码固定 `3a95da49`，浏览器方法最终固定 `e34d643f`；非作者已独审源码、方法、原始业务与资源证据及五张截图，未将作者执行写成非作者本人重跑。

## 用户行为与恢复边界

未触达字段省略，描述空字符串明确清空，null 拒绝。标题保留原字节，不 trim/NFC；标题与描述校验沿 [Work Structure](d11-work-structure.md)，页面与 Session 边界沿[规划 UI 卡](d11-work-owner-planning-ui.md)。表单使用已有带标签的输入控件，取消恢复编辑按钮焦点，成功读取后聚焦当前 Milestone 标题。

复用 `PATCH /api/v1/projects/{P}/milestones/{M}` 的 `{expected_version,request}`，以及 `POST /api/v1/projects/{P}/structure-commands/lookup` 的 `{command:"work.milestone.update",target_id,expected_version,request}`。唯一 Session owner 在发送前捕获原 Project/target、字符串版本、key 和完整字段 presence；Lookup 严格使用 Structure 的 `state/result`。changed 与 no-op 分别校验版本、事件和原触达字段；不新增后端、迁移或通用框架。

明确拒绝保留草稿；版本冲突须显式读取当前详情，再明确采用读取到的版本，之后由用户重新保存。Unknown 沿已交 create/start 切片仅查证原命令，not_observed/in_progress 保持待决，不自动重发、改 key/版本或提供新重放入口。创建与编辑在原 Session owner 内互斥；确认放弃导航只释放明确 rejected，不清在途或 Unknown。

保存回执与当前事实分开：成功或 Lookup 确认后真实 Get/List，读回失败只恢复读取。普通刷新对绑定 Milestone 的明确拒绝、真实 Workspace 对同 Project 的 Owner 拒绝都会清本片私有材料；503、同 Session checking 及无关 Milestone/Project 拒绝保留原 Unknown。真实身份/CSRF 改变清理私有材料，旧响应不能发布到新选择。

## 已执行检查与原失败

作者受控检查分轮记录，不相加成唯一测试总数：`client-session01` 新旧客户端/Session 4 文件 14 PASS；`page01` 实际 3 文件 11 PASS；`regression01` 8 文件 50 PASS。页面控使用 typed ports，不作真实 Session 或数据库证据。

独审发现两项恢复缺口后，`recovery01` 通过真实 Session + Workspace + Tasks 控制器和受控 fetch 复现 4 FAIL / 3 PASS；`recovery02` 窄修后为 6 PASS / 1 FAIL，剩余创建 fixture 回执误用固定 ID，已改用原请求生成 ID。`recovery03` 实际 4 文件 23 PASS；`recovery04` 正确选择 9 文件 37 PASS，包含 10 项真实绑定恢复控、same-Session restore/Workspace 重新授权、明确拒绝清理、503/无关目标保留及两种放弃导航后的下一命令。`type02` 与 `build01` 通过，生产构建生成 74 个资产文件。两项产品修复已独立接受。

原件在 `output/ai/milestone-edit-ui/checks/`，每轮 result.json 保存精确 argv、实际退出与范围。page01/recovery03 命令内未匹配的文件名不计为通过。离线 `browser-profile-controls01` 因 config grep 未命中失败；02/03 的独立无 page 控制通过，03 直接检验实际 Lookup 判断，拒绝 Milestone 的 status-only/非 committed 和默认接口的 state-only/非 committed，不使用两协议兜底。两个原 launcher 的旧 profile/TARGETS 保持，新增 selector 的正控与 6 个反控通过；最终 list03 两模式各恰 1 项。

## 真实浏览器限定交付

沿原真实 Account、显式初始化 fixture、默认 App/PG/正式 HTTP 准备既有 Milestone；浏览器不创建目标。`TestMilestoneEditWeb` 保持两个子例：normal 精确三次 PATCH，依次修改标题/描述、仅触达同值标题得到 no-op、仅以空描述清空；每步当前 Get/List 与用户显式刷新均按原字段核对。lost 仅一次 PATCH，在原完整 TLS EOF/Close、handler 返回和 SQL 原提交确认后截断回执；唯一 Lookup 保留原 key/target/version/完整 patch，随后读取真实当前内容，不再次 PATCH。

数据库后验核对原 command、semantic digest、request、plan 前后像、no-op、Outbox 和 canonical 事实；Project/current Sprint、其他工作事实、排序 generation 及既有 Audit 摘要不变。保存回执不代替当前 Get/List，fixture 初始化不冒生产 initializer。

`native-milestone01` 原 wholeFAIL 保留：normal PASS，lost 已到原 Lookup/当前回读，但共享观察器末端误读 `typed.status` 而失败；原工具 33127 actual1，输入未变且完整资源尾退出。修复仅观察器的 Milestone Update opt-in 判断和专属正反控，默认接口仍只认 status；产品、Go、构建及 launcher 未改，复用原编译二进制。

`native-milestone02` 原 wholePASS：工具 5311 actual0，supervisor 185665 实际退出 0/162.252 秒，原日志 158.668 秒；normal 25.36 秒、lost 19.12 秒。Go、两 Node、driver 与 8 个 Chrome 原 Wait 均齐；7 个精确资源的两轮 absence、private/runtime/desc 与 HOST_TCP 双尾全部闭合，2489 输入前后完全一致。正常 30 请求全部 finished，28 份联合证明；lost 17 请求为 15 finished、1 次声明 PATCH 截断及 1 个 GET 200 ERR_ABORTED。该 GET 的原 350 字节完整消费、同 request ID、typed/public/current 与 Go 原正文由联合证明核对；lost 共 14 份联合证明，不写成全 finished。

五图已逐张独立查看，接受范围为 1440 浅色编辑/Unknown 提示与 390 深色清空/当前/恢复；真实正常链同时验证键盘取消回焦、保存焦点、减少动效和无横向溢出。证据位于 `output/ai/milestone-edit-ui/native-milestone0{1,2}/`，02 原 supervisor 日志 `/tmp/mew02/ui-27a4cb08d4d84757.log`。重建使用专属 `milestone-edit.config.js`、原 driver/supervisor 的 `^TestMilestoneEditWeb$` 与 `AGENTEAM_MILESTONE_EDIT_WEB_DIST/EVIDENCE`，固定输入和资源所有权后运行，不将 list 或编译当作浏览器通过。

本片不增加 Sprint 编辑、排序、删除、完整权限/撤权浏览器矩阵或完整 Work 功能。生产 initializer、完整 F1/participant、ready=false/503 及 Object Runtime join、OpenAI tools、Central SPA concurrent publication、Jina/Image STOP 不变；完整 D11/D27 与 E01 未完成。
