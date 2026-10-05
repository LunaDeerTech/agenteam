# 公开账号入口：独立验收准备

状态：计划冻结，等待作者核心固定输入及后续明确运行授权。基线 `c54f73f3324caa11608d84e5d207141985eb6074`；规格 rev1 SHA `3f3e3a9624af7b73ed8a5082d74e85c6a7f3631286892352e209c2e0b05008d2`。沿已通过的规格静审，不扩大23路径、不新增产品含义。行政头/§10归位不改变技术依据；收到后仅核差量。

## 顺序与取证

先审冻结 client/account/useSession 核心及作者对应纯反例，再审固定链接捕获、owner/router/App/Login 与页面组合；最后审真实 fixture/选择器和最终 dist 绑定。只读每阶段固定副本及其精确原日志，不把活动文件或只有测试名称的说明当证据。作者保留原失败和修后输入；最终核核心、UI、fixture、dist及必要依赖一致后再下结论。

作者仍负责卡列出的纯检查、4个新真实顶层以及旧认证4顶层/设置4顶层受影响回归。独立增量按冻结实际覆盖去重，默认上限为两类定向纯组合和一个真实组合；若45s内不能形成完整有判别力的组合，先向root给具体拆分依据，不私自加时或减断言。此时只定风险和观察，不预写探针或复制源码树。

## 两类优先纯组合

1. **同一 owner 与原命令上下文。** 从真实有效 Session + 已准备匿名上下文开始，验证五个新调用使用匿名 CSRF且现有Session不被伪清除。entry/auth/personal交叉占用时，新调用必须在新key/transport前明确busy；可见超时、页面放弃后原fetch/body仍未返回，busy/原owner不得提前释放。捕获 Browser本地epoch/CSRF、key、完整token与字段的原Unknown，待actual tail真实结束再尝试会旋转上下文的操作，避免测试仅命中busy而漏掉uncertain门槛。显式原重试必须原值、无先行inspect/prepare；之后410/明确拒绝不能抹掉历史Unknown；CSRF失效禁重放而不自动bootstrap/newkey。通过正式工厂的受控fetch/body和可观察调用记录验证，不假装读取HttpOnly BrowserID，不把公开纯替身称真实PG Unknown。
2. **已确认 reset 与后续身份/页面生命周期。** 精确204先发布确认并清token/password，再在原owner内GET Session；200保留真实另一身份、401清真正失效身份、GET503/坏响应返回confirmed+unconfirmed，不能再发reset。用确定barrier区分已消费204与GET未结束，期间切页/放弃/尝试新身份不得插入第二Cookie请求；原bounded GET完成后安全全局状态可更新，旧页面反馈/清理不得覆盖新代。结合作者UI证据核fragment替换：先清URL，dirty取消保留旧意图，批准后等待actual尾部，新旧hash/popstate/同path事件不能重复inspect或由旧unmount清新上下文。若作者已给相同因果链的充分证据，仅补尚缺分支，不重新复制其整套用例。

## 最小真实组合

优先选“另一身份 + 明确动作”一条正式浏览器链：真实当前A打开B邀请，真实Forbidden后A仍有效且零logout；页面明确切换、实际点击并确认logout后才普通登录准备，重新打开链接才能继续。根据作者已覆盖程度，增量可改选A持有Session而重置B的204→当前Session仍A，避免重复全套创建/重置。两者均要求正式HTTP前置、正式dist、真实同源root及只读PG/精确Session事实；不能以页面文案单独判成功、route.fulfill/假Session充当组合。最终选择只在作者结果冻结后确定，并向root交精确selector、输入和资源observer再申请运行；不在本计划中承诺两条都重跑。

Fragment早清/私有存储、switch参数闭集和普通login四return属于共享风险：静审加作者纯/真实证据应证明无自动logout、无token入history.state/DOM/storage、URL清除失败零POST、旧Login leave不擦新页context。若真实组合采用其中一条，另一条可复用作者充分且最终同源的证据。无新网络/协议/COMMIT/ROLLBACK故障方法；Unknown与过期模拟按卡限纯受控依赖。

## 复用边界与完成条件

- 固定Account/app/OpenAPI/accountenv相对已验认证9a710零差量已核，后端业务/迁移不在23范围；可复用其既有正式权限、CSRF、receipt、SMTP不回退证据，不机械重跑两cmd/全Go域。每次最终基线整合仍核这些依赖指纹及真实root，不将历史结论套到有差量实现。
- 旧认证/设置与本卡共享client/controller/App/router/Login，不能因旧测试文件未改就复用历史动态结论。保留原断言/预算，作者给受影响回归或逐分支生产影响及输入一致性证明后，独立决定具体复用；四份mock仅类型适配不是回归通过证据。个人设置历史CDP首红及Node采样限制不改写。
- 作者的当前输入实际覆盖可直接复用；修复前结果仅在相关生产路径与依赖影响被明确排除时复用，并记录版本/组级粒度。编译、模拟、非verbose包级与真实top/case证据分别写清。原red、准备失败、修复delta和历史不确定原因保留，不补造根因或一次全绿。
- 未来运行保持driver race/count1/6m、top2m、PW45s/workers1/retries0；短自有runtime、锁定工具/浏览器、无敏感trace/video/body输出。独占窗口另授；记录实际Node/Chromium/crashpad PID+starttime与wait、4c3n活体归属及双exact absence、可信原2c4n基线与源/资产末核，结束先交窗再归档。

最终只判本卡23路径完整公开流程，不外推生产SPA托管、完整D26/D27或Object/Artifact修复。当前没有实施、纯测试、浏览器或资源验收结果；只核已有冻结文件指纹并写本私有计划。未运行Go/npm/browser/Docker/network，未读活动业务或改共享文件。**all-stop，等待核心冻结输入。**
