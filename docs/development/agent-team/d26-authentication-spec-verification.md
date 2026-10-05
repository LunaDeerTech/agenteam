# D26 正式认证前端规格独立验收记录

结论：**rev1.2 规格独立 STATIC PASS，已采纳并提交推送 `64e47fbc180a1fb2da385e7d4fabcc880699b0d5`；21 路径已获实施授权，业务尚未验收。** 构建输入统一为已接受的 `457b1979c9d6563740543b2011eedc06cce34c71`。实现者 `d08_registry_backend` 先冻结核心交独立审查；`recovery_verification` 未参与业务实现，负责后续独立验证。当前浏览器/Docker 窗口未授，不把本记录当作前端构建、页面或真实浏览器通过。

正式范围见[认证卡 rev1.2](../work-items/d26-account-authentication.md)，原字节与指纹见[证据入口](evidence/d26-authentication-spec/README.md)。本次只归档设计、契约小修与独立静审，不重跑上游产品检查，不修改原临时报告。

## 审查链与原失败

| 固定输入与阶段 | 实际结论和证据 |
| --- | --- |
| `422e0c1` 可行性、rev1 卡 `1a56bc63…` | 可行性仅静态；rev1独立 **BLOCKED，仅F1**。真实 pass 由 UUID36 + 点 + RawURL43 组成80字符，OpenAPI 两字段却 max43，按卡 typed parser 会拒真实 verify 结果。原[F1结论](evidence/d26-authentication-spec/rev1/independent/F1.md.txt)不是前端动态失败或认证绕过。 |
| Account OpenAPI 独立窄修 `df78a9215d1a0dd22f8b8fb26a24e329130fb034` | 唯一 `account.json` 修后 SHA `583783ef80da1e6f93f84e7d5906dfabd3169e2d21f518df930f45d52409f797`。只纠正 LoginInput.challenge_pass 与 ChallengePass.pass 的 min/max80及现有opaque说明；CSRF43、其他API和生产行为不变。作者及[独立复核](evidence/d26-authentication-spec/api-correction/independent/review.md.txt)分别实际执行两字段/整体DTO的80/43/79/81共16项标准schema验证；独立另核optional/required。作者另有211个本地ref核对，本轮归档不冒领重跑。 |
| rev1.1 卡 `c90114c8…` | [独立 STATIC PASS](evidence/d26-authentication-spec/rev1.1/independent/review.md.txt)：正式消费已验上游，CSRF43/pass80与真实typed透传验收明确；原21+3范围保持。 |
| 固定依赖 `422e0c1 → 457b197` | [独立 STATIC PASS](evidence/d26-authentication-spec/root-dependency/review.md.txt)：10路径由API1、root8、structured-wire规格1组成；六Account路径仍走原handler。fixture真实经过Secret→Model同ctx/原30s初始化，原迁移/config/锁/旧fixture不变；不是新浏览器组合已经运行。 |
| rev1.2 卡 `76745915…` | [独立 STATIC PASS](evidence/d26-authentication-spec/rev1.2/independent/review.md.txt)，报告 SHA `339d6f5b32908e2e3efaaf8f9ca6f2fab8445cbdee79dfb835bf4beeca363182`；统一457b197，六API、Cookie/Unknown、21实现路径+3验后docs、矩阵及原预算未扩张。 |

修前schema的“拒80、收43”被原脚本成功复现，`before-result.json` 的exit0表示观察符合预期的旧契约错误，不能写成旧契约通过，也不能虚构命令exit1。rev1.1作者首次自查误要求 `git diff --no-index --check` 在有内容差异时返回0，实际exit1且stdout/stderr空；原[first-check-note](evidence/d26-authentication-spec/rev1.1/author/first-check-note.json)保留，属检查程序期望错误，没有产品失败或源码修订。原patch、报告、输出与输入指纹均不改字节。

## 已核范围与后续验收

规格核对覆盖六真实D07 API、typed完整shape、安全Problem、CSRF/Session与Unknown、同key原输入、bootstrap/Session GET/login/logout四Cookie入口串行及取消代际；共享Ui组件、独立登录面板/空首页、rotate拖拽与键盘、正式dist同源代理与真实root、原预算和唯一资源窗口。规范要求不是已执行结果；原根的System Model初始化不增加Provider调用或新的认证界面。

[短独立计划](evidence/d26-authentication-spec/independent-plan.md)优先看冻结核心的Cookie迟到/finally责任、Unknown原输入和真实pass80链。纯验证按作者有效覆盖去重；真实浏览器仍须正式dist、六API与原CSRF/Host/Origin、完整457b197 root，不能用旧CAPTCHA harness、假Session或route.fulfill替代。未来命令、锁/源码/产物与独占fixture双清理另立动态证据，不为本归档运行。

## 环境准备与归档边界

作者[只读准备](evidence/d26-authentication-spec/environment-preparation/preparation.md.txt)记录当时 Node24.19.0/npm11.9.0、glibc2.41、Chromium151安装文件，以及web145个相关锁包的缓存integrity；两个缺失musl optional不属当前glibc，旧harness当时仍缺六个nonoptional缓存包。原argv/exit/output、文件SHA与缺包统计见其environment/dependency JSON。没有安装、启动浏览器或验证锁定Playwright1.56.1与该Chromium的兼容；后续环境恢复另按业务授权记录，不能把安装文件存在当浏览器通过。

持久归档收46份原字节小型payload，共241158 bytes；三个原API/已采纳卡物件和选读源码由固定本地Git恢复，不复制依赖树、完整源码树、浏览器、cache或binary。原绝对临时路径及旧SHA索引保留为历史来源，当前入口以provenance映射为准。精确原字节空白例外按实际检查列在[名单](evidence/d26-authentication-spec/whitespace-exceptions.json)，不能借例外改写原日志。

本次文档检查仅字节/Git恢复、链接/fragment和格式，命令及结果见[归档检查](evidence/d26-authentication-spec/archive-checks.json)；无Go/npm/browser/Docker/网络或Git写操作。本卡未完成完整D26/D28、Project/Model consumer/Invocation；Summary初值/Settings待决，已知Object退出缺陷及Artifact/Project阻断不因规格采纳解除。
