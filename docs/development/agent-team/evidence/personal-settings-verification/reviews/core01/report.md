# 个人设置 core01 独立静审

**BLOCKED：两项确定静态缺陷，交作者窄修并保留原红。** 本实例未运行反例，不把源码判断称为动态复现。其余本轮核心范围未发现新的确定阻断；活动 UI/routes/草稿owner 和真实整合不在本次结论内。

固定输入 `/workspace/agenteam-d26-settings-author-uydib5xi/evidence/core-review-01/manifest.json` SHA `898f0bcf20f5d638b7662374ece881deb806b06c6bffde494a0dcf3cb2a39691`；delta SHA `a39e27f756949dc2d029a207337bd2e32c816f54af7bfa1b6fc61712c5448bbf`，业务基线 `9a710f272026b41ef69852bbeb41cb7670b500a8`。独立核 sources 七文件的原字节 SHA/size 全匹配。下列 useSession 行号均指该固定 sources/web/src/composables/useSession.ts。

## F1：未知改密实际尾部结束后，原 logout 绕过 Session 重验屏障

`unknown()` 第688–691行及 catch 第737–739行将 `passwordSessionCheck=true`，但保留 authenticated、旧 Session 和 sessionCSRF；第748–754行实际尾部完成后释放 owner。新增 personalIdentity 第628–634行会阻止新个人请求。然而旧 `logout()` 第490–494行只查 owner/pending/session/CSRF，不查待确认屏障，仍生成新key并在第480行用旧CSRF发 logout。

可区别的公开序列：已认证 → changePassword 返回 COMMIT_UNKNOWN 或 transport → **等待该实际Promise完整结束且busy=false** → 未调用restore，直接logout。可能已轮换的Cookie尚未经过当前Session检查，却发出受保护写；违反卡§4/§6的改密Cookie确认规则。现有第239–259、329–347行纯测试只覆盖GET或POST仍held、owner仍忙，不能排除此缺口。

最小建议：让旧 logout/可触发同类身份写的入口尊重同一个待确认屏障，不能只修新personal门面。作者补精确原红：actual结束后logout零transport且零新key；随后明确Session重验完成，才按新当前上下文允许操作。不扩大超时或更改旧安全断言。

## F2：重试的一次普通拒绝清掉先前仍未知的原意图

runPersonal.catch 第733–735行只看**本次**错误：非 isUnknown 且非 IDEMPOTENCY_KEY_REUSED 就令 personalIntent=null。PersonalCommand 没有保存“该原命令此前已未知”的独立事实；restore 的 publish 第226行只将 checked=true。

可区别序列：profile/avatar首次COMMIT_UNKNOWN → 同Session restore成功 → retryOriginal遇到本次前置失败，例如合法 DEPENDENCY_UNAVAILABLE、commit_state=not_started。后一次拒绝只描述本次尝试，不能证明先前写未提交，但当前代码删除原key/body/File，retryOriginal随后invalid-input，新mutation可无显式abandon生成新key。该行为与卡§6保留原未知意图、检查当前状态不等于receipt相冲突，也弱于既有认证 pending 分支第269–278行对后续拒绝的处理。

最小建议：原意图独立记录此前未确认状态，不因后续明确拒绝或Session GET而擦除；只有严格成功、真正身份/context作废或显式放弃才清理。作者补上述顺序的纯反例，核原key/body/File仍可在允许上下文重试、新mutation仍受阻，不能只测 Unknown→直接成功。

## 已核范围与证据限制

- 固定client八endpoint闭集、原16KiB JSON请求限制、raw File/If-Match、严格200/204、头像1 byte至5MiB/quoted Digest/实际长度/有界读及await cancel路径已静读；没有用无限blob读取。原user parser在同一account文件复用，Version/Progress保持int64字符串，无新增身份头或任意URL入口。
- 新runPersonal忙时检查先于newKey/transport，typed结果不复用旧void成功；单一owner与actual finally分离、保存完整私有快照、同owner改密POST→GET、200确认投影、稳定epoch/preview的主要实现已核。F1/F2仍阻止整体通过；最终App草稿/route/焦点需后续固定组合再审。
- 两旧pure文件相对9a710只新增八成员的意外调用拒绝mock，原行为断言保持。作者core-state-unit-02实际4文件56测试PASS、type-02 exit0，日志和输入SHA已核且对应三核心；这些运行仍用旧App/route，不是新UI组合通过。本实例没有重复运行。
- core-format-01是 `prettier --write` 的exit0及其原输入，不是最终 `--check` 或完整工程门槛；本次不扩称格式/整体web已验。

原报告只消费冻结副本/固定Git与已结束原件，不读取作者活动UI或修复稿；没有Go/npm/browser/Docker/网络运行，没有业务/测试/仓库/Git写或委派。作者后续原红与修复另给新冻结差量，本报告保持原core01判断。all-stop。
