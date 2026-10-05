# 个人设置最小独立验收计划

状态：仅风险计划，all-stop。基线是已验认证提交 `9a710f272026b41ef69852bbeb41cb7670b500a8`，技术规格为已审 rev1.2 `65ff66b024de34b32f6b0fbc000f5d3e77a3e40ba7bf1b334045ee2d9b031631`；后续行政状态卡 `f78fb7f883fcac25f598ca8b220ced0ed56219dec39f02d6815be9ad5c835fa9` 不视作新增技术能力。§2 最终报告 SHA `9a0450e4c1f799dcbe110f511fcf7d54ab1fe04fe37bd0986339107d76aba23b`。24路径仍由主线程另授作者；本计划不假定新增 personal 门面已实现。

## 作者责任与复用

作者必须完成原卡工程纯门槛、个人设置4真实顶层及认证4顶层受影响回归，按原分组和预算冻结源码/资产/selector/实际命令/原log/exit/资源证据。独立验收核原失败对应版本与修复，只复用输入及语义适用的结果；不把作者全部组再机械运行一次。业务/测试修复由作者进行，本实例只审固定副本；任何实际独立运行须另获授权及唯一窗口。

## 静审阶段

1. 生产冻结后核24路径/依赖，先审 client/account 的闭集wire、body实际清理与严格头像解析；useSession 的 typed返回、busy、新key时点、唯一owner/actual尾部、epoch、密码200与GET；App/usePersonalSettings 的草稿所有权、真实失效与临时checking、主题saved/preview、返回目标与dirty确认。不得将公开identity当服务端授权，也不得把旧void/no-op变成DTO空成功。
2. 最终测试/helper冻结后核真实root和随机f.origin、普通用户正式邀请/兑换、私有record/凭据清理、专属launcher实际wait、原45s/2min/6m及无重试；纯mock只能验证前端状态机，不能冒称服务端Unknown或writer终局。核新旧断言只有已授权导航迁移，没有弱化权限/队列/资源要求。
3. 冻结结果后据作者覆盖缩减以下增量；默认上限2个纯主题top、2个真实top，实际selector与子例数量届时另冻。没有覆盖缺口不追加整包或布局大全。

## 最小独立增量候选

| 组合 | 关键反例与必须观察的事实 | 证据边界 |
| --- | --- | --- |
| P1 纯：typed owner + avatar实际尾部 | 以公开门面进入头像GET，使用有界受控Response body/cancel完成barrier；逻辑离页/visible超时后实际尾部未完时，新profile读/写必须busy且零fetch/零新key，无undefined假成功；只有原body清理真正结束才放下一调用。小头像正例配超限/截断/声明不符的必要负例；原expectedVersion/同一File冻结与重试不重基按实现缺口选一项，不照抄内部函数。 | 真正运行候选Vue/typed client及受控Web API；无真实PG/服务端Unknown证明，不插入网络故障。 |
| P2 纯：改密200后Session确认 | 控制严格POST200后GET尚未返回：表单原值立即清理、passwordProgress为已确认且无材料；原30s内同owner，logout/其它写不能插入。GET失败或visible超时仍保留commandConfirmed、sessionConfirmed=false，不能重发改密；真实新Session/CSRF成功组合由真实R2补证。用确定Promise/fake-time barrier，不依赖sleep或扩大预算。 | 对客户端确认/尾部责任的独立证据；服务端Cookie/权限由真实组证明。 |
| R1 真实：同身份checking + theme/draft +版本/小头像 | 正式dist登录后改未保存资料与主题preview，触发原真实Session重验并观察真实请求完成；当前身份不变时draft/preview保留，持久User/theme/version仍未因preview改变。第二真实会话保存后，首会话dirty不重基；原版本提交真实409且输入保留，取消preview恢复最新已确认saved。小于1MiB真实头像成功及metadata/长度核若作者已有精确有效证据则复用，独立不重复完整格式矩阵。 | 不route.fulfill/伪造Session，不以DOM提示代替DB/实际HTTP状态；不改后端权限或用SQL造身份。 |
| R2 真实：改密→新CSRF→受保护写 | 两个真实当前会话：通过正式页面改密一次，严格200、真实GET Session获得同User新Session；随后一笔正式资料写使用新CSRF并成功，另一旧Session真实401。观察POST次数、User/Session安全ID、字段清空及实际写/撤销后态。敏感响应/CSRF仅内存比较成布尔，原值不落日志/截图。 | 只证明当前链路真实Cookie/CSRF/权限；不模拟服务端Unknown、不恢复网络/COMMIT/ROLLBACK注入，未绑WS/Agent行为不扩称。 |

如果作者最终已对某个真实组合保留足够细的安全ID/状态/持久后态，独立可只补其中竞争边界，或者只跑另一真实top。相同测试名不等于同输入/相同证明。P1/P2也只针对最终尚缺的高风险区别，不为数量写镜像测试。

## 运行与交付约束

此时不写probe、不复制大树、不运行Go/npm/browser/Docker、不安装依赖。正式执行前从最终冻结24源/已验基线建立最小自有输入，精确Go/offline与已锁Node/浏览器，先纯编译/适用检查、冻结probe/selector/资源observer后候窗；不修改作者产品/测试或预算。真实执行沿原driver race/count1/6m，Go浏览器top2min、PW45s workers1/retries0；首红停并保输入/原log归因。取得唯一fixture窗口后才运行，结束精确owned资源两次absent、原baseline不变、子进程实际wait/runtime空后先交窗。

最终结论逐项列作者复用/独立实际/未验，核最终源及原失败映射；只验本卡，不宣称完整D26/D27、生产托管或Object/Artifact已修。本计划没有新的产品证据或资源占用。
