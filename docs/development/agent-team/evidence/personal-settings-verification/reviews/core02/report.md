# 个人设置 core02 独立差量复核

**PASS，仅关闭 core01 的 F1/F2 核心阻断。** 固定六文件 SHA/size、相对 core01 仅 useSession 与 state pure 两文件的逐字节 diff 已核；没有新增确定阻断。未读活动 UI/草稿 owner，未运行 npm/Go/browser/Docker；不表示个人设置整体或真实 Cookie/数据库场景已验收。

基线 `9a710f272026b41ef69852bbeb41cb7670b500a8`；作者 `core-review-02/manifest.json` SHA `84de3f8bc1a18a81377cb469e9e3f18e355e17c0187851085aaf70decbc4ac24`，delta SHA `dbf5b90e68f54367e4a9dffefa25de339c0ea0b9261bac8b1d912507d8a2d5b3`。前次报告 `/workspace/agenteam-personal-settings-core01-v-8we83jg_/report.md` SHA `dedf5cbc8fa626dea2d53d3ac35295d3a211aab13ba8ce69c18524ac44048cc2` 保留原静态发现，不追称为本实例动态复现。

## 修复判断

- **F1 关闭。** 固定 useSession.ts:490–495 在生成 key / 调用 logout 前检查 `passwordSessionCheck`。原 Unknown 实际尾部释放 owner 后，旧 logout 仍被屏障挡住；成功 publish 当前 Session 才清屏障（205–245）。新增 state pure:140–151 明确等待 changePassword 拒绝、断言 busy=false，再验证 logout 零调用/零新 key，且 GET 仍仅初始化那一次。它可以区别原问题，而非借用 owner busy 前提。
- **F2 关闭。** 私有 PersonalCommand:92 新增 unsettled；unknown:689–693 及 catch:737–741 保留历史未知，普通后续拒绝不再清原意图。publish 同身份只更新 checked，不清 unsettled；新命令起始为 false，严格成功/真正身份作废/明确 abandon 仍沿既有路径清理。新增 pure:101–138 使用真实 File 对象的公开 controller 调用，按 Unknown → 同 Session GET → 503/not_started → 新提交 busy/零 key → 再 GET/原请求成功顺序，核三次实际 mock 调用均保留同一 File/version/media/key/CSRF。没有把 GET 当 receipt 或改写为自动新 key。

## 作者原红与修后证据

六份 JSON 的实际 argv/env/input/exit 及 raw 日志 SHA 均独立核对，定位见 index.json；本实例未重复运行。

| 作者记录 | 实际结果与适用范围 |
| --- | --- |
| core-f1-red-01 | core01 生产；1 failed / 12 skipped，实际完成后 logout 调用数为1。原两源 SHA 与记录相同。 |
| core-f1-fix-01 | 只加 logout 屏障的中间生产；1 passed / 12 skipped。断言从 not.toHaveBeenCalled 换为 calls.length=0，语义等价，避免合成值打印；原 red 不覆盖。 |
| core-f2-red-01 | 已修 F1、尚未修 F2；1 failed / 13 skipped，后续新提交未被 busy 阻止。原两源 SHA 与记录相同。 |
| core-f2-fix-01 | sticky unsettled 中间生产；同一 F2 测试字节，1 passed / 13 skipped。 |
| core-only-unit-04 | 最终六个 core 输入逐 SHA 相同；三个测试文件 39 passed，无 skipped，包含上述两新增反例及原核心回归。 |
| core-type-03 | exit0、核心输入相同；该次也包含当时 UI 准备输入，只记录 type 结果，不宣称 UI 经本次静审或动态验收。 |

F1 fix 测试由固定 F2-red 测试移除新增 F2 case 后，逐字节恢复到记录 SHA `148eb816878ffb60d719f74f7aedaeec2b332ba22cb5de9a99d91c78ac4bf43d`；其生产直接等于固定 F2-red 的 `81e72685…`。

F2 fix 中间生产是作者**事后精确重建，非当时原样备份**：`core-f2-fix-reconstructed/useSession.ts` 已独立核 SHA `6c289626b8fdd6a00b5f57fd9d7c785e37b8ee9b9f6d45640cc0eaeae7a2651b`，等于实际 run input；其与最终 core02 的实际 diff 仅 logout 换行及 `} else` 换行两处格式。重建说明 SHA `b49f2e0c657be1887badeb45bf9bf4179b7b9dde7c0c1bd965a338987bff03a5`。最终 state pure 对 F2 原 red/fix 测试仅排版变化，原业务条件/预算没有降低。版本链已闭合，不把单测模拟的 COMMIT_UNKNOWN 称为真实底层 COMMIT 故障。

结论可供核心继续集成；作者仍须完成冻结 UI/owner/路由组合、原规格真实四组和认证回归及适用工程检查。下一独立阶段等待对应固定输入，不扩重复验证。本次仅写私有报告/index，无仓库/业务/Git 写、无委派、无活动命令或自有资源；all-stop。
