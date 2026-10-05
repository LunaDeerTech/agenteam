# 个人设置 UI02 独立窄复核

**UI-F1 已关闭；UI02 总结论仍 BLOCKED：新增 UI-F2 身份归属静态缺口。** 原独立反例在全新固定输入一次 PASS；新问题没有新增动态运行，不将两者混称。其余 UI01 有界审查结论复用，不扩审活动 fixture/真实浏览器。

输入 `ui-review-02/manifest.json` SHA `47663d34814a455131565791a17efe122c20d3dd8478f936e4ab6f5acf9b7f3a`；delta SHA `8cb3eb8f0c735ba9f033c797a0ccccb21a4a410285ee64eac62043c9ac8ae8b0`。19文件SHA/size匹配，相对UI01精确只有usePersonalSettings.ts与personal-settings.spec.ts改变，逐字节diff与delta相同；三个core保持core02。下述行号均指本固定sources。

## UI-F1 关闭：原探针单次修后通过

load(password)不再直接丢弃已有确认，finally重投影readonly passwordProgress；后续普通Profile读错附加独立错误而不降级改密确认、不重发POST。作者保留原红1 failed/11 skipped，修后受影响31例及type通过；最终31/type的19输入逐SHA匹配。新增原测试还覆盖确认后叶切换回来遇Profile GET503，确认事实与读错误同时可见。format记录是两授权文件的prettier --write，不另冒称完整web-check已重跑。

独立同一probe SHA `8ae42c600f61fe4c35bd87755b93cc824f6c95152c706b8727a16bf8dd9dfe08` 原字节未改，原红目录不动；新私有63文件闭包只更新候选两文件及私有cache路径。实际命令：

```
node node_modules/vitest/vitest.mjs run src/tests/independent-settings-password-feedback.spec.ts --reporter=verbose
```

Node v24.19.0 / Vitest4.1.11 / jsdom，原默认5秒、无retry；**1 top/1 case PASS，exit0，1.45s，case129ms**。严格200→Session GET503→显式检查成功的新Session→空密码字段/POST1/confirmed事实及页面反馈全部通过。

本目录 input.json SHA `d760821edd0653638b93d90cc000ec160d64d31c7dc24c78e11ce06594a49f28`；原日志 `pure-probe-fixed-01.log` SHA `2040069d2be1fe9e6bc629df3a4f8897e6d5939c5422f7f772a176165ef58a20`。这是前端正式typed API mock与真实候选Vue组合，不是服务端COMMIT/Cookie或真实浏览器证明。

## UI-F2 阻断：新 helper 按 userID 复活被身份代际清掉的旧反馈

新增 `showPasswordProgress:577–593` 和 load:262–264 只比较 `progress.identity.userID`。每次密码叶读取后都会重新投影该progress（289）。当前核心 `useSession.restore:364–398` 的临时clearIdentity(false)保留progress；`publish:205–245` 遇新Session/CSRF会新建identity epoch，但并不清旧progress。

可区别的合法序列：原Session **A** 改密确认并换发 **B**，progress仍携原A identity（core:851–856）；随后同用户经其它正常登录换到真正的新Session **C**，当前页调用restore看到C。owner的 `stopIdentity:545–564` 发现B→C，因旧progress=A而不符合完整same，本来正确resetOwner清B代反馈；然而新挂载密码叶或一次load又按相同userID，从核心仍存在的A progress重新显示旧成功反馈。card要求“新用户/Session/context清上一身份draft/反馈”。这不是本次合法A→B换发的保留，也不是同B的checking重验。

UI01原watch仅在progress改变时投影；UI02新增每次load重投影让上述清理被绕过。该结论是固定源码链的静态判断，本实例没有编写/运行新增C场景，原授权单次probe只覆盖UI-F1。

最小建议：把安全确认反馈归属限定到本次改密原identity及其一次合法换发目标，允许同一目标identity的checking重验/叶切换，真正后续epoch变化退休旧投影；不能仅以userID判断。可在现有私有owner中记录绑定，不要求新增公开契约。需补 A→B合法保留、随后独立C清理的精确纯反例；不改变原成功/错误断言或重新发送改密。

## 证据与停止边界

作者五个结果JSON及原log逐SHA核对并索引：原red、初fix31、format、终版31、type。原UI01的108web-check只归属原版；本轮没有机械重跑它或作者31。后续真实fixture/browser仍未验，不能称完整个人设置/D26通过。

末核63文件/probe不变，精确delta匹配，原红input不变；实际私有进程扫描为空，没有Docker/browser/服务资源。只写私有报告/证据，未改作者源/旧测试/仓库/Git，无再委派；all-stop，交root决定UI-F2窄修。
