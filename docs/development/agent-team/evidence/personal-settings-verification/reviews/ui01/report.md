# 个人设置 UI01 独立静审与定向纯验证

**BLOCKED：UI-F1 已独立纯测试复现，待作者窄修。** 已完成本轮固定生产/测试及作者证据的有界复核，除此未发现新的确定阻断。core02 的 F1/F2 修复不受本项影响。本结论不外推真实后端 COMMIT、Cookie/CSRF、视觉/浏览器或完整个人设置验收。

固定业务基线 `9a710f272026b41ef69852bbeb41cb7670b500a8`；作者 `ui-review-01/manifest.json` SHA `8f5e5d95ec655b805f34bb614f39b78c335fcb6cf0f6d3014058fceca70dd890`、delta SHA `ca94a1d24d6e4f26713eb62d50703f0d2bdee184deaaea2b0f7a8356207d6dce`。19 web 原字节 SHA/size 全匹配，client/account/useSession 三生产逐字同已验 core02。未读其余活动五 fixture。

## UI-F1：确认新 Session 后，自动读取抹掉已确认改密反馈

公开序列：在真实候选 App/Router/页面与 controller 组合填写密码 → 模拟正式 typed changePassword 严格成功 → 紧接 Session GET 返回合法 DependencyUnavailable/not_started → 页面进入 unavailable，密码表单已卸载，原 POST 确认事实仍在 → 用户点击现有“检查当前会话” → GET 返回同用户的新 Session，重新挂载密码页。

独立反例实际已通过：POST 始终1次；第一次失败后 core progress=session-unconfirmed；再次检查后 state=authenticated、新 Session 正确、progress=confirmed；路由仍是密码页、三个密码字段均空。**唯一失败是最终页面不存在应保留的“密码已修改，当前登录已更新，其他旧登录已失效。”反馈。** 本轮只证明前端模拟边界，没有真实PG、网络故障或底层 COMMITUnknown。

确定链路：`PersonalSettingsView.vue:20–29` 新挂载时无条件调用本叶 `load('password')`；`usePersonalSettings.ts:258–278` 在普通资料读取前清 message，成功后置 idle。它覆盖了同文件 `stopPassword:566–584` 已从协调器确认事实投影的反馈。卡§5.3要求确认后在密码分区就近说明登录更新，且后续读失败不能降级改密事实。

最小建议：让密码确认事实和常规资料加载状态分离，或在加载后保留/重投影该事实；不能删除成功断言、重发改密POST或因普通profile读取改称改密失败。需保持真实身份代际清理及离页不弹回反馈。作者沿同语义原反例定向修后，再给固定差量；本实例首红后没有重跑或扩大预算。

## 独立实际运行

私有根即本报告目录。固定最小闭包为19冻结文件加9a710必要依赖，共63文件346830字节；依赖仅复用作者已锁 node_modules 的逐包只读链接，生成缓存及TMPDIR均在本实例私有目录，没有安装/修改包。probe 用固定作者测试的 helper 前缀（原 cases全部排除），新增一个独立序列；不是重跑作者旧测试。

实际 cwd：`tree/web`；Node `v24.19.0`、Vitest `4.1.11`、jsdom；原默认单例5秒，无retry。

```
node node_modules/vitest/vitest.mjs run src/tests/independent-settings-password-feedback.spec.ts --reporter=verbose
```

1 top / 1 case，**exit1**，Vitest 1.43s，case119ms。`input.json` SHA `599227f3c69c1ce5b3f908a27ca033b446247479accd14b376e2c6c7e69107d6`；probe SHA `8ae42c600f61fe4c35bd87755b93cc824f6c95152c706b8727a16bf8dd9dfe08`；原 `pure-probe-01.log` SHA `b8f5d7650c5050d6022f9db474407571676023d313ce8bb28f12973fed62a4b1`。command/result/postcheck保留原argv/env/input/exit/末检。所有63固定文件与probe末核不变；实际私有进程扫描为空，无Docker/browser资源。

## 其余已核与作者证据

- App唯一草稿owner跨checking临时视图卸载保留；真实失效/身份代际切换清理。dirty确认先于路由Session重验，logout同样先确认；readonly认证owner仍负责actual tail，不新增请求队列。四个安全返回目标精确闭集，Login只做该安全返回窄改，认证旧断言仅已授权settings导航和typed mock适配。
- saved/draft原version、候选File版本与待重试快照分开，Unknown检查不当receipt，主题preview和当前saved取消来源分开；确认204与后续GET失败分别反馈。候选/当前Blob独立引用及失效/owner.dispose清理路径已核。密码200同步progress清输入、同owner新Session确认主要组合沿core02及作者固定测试复用；本项补的是失败后显式检查、重新挂载的缺口。
- 作者 `web-check-01` 输入19文件与本冻结逐SHA匹配，原日志 SHA `742cb15c84c95e30d3641bc20c5933ca4ee8f8c8a6ec95b9ced543c50c0cf9f0`：format check、9文件108 pure、type-check、production build exit0。并未因此覆盖本独立反例；不把build等同浏览器验收或独立dist运行。
- 保留作者早期 type01 的 TS union narrowing 红及原owner字节，最后分支现明确判断 password，最终type过。ui-unit01四红来自button helper把隐藏loading/success标签计入text；固定01→02仅改选活动label。ui-unit02剩余头像case创建数3而非2：最终仍在离页前要求候选+读回共2，离页后允许重验引起的短暂重挂载，并以全部创建/释放的排序数组精确相等（不再用Set掩盖重复）核每个URL，原断言目的未弱化。unit03的11例及owner01的30例属于其各自中间输入，不伪称终版单次全绿；终版依据web-check。原日志SHA及必要旧源位置均入index。

后续等待作者 UI-F1 固定修复；独立真实阶段及其余五fixture仍未启动，不占Docker/browser窗口。不改业务/旧测试/仓库/Git，不委派；原私有固定输入及首红保持冻结，all-stop。
