# 当前状态：供应商模型列表动作已完成适用验证

2026-10-17 供应商详情“模型”Tab改为仅展示本供应商模型ID、轻量类型标签、实际保存开关及并列编辑/测试；跨供应商选择器与用途页保留供应商/ID。删除入口保留在干净编辑弹窗中；测试仍须显式开始，使用已确认的最新模型/供应商版本，不因匿名配置禁用。等待、409、Unknown原请求恢复和确认后只重读保持。

新增4项开关与2项编辑恢复回归通过，最终type、生产构建及限定格式通过。独审桌面深色/390浅色已通过完整input启停、失败/Unknown恢复、编辑取消与焦点、嵌套删除取消、保存后列表读取失败保最新模型及显式测试。静审两处恢复接缝与取消确认焦点均已修复关闭；独审首轮fixture缺provider投影、测试声明类型失败保留，不称首次全绿。旧catalog/selection harness仅适配入口并检查语法/加载，未重跑旧真实E2E。

[模型列表](https://img.lunadeer.cn/i/2026/10/17/6ad319070f921.png)与[编辑弹窗](https://img.lunadeer.cn/i/2026/10/17/6ad3192a9f03e.png)为1440×1000深色合成数据，作者与独审均已实看，图床上传/匿名下载200且字节一致。独审浏览器全部关闭，自有临时Vite核验后TERM、原launcher实际143，进程/监听归零，共享依赖保留。未新增真实后端E2E、未调用外部模型、未部署公网或操作用户数据。

## 前轮已配置凭据提示

2026-10-17 已保存的供应商API凭据、SMTP密码与出站代理密码共用固定圆点占位，仅依据配置元数据；实际input/model仍为空，聚焦不制造编辑，填写才替换，清空新草稿后恢复提示，删除或待移除状态按当前配置撤掉提示。登录、账号及Passkey密码输入不接此功能。共享控件与Provider现有3项定向用例、type、限定格式及生产构建通过；独审4个合成浏览器输入通过两主页面已配/未配、空保存无密码写入、新输入准确替换、移除后提示消失和键盘往返。代理仅复用共享控件并静态核对状态接线，未重跑其旧业务矩阵。

[供应商配置深色图](https://img.lunadeer.cn/i/2026/10/17/6ad314eb2769b.png)与[SMTP窄屏浅色图](https://img.lunadeer.cn/i/2026/10/17/6ad314ece0678.png)均为合成数据、真实输入为空，作者与独审均已实看，上传及匿名下载200且字节一致。所有浏览器和自有临时服务已退出，原launcher实际143，进程/监听/自有子进程清零，共享依赖保留；未运行真实后端E2E、未发送SMTP、未部署公网或操作用户数据。

2026-10-17 凭据删除入口已完成：移除解除关联checkbox和详情重复菜单，在API凭据框旁明确确认后先解除当前供应商关联，再真实删除凭据；两个命令分别保留版本/key，部分完成可继续或明确结束，Unknown不冒充未删除，已确认删除后读回失败仅重读。桌面按钮与输入框居中，窄屏换行；[最终配置图](https://img.lunadeer.cn/i/2026/10/17/6ad31187aefc9.png)为1440×1000深色合成数据、凭据空，已实看、上传及匿名下载核对一致。

新删除5项与受影响旧组件12项分轮通过，最终type、限定格式及生产构建通过；作者两尺寸正常/取消/确认后读取失败链、独审两阶段Unknown/引用阻挡后继续或结束/无凭据及只读门通过，桌面与390最终布局已实看。原type声明问题、取消焦点失败及按钮对齐问题经限定修复；独审同名关闭按钮定位已收窄，最后窄屏首load失败原因未定、同预算诊断复验通过，不称首次全绿。所有浏览器和自有临时服务已退出，原launcher实际143、进程及监听清零，共享依赖保留。未新增真实后端E2E、未另测reduced-motion、未部署公网或操作用户数据。

2026-10-17 小修：供应商列表开关与“配置”按钮改为垂直居中、窄屏横向可换行；仅页面CSS。格式与作者/独立浏览器检查通过，1440×1000深色和390浅色三行中心差均0，390长名与Unknown反馈无横溢、恢复按钮可达；[新列表图](https://img.lunadeer.cn/i/2026/10/17/6ad30d2b8db8e.png)为合成数据，已实看及上传匿名字节校验。浏览器均关闭，自有5281经身份核验TERM、原launcher实际143、进程/监听/子进程清零，公开5245未动；未重跑业务/type/build/App/PG。

2026-10-17，起点`90a1229b`。模型接入列表已增加“供应商列表”小标题，启用状态改为可操作开关，右侧凭据状态改为“配置”按钮。名称链接进入供应商详情默认“模型”Tab；“配置”进入`/system/models/:providerId?tab=config`，同页“配置”Tab直接编辑API端点、凭据与供应商属性，创建仍用共享modal。Provider命令版本/key/Unknown、凭据独立命令与部分成功恢复合同保持，后端API未改。

proxy_copy唯一实现树为`/workspace/agenteam-provider-settings-vue`，account_backend只读核既有API边界，outbound_visual独立审查，root负责Git。Vue基础`4eafeeee`、修复`268d27eb`及后续窄屏CSS组合已通过作者两尺寸正常链；受影响8组件用例分轮与旧provider4项通过。最终逻辑type、CSS后build及限定格式/CJS语法通过。独审焦点原两case、Secret Unknown/窄屏只读及390 Unknown反馈溢出定向复验均通过；常驻配置props同步与成功提示保留亦已核。

原测试准备缺JSDOM dialog.close、Unknown409自动解锁误期望及raw/reactive宿主偏差保留，定向修正后通过；不称首次全绿。最终[供应商列表](https://img.lunadeer.cn/i/2026/10/17/6ad308b112151.png)和[配置详情](https://img.lunadeer.cn/i/2026/10/17/6ad308b192f95.png)为1440×1000深色合成数据、密码空，已实看并图床匿名下载校验一致。这轮未起真实App/PG，旧后端证据只沿原边界复用；旧catalog harness仅语法/格式/列表加载，不冒充新E2E。自有Vite5281按精确身份TERM，原launcher实际退出143、进程消失且无监听/子进程；仅unlink自有依赖链接、共享目标保留。所有作者/独审浏览器已退出，公开5245未操作，未更新公网或用户数据。

## 前轮ModelPicker与用途命名结果

2026-10-17 用户命名微调：四用途展示名改为“向量模型（Embedding）”“总结模型（Chat）”“重排模型（Rerank）”“图像生成模型（Image Generation）”，仅共享标签与必要精确定位器随动；限定格式、3个CJS语法及既有页面读取/清除2项用例通过，未新增测试、重跑浏览器/完整工程矩阵或启动服务，用途key/API不变。

2026-10-17，起点`857d94a3`。用户已确认ModelPicker改为触发选框下方的可复用锚定浮层，一层跨供应商模型列表与搜索、当前勾选和不可选原因；空选框“点击选择模型”，已选“供应商名称/模型ID”始终可见，hover只改样式。四用途各有短描述，image显示“图像生成模型”，重排/图像生成明确尚未绑定。后台提供真实全局查询，禁止前端拼供应商各页；单summary保存、草稿与Unknown不改。

account_backend在`/workspace/agenteam-model-picker-backend`（`ai/model-picker-backend-20261017`），proxy_copy在`/workspace/agenteam-model-picker-vue`（`ai/model-picker-vue-20261017`），runtime在`/workspace/agenteam-model-picker-tests`（`ai/model-picker-tests-20261017`）；root负责集成/Git，组件规范由Vue owner维护。后端生产`d95dffb2`稳定，Vue最终`1b8bd205`含分页重试回焦修复`575db175`；前一主树产品集成`82ff92d6`。GET/HEAD `/system/models`的全局查询与安全provider投影已落地；合同沿[模型接入工作项](../docs/development/work-items/frontend-model-catalog.md)。本轮仍只交图床匿名校验后的截图，不部署公网、不动现用户数据或外部SMTP。

已过范围：后端定向unit、OpenAPI route与真实PG跨供应商搜索，含字面%/_、游标绑定/水位、安全投影、System权限和Project隔离；原compile适配失败及首次helper漏Go环境未RUN保留，后续通过且专属资源清理。Vue共享控件、页面、搜索及原共享/q回归分轮通过，基础实现type/build通过，575焦点增量后最终type通过、未重复build；搜索测试准备移入限定beforeEach，业务仍5秒，但不称原总计时边界未变或已证fakeTimer死锁。作者两尺寸受控浏览器通过搜索/选中/Escape/disabled/单PUT，初次带副标heading定位失败仅修harness。

独立视觉已接受桌面/390触控、长名、浮层翻转与内部滚动、嵌套删除复用、键盘回焦及原生200%/reduced；分页500重试后焦点落BODY的P2已修，同输入复验及定向焦点测试通过，三张合成示例图已上传匿名校验。真实API与UI均actual0：跨供应商搜索/超过首页分页、草稿零PUT、唯一保存与版本加一、刷新和替换持久化通过，严格TLS/nonce及logout204→401、浏览器关闭，零模型外呼。此前两次response.json观察失败、一次response.finished等待挂起及随后旧图像字段标签失配保留；前三轮底层CDP/请求原因未证，最后一项仅harness标签修正，不写首次全绿或全仓全量通过。

隔离运行已精确清退：App、外链与原launcher实际退出0，4容器/4网络/4目录均不存在，5282/5283无监听；App二进制核SHA及零消费者后删除。Vue5281按PID/start_ticks/cwd/参数归属TERM，原链退出143，进程/监听/活动子进程归零，仅unlink自有依赖链接；主树依赖及公开5245未动。私有安全结果和原失败材料保留不入Git，旧总结模型证据仍保持原输入边界。

## 前轮统一总结模型结果

2026-10-17，起点`0c182e08`。用户已确认Memory与Meeting共用一个“总结模型”，平台用途改为向量/总结/重排/图像四项；唯一`/system/model-selection`资源按一id/version/回执原子保存，summary须为启用的System chat且支持`json_schema`。两业务消费者统一`SummarySelector='summary'`，业务Purpose保持；删除旧memory字段与System独立meeting-summary配置/API，00149仅重建用途配置，不迁旧值。Project未绑定预留契约不扩展。模型显示改为“供应商名称/模型ID”，保留页头“重新获取”和单保存。

主树`846fe701`已集成backend `b87ed09b`、Vue `0852da63`和harness `5edd81b1`，产品输入随后未变。对应工作树为`/workspace/agenteam-summary-backend`（account_backend）、`/workspace/agenteam-summary-vue`（proxy_copy）和`/workspace/agenteam-summary-tests`（runtime）；security与visual独立审查。完整结果见[模型接入工作项](../docs/development/work-items/frontend-model-catalog.md)。用户仍只要新增截图、不部署公网；现有公开版本、账号/SMTP/密钥与数据保持，外部邮件暂停。

本轮适用门已收齐：真实API与用途/目录UI通过单summary保存、同key恢复、旧接口拒绝、共享引用替换、四用途单PUT及斜杠名称；独立并发两Session仅一成功、另一RESOURCE_BUSY，版本只加1，异body复用key409及撤销Session401通过。作者PG、Unknown提交/回滚、真实Memory→Meeting共用模型及Cold定向复验通过。Cold只证明既定中断状态、无重复解析/dispatch和取消收尾，不代表自动恢复生成。Vue18项受影响单测分轮、type/build、有限视觉与名称漏项修复复验通过；两张1440深色示例图已上传并匿名核对。

首次route/schema环境/media fixture失配、MemoryOrigin迁移准备超时及Cold启动健康失败均保留；后两项定向通过不倒推初次原因，不称全仓或首次整轮全绿。所有本轮隔离运行已清退：runtime原链exit0，专属容器/网络/目录清零，5282/5283无监听；Vue5281原链TERM退出143，仅解除自有依赖链接；测试二进制按SHA与消费者核对后删除，私有安全材料保留。最后测试/独审说明由root集成，不再启动旧fixture；下方历史检查仍保持其原输入。

## 前轮模型页面结果

前轮起点`e36992d8`：系统与项目共享分类已改为“模型配置”，页面及导航显示“模型接入”；用途页按钮由“重新读取”统一为“重新获取”，通过共享页头动作移到标题右侧。URL、单保存编排和后端不变。作者15项受影响单测、type/build与限定格式通过；独审1440深色/390浅色两场景通过页头同行、名称/返回、键盘及重读两个GET各一次/零写入。未重跑保存/Unknown/真实PG。新[1440深色示例图](https://img.lunadeer.cn/i/2026/10/17/6ad2e47c1752d.png)已实看并上传、匿名字节核对；浏览器已关闭；临时5314按精确进程身份TERM，原launcher actual143、进程消失且无监听/子进程，该轮未更新公网。

前轮起点`09efa870`，会议摘要已并入平台模型用途，五项平级、整页一个保存；后端接口未改。仅校验/写入dirty资源，四用途→会议摘要串行，首失败/409/Unknown停止，receipt确认的成功项不重发；未完成草稿按原请求恢复。作者定向15/15、type/build及限定格式通过；独审4个受控浏览器场景通过双dirty顺序、第二阶段失败续写、Unknown原key/body恢复和四用途未配置时仅保存会议摘要，1440/390浅深布局与焦点无返修。没有重跑PG或旧业务全矩阵。5314精确停止，原launcher actual143、进程消失且无监听；公网未动。新[1440深色示例图](https://img.lunadeer.cn/i/2026/10/17/6ad2e10dcc7bb.png)已实看、上传并匿名比对一致；下方前轮结果保持原输入边界。

2026-10-17当前包为[模型接入与平台模型用途](../docs/development/work-items/frontend-model-catalog.md)：系统模型入口三并二（系统10项/正式39叶），供应商列表→模型主体详情，配置用弹窗；新建`openai-compatible`供应商共享地址/凭据，Model.type派发chat/embedding两个既有adapter，旧协议记录只读。用户最新明确完成后不部署公网，只交新增页面功能截图，由视觉owner上传图床；既有公开版本与数据保持，不使用用户凭据或外部供应商验收。

当前集成`65e61bd4`包含backend生产`e48dceaf`、Vue最终`2464e966`与测试`c798f9b4`。r3严格CA管理API及独立权限probe通过；真实Vue目录与用途两case分轮完成同供应商chat/embedding、凭据/编辑/发现导入与停止继续、两个adapter测试、429、成功丢响应同key恢复零额外出站、用途保存刷新和引用替换删除。Vue最终受影响13项单测、格式/type/build通过，旧86unit按原输入复用；秘密材料closure、known Secret 409两状态、窄屏横溢、滚动/回焦等已修并有限独审关闭。四张示例截图已上传并匿名核对，链接与证据边界见工作项。

作者最后真实PG/代理/取消集合通过：同family两type及DB约束、认证代理、规则拒绝零供应商调用、HTTP取消后caller/handler/socket join、原key返回持久cancelled、租约归零及删除后历史receipt恢复；这是作者组合门，非新race或独立全矩阵。r1旧CRUD/权限有效；r2启动依赖失败未归因、初次缺CA、UI定位器失败和旧PG16准备失败保留，不以分轮成功倒推原因。r3与风险fixture均actual exit0，专属容器/网络/目录与进程逐项清零；5281已TERM并actual143，5282/5283无监听，5288仅核已无进程/监听、原wait不可恢复。两测试二进制按SHA精确删除，私有证据保留。用户要求仅截图，本轮未部署公网、未动用户数据或外部供应商。

## 既有预览与前轮交付

2026-10-17。用户已授权将最新代码更新到既有18443入口。环境恢复后旧宿主进程已消失、三个原持久容器停止；运行owner核原容器/挂载/nonce后恢复PG、MinIO和nginx，再完成私有备份与保数据升级。当前候选为VCS clean的 `329591cc41d1a3c2bc2c38ac9d85ded997d717b1`，schema146已应用，26组原数据HMAC、18个immutable文件与4目录身份保持，未终结邮件/open attempt均0；未发外部邮件或注册真实Passkey。真实新组合1440/390只读浏览器已通过：单次密码登录200、IP能力提示与密码路径、初始化入口、资料modal及键盘回焦、原系统11项均符合当前状态；未写资料/设置或发邮件，logout204→401、浏览器退出0、relay关闭且socket0。前两run为harness全页同名label命中隐藏modal，限定密码section后通过，产品未改。公网三种有界输入均未取得nonce：专用CA+SPKI和系统CA的两次IP请求均CONNECT200、curl60、HTTP0，系统CA那次证书subject为openai.com、与目标IP不符；最后以同IP的域名SNI、原IP HTTP Host及系统CA对照，hostname校验与curl成功，但HTTP503明确为上游TLS CERTIFICATE_VERIFY_FAILED。该环境代理上游证书门阻断仍在，未取得新页面；全部网络探测已停止，不宣称公网响应归属通过。

## 已完成的 Passkey 本地适用验证

2026-10-16。用户已授权[Passkey 与邮箱变更](../docs/development/work-items/account-passkeys-email-change.md)：登录增加免填邮箱的 Passkey；个人改密页扩为“登录与安全”并支持凭据添加/命名/删除；资料邮箱经新鲜 Passkey 与新邮箱数字码原子变更。系统设置、导航和全局开关均不新增 Passkey。首次登记/全 key 丢失恢复采用当前密码加原邮箱码，bootstrap 默认邮箱不视为已验证，另实现明确的一次性部署者初始化/恢复入口。

本轮基线 `c09d69b71e9ca982e02089560eeeaeb7765749e8`，已实现并完成适用的本地功能与安全验证。固定部署 PUBLIC_ORIGIN 决定 RP/Origin，IP 下能力不可用但密码登录保持。能力/列表/登录、action绑定grant、Passkey管理/恢复、邮箱变更及运维初始化共享modal/CLI合同已落地；email_initialization_required明确区分bootstrap与普通未验证用户。后端产品 `163c21`、Vue产品 `257cc` 及正式harness/说明已集成主树，隔离验证及资源收尾完成。真实域名、物理认证器、云同步及外部SMTP未验，由用户在独立服务器后验。上一轮隔离验证未改持久服务、账号、SMTP或keys；本次保数据部署见本页最新运行状态，外部实发仍暂停。

owned r3真实浏览器run02已通过密码加原邮箱码恢复、原生注册、免邮箱登录、新鲜Passkey加新邮箱SMTP码、确认成功丢响应后的原key/body恢复；当前Cookie/Session保持，另一Session失效，同key不重复扣验证码预算。独审run03已通过跨Session/operation/action/target拒绝、登记grant单消费者及认证key撤销后正确邮箱码仍拒绝；实际两把key与退出均核验。69项web unit、类型/build及受控视觉按未变输入复用。r1迁移Down误项、r2误取异步旧邮件均已修并清理；浏览器run01导航Timeout及独审run02准备Error原因未定，原材料保留，不由后续通过倒推原因。

accountmail完整非integration单测通过；account完整执行唯一失败为旧HEAD/schema测试将新增合法布尔字段误读为字符串，测试模型修正后该项定向通过，其余沿同输入已执行结果，不称首次全绿。CLI实际签发exit0、空stdout与0600授权、真实隔离SMTP码、错码同key仅扣一次、正确确认HTTP200均已核；原harness随后误解Version类型退出1，修正后仅只读续验确认唯一receipt/已验证邮箱/已消费授权及申请/当前Session保留与其他撤销，没有重发或重新确认。续验首次编译失败后修正通过，原失败保留；普通改邮箱的旧邮箱通知为唯一intent/job且sent、terminal、io_joined。

任务Vite5274已TERM退出143、进程消失且无监听；r3 App及外层runner实际退出0，4容器/4网络逐exact ID确认不存在，App/launcher退出、5270无监听且可绑定，runtime/sidecar/atomic临时文件自动撤销。CLI保留的测试Session随owned数据库清退终结；用户旧公开资源未触及。

既有预览已使用329591cc Central/schema146和私有快照 `/workspace/scratch/agenteam-central-persistent-r1/frontend-stable-329591cc/`。新快照101份源码与候选逐项一致，node_modules以符号链接复用旧c09d快照的同lock依赖目录，新.vite缓存独立；恢复须保留该旧依赖目录。Vite PID15906/start_ticks142188；实际环境和变换模块均确认 `VITE_ACCOUNT_ENABLED=true`，严格本机TLS下login200。原c09d快照与旧PID仅属上一轮记录，不能据此恢复当前组合；其首次遗漏flag的失败仍保留。当前快照preview-record.json保存绝对恢复命令、cwd、必需环境及源hash；按[快照恢复说明](../docs/development/backend/account.md#固定已核验的前端快照)启动，不能从主树任意版本启动5245。备份位于 `/workspace/scratch/agenteam-outbound-runtime-review/passkey-cutover-backup-20261017-r1/database.dump`，0600、PG17.8 custom格式1556890字节，归档list退出0，仅验证可读，未做实际还原。最终Central/Vue/FRP四进程均S且start_ticks匹配，5245/5256/5257监听，原PG/MinIO/web三容器nonce一致且Running；服务保留，验证浏览器与relay已退出。


## 此前 SMTP 独立直连交付

2026-10-16。用户已确认 SMTP 与数据库、对象存储同属独立基础设施：直接连接保存的 host/port/TLS，不受业务出站规则、全局代理或环境代理影响，不新增 SMTP 专属代理开关。保留严格 TLS/STARTTLS、超时取消/join、Secret、SMTP 版本和当前凭据、账户 SendPermit 的 AUTH/MAIL 真实首写、权限审计、队列重投重试与 Unknown。现有账号/Session、SMTP、proxy、规则与 keys 全部保留，无迁移或清库；真实 SMTP 实发暂停，本轮仅 owned 隔离验证。正式合同见[工作项](../docs/development/work-items/system-outbound-policy-proxy.md)。

实现已集成至主树 `edc4104b`，其生产输入与 e2039777 clean 候选一致。独立审查已确认本次关键证据支持交付；runtime 已完成备份、保数据 stage 与切换，活动版本为 `e203977766438a9045b2ac4ef54161e5e5c943f5`，实际 PID/start_ticks/exe SHA 与 manifest 匹配，恢复入口见下方。原 `13a22493` 已真实优雅退出0；本轮未发送外部邮件。

切换保持 schema145，私有备份位于 `/workspace/scratch/agenteam-outbound-runtime-review/smtp-direct-cutover-backup-20261016-r1/`，dump为1556714字节、`pg_restore --list`退出0。规则/receipts/SMTP/proxy/账号/keys/objects 共23组保留HMAC投影全等，18个immutable文件、目录身份及基础容器保持，未终结邮件任务为0；SMTP v5仍为TLS且enabled，proxy v2无凭据状态保持。本机严格TLS、leaf、nonce、基础设施与活动进程身份通过，live200、出站匿名401及login200；ready503仍属既有边界。最终只读收尾全部脚本exit0：Central591580/ticks4770391、Vue55037/ticks274699、FRP303867与303876/ticks2387133与2387171均真实S存活，安全结果文件为 `smtp-direct-final-runtime-result.json`。本次未验证公网或真实外部SMTP。

owned 验证已收尾：真实 App 根在规则拒绝 SMTP 目标且统一代理故障时仍送达隔离 SMTP sink、HTTP 继续拒绝，44.93秒通过；域名/环境代理隔离与真实 socket 首写许可/取消 race、真实PG当前凭据换版、证书链/hostname拒绝均通过。四个最终代表为三模式 none/starttls/tls（76.28秒）、lost_acceptance（38.64秒）、cancelled_parent（67.86秒）、RuntimeForce（39.98秒），均实际exit0。累计16组owned nonce下32个容器/网络均确认不存在，匹配fixture目录为0。

原批次六分钟超时实际exit2、停于 protocol/too_many_lines，admission/material setup失败及 no_starttls 的 MIGRATION_FAILED原因未知均保留；后续仅复验上述四代表，不改写为整矩阵通过。root 已从 `362afebe` 同步 `tests/accountmail/common_test.go` 到主树待保存，仅迁移 context 采用 `cfg.StartupTimeout`，业务20秒预算不变，不倒推旧失败原因。未变更的其余队列、重试与 Unknown 逻辑只按原输入复用；具体分轮证据由实现分支 current 维护。

已部署候选来源 `e203977766438a9045b2ac4ef54161e5e5c943f5`，生产输入沿 b5 未改；Go1.27.1普通构建 VCS clean、1854项生产输入一致、check-config退出0，binary SHA256为 `390b106335240222fb3b7e0d52e34fae9daa23ed67f58c619cd10cedda8d9c19`。首次3df构建虽退出0但VCS modified=true，不作为部署候选；配置检查与实际运行验证分别记录。精确验证材料、原失败及最终资源清理由实现分支 current 维护。

| 此前SMTP轮 Owner / 工作树 | 当时职责与收尾 |
| --- | --- |
| `/root/account_backend` / `/workspace/agenteam-smtp-direct` | 分支 `ai/smtp-direct-transport-20261016`，起点 `1a9250b1`；e2039777实现已冻结并集成，关键代表与资源清理已收尾，维护该分支原失败及验证边界。 |
| `/root/account_backend/smtp_route_review` / `/workspace/agenteam` | 10份规范已归位；本次仅更新主树 current 与账号文档的活动版本定位，记录验证及实际切换结果后冻结，不写产品或实现树。 |
| root / 指定独审与 runtime owner | 关键验证、资源清理、保数据切换及最后只读存活收尾均已完成；root 保存测试预算差量及最终 current，保持外部实发暂停。 |

## 此前规则、代理与 SMTP 诊断（历史）

以下保留原输入、原结果和未验证边界；SMTP 拆分的当前结论以上方记录为准，历史证据不代本轮验证。

2026-10-16。本轮用户确认全公网/私网白名单或黑名单、HTTP统一按规则，以及单IP/CIDR/域名/最左通配目标；页面改紧凑表格、行末删除。新合同使用必填rule_mode与 `{target,ports}`，最多512条；风险地址改为默认32条可见可编辑黑名单，可删后按当前规则访问；不保留目标/DNS/网关隐藏地址硬拒，proxy独立版本及Unknown双snapshot保持。用户明确早期开发不需要旧规则/接口迁移：仅访问policy/规则回执重置，默认version1与32条显式风险黑名单（普通RFC1918/ULA不加入，未加载/未核实Unknown仍fail closed），账号/Session/SMTP/代理配置与凭据等保留；旧body400，无历史兼容分支。已按[工程契约](../docs/development/work-items/system-outbound-policy-proxy.md)完成隔离验证并集成：backend `89e99c6f`、Vue `db4e472f`均已推送；正式预览已保数据切换；用户已恢复指定收件人的单任务真实SMTP实测，结果见下，公网身份未新验。

首轮正式SMTP实测使用当时已保存的SMTP version4（STARTTLS）与HTTP代理 version2，经严格本机TLS/实例身份门后仅提交一次测试请求并获202；共4次尝试（初发加原配置3次自动重试）后终态为 `failed / timeout`，4个attempt均已closed、terminal及io_joined，无 `sent` 或SMTP接受事实。第3/4次采样为 `negotiation → closed`，不据采样断言未进入AUTH或密码有误。跟踪CLI曾因一次GET断言失败退出1（未记录HTTP状态，原因未定），会话已logout204→401；之后仅同任务只读续查退出0确认终态，未重复POST。用户随后自行改为TLS（SMTP version5），主机和端口保持，不能再把STARTTLS当作当前配置或唯一已证原因。

后续只做获授权的连接诊断：固定保存的同一代理、当前解析的同一数值目标和原主机名SNI，CONNECT均得到完整HTTP200响应头。原STARTTLS顺序等明文220、立即严格TLS1.3对照后等220，各5秒均未取得完整220行；前两次行读取超时不能推断线路零字节。随后证书与原始读取核实，peer证书匹配原目标名，但由系统信任的环境代理CA签发，确认中间TLS终止；无ALPN，15秒实际读取到的应用数据为0字节。最后唯一反控在同路径严格TLS后先等3秒仍无数据，仅写一次 `EHLO agenteam.invalid` 加CRLF，28毫秒收到153字节，完整首行为 `HTTP/1.1 400 Bad Request`。这证明当前TLS链路存在HTTP解析或响应组件，未呈现SMTP透明隧道预期；不能据400确定内部具体程序、泛化所有代理，或证明AUTH及最终收件。该对照不还原历史job未持久化的实际目标IP。所有诊断连接均已关闭、CLI退出0，未发送AUTH/MAIL/RCPT/DATA/QUIT、未新建邮件任务，未修改配置或重启生产。私有原始材料保留在运行owner目录，未纳入Git。

本轮后端生产输入为 `13a22493`（交付 `929f660b`），Vue产品为 `20ebed6a`（交付 `41acc04d`）。作者outbound/HTTP完整测试、定向race、相关六包编译、精确规则重置与近1MiB数据库重载通过；独立迁移与API/socket通过，原外层清理失败后精确6资源/3目录已清零。规则Unknown三子例分轮通过，settled后的恢复按既有合同验证，不扩称本轮重跑了双snapshot恢复。新Vue真实规则旅程1/1通过：模式/通配目标增改删、重读、409保草稿及恢复原配置，测试Session退出204→401；owned5260已优雅退出并清理。独立1440/390及原生200%通过；首次visibility失败原因未证，浏览器首轮导航观测失败未进业务，原材料保留，详情见正式测试说明。

正式预览schema145已应用：规则为version1/denylist/32条可编辑默认，旧规则receipts为0；21组账号、Session、SMTP、proxy、Secret、对象及keyregistry保留投影一致，18个immutable文件、数据目录和三基础容器保持，私有备份 `/workspace/scratch/agenteam-outbound-runtime-review/rule-modes-cutover-backup-20261016-r1/` 的dump经PG17.8实际archive-list验证。本机严格TLS/nonce、live200及匿名401通过，ready503仍为既有D05边界。最终用户实例只读页面分轮通过：严格本机TLS/实际浏览器证书与nonce、登录及新Vue模块通过，黑名单32行目标逐项与真实GET一致；代理断点复验确认真实direct值及控件显示。首轮通用display断言失败原因未定，产品未改，原预算保留。两次均logout204→401、浏览器退出和relay清理，零配置写入；任务Vite5246按精确身份SIGTERM后为exit143的Z、无监听，未触用户5245/5256。终核Central512863及实际exe SHA匹配，用户Vite55037与FRP303867/303876存活、5245/5256/5257监听；无SMTP发送或公网重试。

此前Central全部业务出网与持久代理已按[工程契约](../docs/development/work-items/system-outbound-policy-proxy.md)实现，后端及Vue分别集成于 `ef68e733`、`241df5be`，已推送。真实UI/API三旅程、代理选路及权限/Secret/Unknown/lease独立恢复门通过；持久预览已备份并保数据切至最终候选，账号、Session、用户SMTP与密钥保持。最终只读页面基本检查已通过严格本机TLS/身份门，不能替代公网身份验证。Runner、DB/对象部署基础连接不纳；真实SMTP继续暂停，公网仍未取得本实例标记。

此前代理表单文案小轮：改称“代理服务器类型”，主机提示仅IP/域名且不含协议前缀，端口示例7890，用户名/密码标为可选；密码留空保留语义不变，局部修正地址行对齐。唯一产品页及两处精确标签selector已冻结；vue-tsc、三文件格式与两CJS语法检查通过，最终1440浅色/390深色受控浏览器实看无横溢、提示关联与空密码占位正确，零写入，浏览器已关闭，未重跑业务或全量构建。运行owner实核原Central/Vite5245/FRP仍活、严格本机TLS/实例身份通过，无需恢复或重启；DB/账号/密钥/SMTP均未操作，真实SMTP仍暂停，公网未新验。

## 此前范围、组合与所有权（历史）

- 主树 `/workspace/agenteam`、分支 `ai/frontend-vue-rebuild-20261016`；账号工作起点 `c13c2db5`，本轮系统四页从已推送 `cd1a5df6` 接续。此前账号业务检查输入 `85c4ac49`、顶栏共享头像首批 `1e0dbf0a`、设置整框 `6214bc1d` 的限定结果继续保留。旧公开r2 `9dd3044c` 已退出清理，现为下述生产cmd实例；此前fixture结果继续保留原输入边界。恢复以任务分支最新提交及实际差异为准，不回填本状态自身提交号，不宣称已合main。
- 本轮backend树 `/workspace/agenteam-rule-modes-backend`、frontend树 `/workspace/agenteam-rule-modes-vue`；上一轮出站树及账号树只作既有输入。root负责全部Git/worktree/checkpoint及集成；内容按唯一路径owner维护，执行者不做Git写。
- 新 `web/` 的client/session/权限和页面独立编写，不导入、复制或包裹 `web-crape/`。后端成熟密码学、CSRF、事务和领域规则保留。此前七页为login/invite/forgot/reset与profile/appearance/password，并已接系统账号四页；本轮只新增出站规则/代理，不增加开放注册、MFA、设备管理、邮箱修改、用户角色切换/禁用/删除，其他未讨论业务正文仍为骨架。
- 用户已确定登录与找回密码共用同一AccountShell面板模板，含官方Logo、标题、当前表单及居中轻边框/克制阴影；设置用宽页头、左对齐开放表单和唯一SettingsShell，整框居中及中性炭灰已获用户接受。[此前找回密码面板原图](https://img.lunadeer.cn/i/2026/10/16/6ad1c0e89dd92.png)已上传并校验。顶栏“个人设置”只在右上菜单内，触发器显示当前身份已保存头像或默认占位；本轮沿此基准接入四页内容。

| Owner | 唯一职责 |
| --- | --- |
| `/root/account_contract` | 本状态、出站架构与system-outbound-policy-proxy工程契约；本批冻结后由root保存。 |
| `/root/proxy_copy` | 本轮规则模式/目标/紧凑表格及新严格DTO，写独立Vue树；主树用户5245保持。 |
| `/root/smtp_list_adapter` | 此前SMTP适配owner，源码已冻结；原system_api_surface已撤写权。 |
| `/root/account_backend` | 新规则模型、精确规则重置/API/安全匹配及受影响消费者；隔离验证，不动现production或真实SMTP。 |
| `/root/vue_tests` | 本轮出站UI/API正式harness与必要回归；旧TLS/恢复/邮件结果保留原输入，不代本轮通过。 |
| `/root/outbound_visual` | 本轮独立受控视觉/交互审查，两项P2已关闭；不代真实后端或公网。 |
| `/root/outbound_security` | 独立transport与真实PG权限/Unknown/lease恢复门已关闭，保留各自输入与覆盖边界。 |
| `/root/outbound_runtime` | 保数据stage、备份和实际切换已完成；保留服务，只读页面检查与最后存活核对通过，公网身份未新验。 |

## 已打通的流程与检查边界

- 本轮Vue候选 `e6bc7d618d26745f4acbd0e0d31a5457b3d7b4e4` 已远端保存。作者完整check为10文件55项unit及格式/类型/build通过；之后回焦、严格receipt成功而GET失败两项P2定向修复并分别通过作者/独立受控复验，模板增量格式/类型/build通过，未重跑55项。后读失败不再猜直连或空规则，恢复只GET。独审四视口、浅深、两Tab、原生200%、焦点/减少动效完成；两次初期load失败根因未定，保留原材料。合成受控[访问规则浅色图](https://img.lunadeer.cn/i/2026/10/16/6ad20780f2705.png)与[代理深色图](https://img.lunadeer.cn/i/2026/10/16/6ad2062bbfc37.png)已实看、上传和匿名校验，不代真实后端或公网。
- 本轮三个真实浏览器场景以Vue `e6bc7d61`、后端生产 `6ea558f1`、fixture `5a1871f1` 分轮通过：rules17.1秒、authority18.8秒、proxy58.0秒，均exit0且测试Session退出204→401。覆盖规则CRUD/持久/409，代理密码空保持/替换/移除、真实commit丢响应原key/body恢复及后GET失败只GET、权限/CSRF。run01死Vite502已恢复；run02准备和run03五秒导航原因未证；run06测试响应body读取失败改专用CA真实route.fetch原样转发后复验。原失败和输入留在[出站harness](../tests/frontend-outbound-policy/README.md)，不倒推根因。r3真实优雅退出0、3容器/3网络清零、5260无监听且runtime/sidecar撤；任务专用5246随后按PID396832/start_ticks3059717确认归属后TERM清退：Vite及shell已消失、进程组无活成员，launcher396779为待reap的Z且不占端口，5246无监听/TCP拒绝；5241/5245/5256/5257仍监听。隔离测试未触用户18443，后续保数据部署另见下。
- 后端生产Go为 `6ea558f1`，最终候选保存至 `a5d12bf0`（含独审lease测试）。作者root HTTP API→认证代理→Secret借还→owned HTTPS→坏gateway无直连→direct通过（18.42秒），独审自有nonce再跑同冻结binary通过（49.05秒），各实际退出并清理。独审6ea真实PG权限/Secret通过（38.03秒）；Unknown两旧a37原反例在6ea同输入通过（74.03秒），含held COMMIT settle/writer join后原key恢复、另一资源保存不提前解禁。18项transport定向race/env毒值及两个既有受影响selector（10.96/12.24秒）已有实际结果；不称PG race或所有Provider逐一端到端通过。Model/MCP/OAuth/SMTP源码沿同root，独立web-fetch消费者不存在，仅通用HTTP覆盖。
- lease独审旧版remove409反例失败（26.29秒），最终6ea同输入通过（37.12秒）：真实foreign ProcessGuard仍活时保留lease/operation，旧root实际join后才回收并允许HTTP remove200。输入为合法持久状态注入加真实guard生命周期，不称SIGKILL；原链退出0、精确6资源/3目录清零。此前同spool双root、启动预算及测试输入失败保留，不视作产品缺陷或放宽产品deadline。最终 `a5d12bf0` 普通构建通过、VCS clean，1851项实际生产输入与主树匹配；OpenAPI JSON不在此cmd实际Go/Embed依赖中，纠正此前必须因embed重建的假设。
- manager `bbc6236e` 曾独立复现Popen后record发布失败留下存活未登记candidate；修正 `f01ca6ee` 后同.next故障实际关闭：原错误/未知.next保留，新child已wait且无PID/端口占用，status/stop与实际停止一致，selected仍为候选且旧版fallback拒绝，修复本fixture的.next后同版可恢复，合成data/keys/env未变。CLI退出0、子进程全回收；该fixture不代证production迁移/升级或公网。保留immutable nginx的nonce/基础设施source，候选后端另核active-release、实际进程/exe SHA与前端组合；旧probe强制identity.source等于runtime.source的假设不适用于staged manifest，未宣称复用通过。
- 此前正式binary来源 `62ad504378fcab4e65722cbb7e5d2fffa8bc60a1`（含7aa修复），生产bootstrap/PG/MinIO真实就绪。严格本机CA/IP/SPKI、nonce/source、匿名401无Set-Cookie、登录/读取通过；仅Central停启后原Cookie/user/session/SMTP投影保持，PG/MinIO/web及持久keys未换，logout204→401，probe退出0。初次PG父目录权限失败未建账号，专属失败资源清理后修PGDATA挂载成功；恢复管理器端口检查修正后验证通过。
- 此前12:14:30 UTC的Central PID424642/state S/start_ticks3218571，source `a5d12bf0f24afe10e80a142c79d05fdc0ce07e81`，exe SHA256 `777768c55735d7de70ba66ec678156351e1d11f3a4b2150ddd790ae6a94ee26c`与active-release相符；schema144已应用，代理初值direct/null且receipt/operation均0。停前后users/sessions/完整SMTP/SMTP凭据/原出站规则/account keyregistry的HMAC一致，18个immutable文件、4目录身份及PG/MinIO/web容器均保持，未终结mail/openattempt始终0，未发送SMTP。私有一致备份经PG17.8 pg_restore --list通过；首轮验证的buffered FD offset问题已修正，同原dump成功，原失败保留。严格本机CA/IP/leaf与nonce匹配，live/login200、规则/代理匿名401；ready503为D05 DEPENDENCY_UNBOUND，首轮断言失配未存详情，不倒推原因，限定复核符合原契约。
- 当前入口为18443→TLS5256→Central5257，Vue仍为5245。私有数据、五组keys、环境与TLS材料位于 `/workspace/scratch/agenteam-central-persistent-r1/`（0700/0600）；活动manifest为该目录 `runtime-staged-329591cc41d1a3c2bc2c38ac9d85ded997d717b1.json`。Central PID15487/start_ticks140793/state S，source `329591cc41d1a3c2bc2c38ac9d85ded997d717b1`，实际exe SHA256 `bdedc437375eb7647796acbcefc04d239cb0a790fd87c6ae5c0ee68f85907e6b`与活动manifest相符；FRP helper16216/start_ticks144734、frpc16223/start_ticks144750均S。管理命令为 `python3 /workspace/agenteam/scripts/manage-persistent-central.py <action> --runtime <活动manifest完整路径>`（action为status、stop或start）。必须使用当前staged输入，不能用旧e203/schema145或更早binary启动schema146；不自动回旧dump。按[stage与恢复说明](../docs/development/backend/account.md#既有持久实例的stage与恢复)保留原数据/密钥，前端只用上方329591cc快照与其preview-record。
- 此前13a预览只读浏览器实际通过：本机CONNECT→5256保持18443 origin，严格CA/IP/leaf/nonce及浏览器实际SPKI匹配；真实登录200、新Vue module200、规则GET200及内容显示、代理GET200及直连显示。未执行规则/代理写入或SMTP，logout204→session401，CLI/Chromium退出0、relay关闭且socket0。当时终核manager running、13a Central与原frp helper/frpc身份匹配且非僵尸，5245/5256/5257仍监听。此结果仅证明当时本机组合，不代本轮e203浏览器或公网验证。
- 此前唯一公网匿名尝试使用专用CA和SPKI：CONNECT200，curl60/TLS_TRUST_REJECTED、HTTP0、instanceMatched=false；未发送凭据或取得实例标记，也未观察实际leaf issuer，不能将旧代理背景或frp登录当作本次公网证明。
- 此前用户恢复后正式SMTP安全投影为configured/credential_present、version2，`smtp.exmail.qq.com:465/tls`，retry3/60未改。只读确认已有test任务目标与授权收件人相等；终态failed/attempts4/version9、attempt_channel=smtp、attempt_result=failed、reason=network_failed。未新POST或人工retry，诊断Session退出204，observer退出0；未证明SMTP接受或邮箱收到。
- 此前SMTP诊断的一次直连探针DNS成功，但同目标465连接TCP errno111，拒绝来源未知。当时Central已继承HTTP_PROXY/HTTPS_PROXY及小写变量至proxy:8080，无ALL_PROXY；该运行版本SMTP不读取这些变量，未做无效重启。一次代理CONNECT200、系统CA/hostname严格TLS1.3通过（代理颁叶），随后10秒未收到任何greeting/220而超时，未EHLO/AUTH/MAIL；仅说明该探针未建立SMTP会话，不推断代理永不支持。未改SMTP配置、密码、出站policy或产品代理契约。
- 此前backend在旧r2登录、读取安全配置/任务投影并logout204：SMTP为 `smtp.exmail.qq.com:465/tls`、credential_present、version6；近期两个test任务为failed/network_failed，记录attempts=4。未新建已知必败任务。脱敏 `diagnose-smtp.py` 一次inspect通过，已随后端 `62ad5043` 保存；未输出密码、认证材料或收件地址。
- 此前装配与源码复核确认r2的previewSMTPResolver只认识私有sink，对外部hostname直接DNSFailed，未到连接或认证；普通公网IP出站策略本来允许，不能改policy掩盖resolver。r2随机account keyring/TLS私钥仅在内存，无无损切换控制；用户现已授权重建测试数据。另用系统DNS得到的两个A记录各直连QQ465一次，均TCP errno111、未到TLS/AUTH/MAIL；只证明这两次连接未建立，不能推断密码错误或由谁拒绝。production cmd已用Go1.27编译，持久配置、五组keys、DB及独立资源按正式预算/owner核对后启动；新实例与一次停启持久性尚待验证，未宣称真实投递通过。
- 此前三列表产品 `9e557cd6` 与测试/组件说明 `409b49b7` 的完整工程检查通过：全web格式、9文件51/51 unit、类型与build（143模块）。后续U8只改SMTP页的operationBusy禁用条件，限定Prettier及build（含vue-tsc、143模块）通过，未再跑51项，共享五源未变。公共分页定向unit 4/4通过（5.27秒，既有两项加保留行失败重读/停用迟到两项）。SystemList/useCursorList正式接口与三页适配已落，三路解除正文760px上限，外框1200px、主区padding和表单自身行宽保持。
- 五个受控实际Vue浏览器目标分轮有效通过：run01普通模态及发送两项通过、三项失败（两个layout的empty文案预期错误，仅修测试；Unknown停在SMTP初配置）；run02桌面layout及Unknown两项通过，390再次在初配置5秒未见端口处失败；run03仅390场景1/1通过（20.8秒）。两次初配置失败原材料只有results errors，attachments/stdout/stderr为空，无URL/DOM/trace，证据不足以分类或确认根因；未提高timeout，新增安全初页诊断在run03未触发，不因后续通过声称原问题已修，也不继续盲重跑。未重启5250、操作真实后端或发邮件，所有测试CLI/浏览器已退出；原失败和精确输入见harness说明。
- 独审U8（SMTP从10切5焦点落BODY）在operationBusy修正后同输入关闭；1440/390与原生200%页长选择保焦，SMTP模态原DOM、页码与滚动保持。三列表外缘与页头有效边距对齐，限定视觉/交互范围内无未结缺陷。最终实际源码为 `35421b77`；同1440×960浅色合成数据的[用户列表原图](https://img.lunadeer.cn/i/2026/10/16/6ad1ebcfa17d8.png)和[SMTP列表原图](https://img.lunadeer.cn/i/2026/10/16/6ad1ebde6f239.png)已实际查看、上传并匿名下载校验字节一致。独审CLI/浏览器已退出、临时缩放profile清零，无新服务；合成拦截不代证后端或公网。
- 此前SMTP唯一产品差量为 `web/src/views/system/SmtpSettingsPage.vue`：底层列表常驻、详情复用UiDialog，测试受理后保留收件人。U7来自busy时所有Tab被禁用令当前值丢失、列表卸载，局部改为只禁非当前Tab；关闭后等待原生dialog退出再按当前代际回焦，阻止旧恢复抢焦点。路由、共享UiTabs、App认证入口与后端/API均未改，命令及Unknown门禁保留。原组级检查的全web格式及9文件49/49 unit通过，类型阶段因confirmedJob可能为空中断；补显式守卫后单独build（含vue-tsc）通过，未重跑49项。
- 此前SMTP模态三个受控实际Vue场景分轮通过：run01基线 `91f16e7e` 为2过1失败（36.3秒），run02基线 `5a102c9c` 为2过1失败（31.9秒）；后者确认原列表/触发器DOM存活，剩余Esc回焦失败。最终run03只复验首条1/1通过（9.2秒）：页2×20条、收件人、原DOM、滚动误差≤2px及原触发器焦点保持，详情开关零额外列表/配置GET。发送测试一次POST后GET失败仅重读、重试Unknown同key/body两条沿run02有效通过，不宣称最终又整组重跑。旧真实SMTP spec只适配模态及显式清输入，未重启5250执行；原失败与精确命令留在harness说明。
- 此前独审以 `5a102c9c` 加冻结关闭回焦补丁为输入，1440浅色/390深色实际确认Esc、遮罩、footer及X四种关闭回原触发器，原DOM/页2×20/邮箱/滚动保持；受控发送一次POST后查看详情、关闭回发送按钮，Unknown二层关闭确认及继续追踪保留。原生200%与reduced motion下模态内滚、头尾按钮可达、背景不动且回焦，U7关闭，无新阻断。[SMTP模态原图](https://img.lunadeer.cn/i/2026/10/16/6ad1e13da5556.png)已上传并匿名下载校验字节一致，全部合成数据；独审浏览器/CLI及缩放profile已清理，无新服务、真实后端/SMTP或公网操作。
- 此前四页最终成果已保存于 `5db51fe2`。后端生产修复仍为 `7aa9b503`，正式源码/测试/OpenAPI/说明已集成，后端current不并入。窄修将已提交后的投影失败标为committed/replay，后读COMMIT_UNKNOWN改为DEPENDENCY_UNAVAILABLE，成功DTO不变；定向unit及真实PG HTTP验证通过（11.04秒），含四SMTP命令写后读失败、历史同key恢复且版本/投递/audit不重复，普通GET失败仍not_committed。验证用独立5250已核专用CA/nonce与内建首链，现已清理；公开r2继续保留 `9dd3044c` 及原账号/Session，不热替换、不新增公网端口，新Vue保守兼容旧后读not_started的Unknown边界。
- 此前分页功能冻结输入通过 `npm run check --prefix web`：全web格式、9文件49/49 unit、vue-tsc与build。随后U6仅给PageSizeSelect根补一行position:relative，限定格式/差量检查与原输入独审复验通过，未重复全量unit或业务。页级动作位于标题右侧，description按需显示，表单保存/取消就近；三列表共用5/10/20/50页长（默认10），改值清游标历史并回第一页真实读取。旧骨架兼容2/2的输入为U4后、分页前dist；defaultfalse dist只作工程/预览构建，真实链使用5245 true经独立5250代理，不混同输入。
- 此前六条真实spec各1/1有效通过：users 38.6秒，security 11.5秒，invites run02 83.06秒，permissions run02 18.1秒，SMTP 18.9秒，pagination 10.2秒。覆盖真实目录/注册可见、安全冲突与原请求恢复、邀请投递/重试/限流/撤销/兑换、普通用户管理403但个人Session200，以及SMTP空保持/替换与丢响应恢复、202后读失败不重发、实际policy_rejected后人工重试至sink sent、移除认证及停用。分页另以真实只读目录确认默认10→5翻页→20/50回首无cursor及响应行数，定向unit 2/2（7.22秒）覆盖body尾部/迟到旧页长。SMTP输入为 `243a4560`，分页为最终功能候选；均用独立5250，非公开r2或外部邮箱。原分页unit失败、邀请run01 DOM断点失败及权限run01准备CSRF错误留在harness说明；邀请失败虽与HMR时间重叠，未认定唯一原因。作者浏览器/CLI与测试Session均已退出。
- 此前有限独审以 `0a824d41` 加冻结分页/U5/U6差量为输入，U1–U6按各自原输入复验关闭。实际查看三列表1440浅色/390深色的页长、状态行、请求limit与回首页，SMTP原生200%及reduced motion、键盘/焦点；U6修后390根文档与视口均844高，无页底外溢。全为受控合成身份/响应，不代证真实后端或公网；[邀请原图](https://img.lunadeer.cn/i/2026/10/16/6ad1d97b6a56c.png)与[SMTP原图](https://img.lunadeer.cn/i/2026/10/16/6ad1d99969051.png)均已上传并匿名下载校验字节一致。独审浏览器/CLI退出，临时缩放profile已移除，无真实admin或公开服务变更。
- 此前深色仅改 `web/src/styles/tokens.css` 的dark语义色、`web/src/composables/useTheme.ts` 的浏览器主题色映射及 `web/index.html` 的meta。独审发现此前HTML缺theme-color，补标签后才具备实际更新目标。最终三源限定Prettier、build（含vue-tsc）及差量检查通过；实际DOM确认唯一meta随dark→light→dark更新，未称headless已验证原生浏览器栏像素。
- 此前深色有限独审使用5245受控合成只读身份，实际查看1440同输入前后、390深色、两尺寸菜单及焦点/hover：层次可辨、无横溢，键盘菜单与Esc回焦通过；token对比度辅助检查亦通过。作者另看1440浅色，dark块外CSS逐字保持，无真实admin或SMTP操作，未重跑unit/业务/完整布局矩阵，浏览器均已退出。[中性炭灰原图](https://img.lunadeer.cn/i/2026/10/16/6ad1caedf1ecd.png)已上传并匿名下载校验一致，用户现已接受。
- 此前设置整框仅改 `web/src/layouts/AppShell.vue`：按settings路由约束完整内容框最大1200px并居中，桌面外侧留白48px、中等宽32px、窄屏沿原5px；SettingsShell填满框内且内部标题/表单继续左齐，项目设置的项目导航随框收口，其他页面仍全宽。仅收窄内部的旧候选已被用户否定并撤回，SettingsShell相对基线无差异。限定Prettier、build（含vue-tsc）及差量格式检查通过，未新增unit或重跑业务旅程。
- 此前整框修正版独审实际查看profile的1440/1920浅色、390浅深及系统1440浅色/项目1920深色6整页：完整框1200px且左右对称，目录贴框内边缘；390框380px、正文x25且无横溢。作者另验普通projects保持全宽、390目录Esc回焦；此前候选的原生200%和键盘结果仅按未变窄屏几何复用，未声称新版全矩阵重跑。检查均用5245受控合成只读身份，无真实admin或SMTP操作；浏览器已退出。[修正版设置外框原图](https://img.lunadeer.cn/i/2026/10/16/6ad1c8a9d9a0c.png)已上传并匿名下载校验字节一致，无该次布局阻断。
- 此前顶栏产品差量为 `web/src/account/avatar.ts`、`image.ts`、`components/account/AvatarEditor.vue`、`AccountActions.vue`、`layouts/AppShell.vue`。顶栏去除独立个人入口，菜单显示共享的已保存头像；首次认证后读取Profile与受保护图片，资料页候选预览独立。PUT确认后更新共享显示，DELETE204立即清图、后续重读取新版本，旧版本不能复活已删图；同身份Session重验保留Blob，身份代际改变即撤销。读取继续共用账号owner及原容器/尺寸/hash/ETag校验。
- 此前顶栏的build及全web格式检查通过；全量unit为7文件39/39，含新增共享头像生命周期4/4。新增测试首次类型检查发现Mock签名问题，经纯类型修正后最终组合type-check与限定格式检查均通过，产品源码无后续改动。作者受控浏览器已验证首次projects加载、跨profile导航复用同一Blob/单次图片GET、候选不发布，以及1440浅色/390深色和菜单键盘/Esc。
- 此前 `avatar-menu` 真实旅程1/1通过（14.2秒）：独立普通账号候选不发布→上传后两处共享Blob→跨页/菜单进入资料→退出401与Blob撤销→新document直入projects登录后自动读取→删除两处同步与Blob撤销→退出401。输入为 `1e0dbf0a` 产品和新增spec，使用r2单目标本机CONNECT且实际SPKI/nonce匹配；这是真实受保护后端链，非公网验证。admin仅用于创建独立测试邀请，未改其资料/头像/密码；浏览器与临时relay已关闭，原服务未重启。
- 此前顶栏独审接受有头像/默认占位×1440/390×浅深8组合、16张闭/开菜单整页：圆裁比例、触发器尺寸、菜单边界、无横溢及键盘进入个人设置/H1获焦均通过。输入为5245真实Vue配受控普通身份与自绘PNG，无真实用户头像或admin变更，不代证后端生命周期。[合成头像菜单原图](https://img.lunadeer.cn/i/2026/10/16/6ad1c5118941e.png)已上传并匿名下载校验字节一致，独审浏览器已关闭；该轮未重跑旧完整账号旅程或S1。
- 此前面板差量为 `web/src/layouts/AccountShell.vue`：login/forgot-password复用同一面板，账号逻辑与其他页未改。限定Prettier、build（含vue-tsc）和差量格式检查通过；作者与独审均实际查看两页1440/390×浅深8整页，独审另验找回受控202/503、390×320低高度及键盘往返，无新缺陷，浏览器已退出。这是5245真实Vue的有限布局观察，未重跑unit/账号旅程、未发SMTP，也不增加公网通过结论；下列业务结果保留其此前输入边界。
- 真实首链覆盖登录、受保护返回、Session/profile、刷新、退出及401；真实API边界另有一项通过。`browser-run-04`完整旅程通过受控SMTP邀请正文→兑换→普通账号登录→资料保存与409保草稿/显式采用新版本→外观预览/取消/保存/刷新→改密后当前Session200及另一旧Session401→找回与真实SMTP reset正文→重置后目标Session401→新密码登录且主题保留。
- 头像前半已实际完成SVG拒绝零上传、PNG预览/取消、真实上传存JPEG及刷新读取。后半在修正测试Node专用CA信任后定向通过：DELETE204后GET丢响应只重读、DELETE一次；真实PATCH提交后丢响应进入Unknown，冻结草稿并显式同key/body重放，服务端版本不二次递增。受控响应丢失不冒充自然网络或PG提交Unknown。
- 正式挑战测试另有实际1/1通过：连续错误密码401、合法 `commit_state=not_started` 不误入Unknown、错误proof400、真实键盘旋转验证200和同login key登录200，390px无横溢。作者早期scratch验证与正式spec运行分别保留。
- 此前账号业务输入的全量unit为6文件35/35；`format:check`、类型检查及production build均exit0。defaultfalse dist的旧骨架受影响兼容3/3通过；账号真实行为用的是5245的 `VITE_ACCOUNT_ENABLED=true` dev经TLS代理，不能把false dist称为业务输入。账号作者证据为5条独立通过测试加头像前半已执行步骤，精确命令/原失败见[账号harness](../tests/frontend-account-rebuild/README.md)，基础兼容见[框架harness](../tests/frontend-rebuild/README.md)。
- 独审接受登录panel与三项设置、forgot/reset浅深主题和1440/390整页，键盘、reduced motion与原生200%已覆盖。V3外观保存焦点环裁切经12px滚动余量修复，在390与原生200%同输入复验关闭，profile/password共享样式亦通过。真实跨身份reset确认当前管理员user/session保持200、目标旧Session200→401，密码清空且fragment移除，无伪注销。
- S1旧实现真实GET/PATCH401迟到清新Cookie已修：普通认证错误不Set-Cookie，显式成功logout/换会话保持。最终24小时r2、后端 `9dd3044c` 的独立双tab浏览器复验通过：B新登录后释放A的原GET/PATCH401，两次Cookie数量和值保持、Session200；logout204后Cookie0/Session401。验证采用只允许目标IP18443的本机CONNECT固定拨5254，浏览器实际leaf SPKI和nonce匹配，保持规范Origin及完整TLS；这是本机验证，绝不代证公网frp。
- 原登录成功离页误弹、邀请初导航错误均已修并定向复验，保留原失败。头像run05因Node route.fetch缺少fixture CA导致默认失败日志含临时Cookie/CSRF，已真实logout204→Session401撤销、敏感行清理并改0600，独审复核；无trace、截图、上传或Git跟踪材料。原外层失败不改写为整例通过。

## 旧实例与先前公网断点（历史）

以下是切换前记录，不代表当前实例或存活清单；旧r2现已退出清理，当前恢复入口与公网限制以上方当前记录为准。

| 资源 | 当前事实与恢复位置 |
| --- | --- |
| public r2 | 目标体验URL `https://154.12.86.233:18443`，当前仍为旧后端 `9dd3044c`、本地TLS5254及原账号/Session。新production就绪前保留，届时同18443顺序切换；尚未切换或完成公网身份/登录验真。 |
| 旧r2有效期限 | started `2026-10-16 04:24:20 UTC`；实际expires **`2026-10-17 04:23:20 UTC`**，约24小时。页面、PG、MinIO、outbound、SMTP的10张实际CA/leaf期限均已核覆盖；进程或环境退出仍可能提前结束，不是永久部署，也不代表新实例期限。 |
| 运行与停止 | 私有目录 `/workspace/scratch/account-preview-public-r2/`；`runtime.json`为0600原子manifest，`run-state.json`记录运行/停止，`credentials.txt`为0600最小凭据文件。密码、Cookie、CSRF和一次性链接不写入文档或普通输出。 |
| 本地作者组合 | 独立5250已于 `2026-10-16 08:00 UTC` 优雅清理，无监听，runtime与scope sidecar已撤。验证输入是后端 `7aa9b503` 的长驻Go测试进程内真实App及专属PG/MinIO/SMTP等依赖，非独立cmd/Central二进制部署；后续验证需重新启动owned实例，不能复用旧manifest。 |
| 静态预览 | 5244/HTTP18081保持 `VITE_ACCOUNT_ENABLED=false`；公开 `/login` 的禁用输入、官方Logo和预览提示已浏览器核实，账号API请求为0。原型5241、业务dev5245及既有frp18081按授权保留。 |

公开实例不要求frps配置HTTPS/证书，也不依赖用户Nginx；现有443应用保持。临时专用CA和IP SAN会产生普通浏览器的不信任提示，用户已知；保持 `DevMode=false`、Secure Cookie和Host/Origin/CSRF校验，不修改系统信任。严格测试只信本轮专用CA/SPKI。准确启动与停止命令见[后端预览说明](../docs/development/backend/account.md)，不把历史manifest视为服务仍在。

**公网转发已修复，身份验真仍待确认。** 用户直连IP18443报 `ERR_SSL_PROTOCOL_ERROR` 后，查明旧helper87069、frpc87076及launcher83725已被SIGKILL（exit9）并处于Z状态，不能再称其运行；旧helper/frpc归tool的同一进程组/会话。此前 `kill(pid, 0)` 只证明PID存在，漏掉了僵尸状态。App5254仍活，本机复核TLS1.3、实际leaf、nonce200与Vite/login200均通过；原用户错误有真实死隧道缺陷，不能只归因环境代理。

新helper114403/frpc114410使用独立SID/pgrp114403、stdin DEVNULL，输出写本轮 `frp-recovered.log`，不继承tool管道。启动tool结束后，backend及独审在独立调用中确认两者均为S，start_ticks分别672151/672168，实际login/start各一次；运行/停止位置仍见私有 `run-state.json`。独审的一次匿名公网请求期间，捕获该frpc自有连接到127.0.0.1:5254为ESTABLISHED，证明新数据隧道已接到本轮TLS端口。产品Auth、共享frp助手脚本、443和18081未因本次恢复改变，24小时截止保持。

该匿名请求仍被环境管理代理的上游私有CA校验拒绝并返回503；面向客户端的IP证书改签问题也保留。尚未取得公网nonce、未发送凭据，用户恢复后的重试结果未确认；隧道连接及本机检查不代证完整公网登录。root继续收用户直连结果，服务保留至上述期限，后续按实际证据补充。

此前邀请/重置正文链只验证私有sink，不代表外部邮箱送达。当前r2已存在用户保存的外部SMTP配置，但测试resolver不能解析该外部主机，任务在连接/认证前失败。用户本轮已授权向其指定收件人实测，此授权覆盖原来的不发真实邮件限制，仅限该次明确范围；目前未新发或确认送达。202仍只表示受理，SMTP配置后不回退backend_log。

旧45分钟public-r1已正常退出并完成原App/依赖清理，旧manifest/凭据已撤，不能再交给用户。本轮独立5250的App与外链均actual wait0，专属4容器/4网络逐ID确认不存在，7子进程已消失；launcher192442为exit0待reap的Z，非运行服务。作者Playwright均已退出；兼容preview5242已closed、exit143且无监听。backend清理时只读确认public-r2 App、恢复frp及5254/5245/5241仍在，未重新TLS探测或操作它们；旧r2在新production就绪前保留，之后按本轮授权顺序交接，dev保持。

## 保存与下一步

当前功能、适用工程/视觉检查、两张截图与自有Vite清退已收口，由root统一保存源码、测试和本轮记录。检查仅覆盖本轮受影响范围，不新增真实后端接受结论。原公开预览的恢复入口、IP下Passkey限制和外部邮件暂停保持，本轮未更新公网。

账号、原Session、SMTP配置/凭据、keys、proxy、规则与对象数据按既有约定保留；后端恢复用上方329591cc活动manifest，前端从同版私有快照及preview-record恢复。公网两次TLS拒绝与域名SNI对照的上游503均未取得nonce，不能以本机验证或FRP连接成功代证公网响应归属。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
