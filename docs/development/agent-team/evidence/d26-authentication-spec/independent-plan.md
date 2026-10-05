# D26 后续独立验收短计划（未执行）

规格基线为已采纳64e47fbc180a1fb2da385e7d4fabcc880699b0d5的rev1.2，业务构建基线为457b1979c9d6563740543b2011eedc06cce34c71；卡与API/根装配静审可复用。本计划不授运行资源，也不预先证明作者实现。

1. 作者冻结核心源码、锁/测试及manifest后，先只读核精确diff。重点确认六API与CSRF43/pass80原样透传、请求/响应闭合、安全错误、不持久化敏感材料；Session协调者的四Cookie入口（含logout）共同串行、原输入/原key和generation、延迟body/transport/finally责任。不能把abort或GET401当作原写未提交。发现确定缺陷交作者，验证者不改业务。
2. 按作者有效纯测试去重，仅补遗漏的高风险反例，暂定最多一个独立测试组：旧Cookie请求忽略abort/在30s未确认后仍晚到，前后代busy与导航不混淆，后继Cookie请求不提前进入；Unknown同key冻结email/password/pass不被表单编辑或bootstrap换代偷换；登录200必须再取真实Session CSRF且身份匹配后才能注销。确切分组和预算待冻结diff/实际覆盖决定，不机械另造大全。
3. 正式browser/Go/PG/MinIO窗口另授后，对完整457b197+候选检查作者四顶层固定证据（Session生命周期、rotate两交互、撤销/期限、布局/生产隔离）。独立优先复核真实80pass经typed client→同key消费与login→GET Session→logout，必要的小型路由/迟到行为补证按风险去重。正式dist+原Host/Origin代理、完整Secret→Model初始化不能换harness或route.fulfill；浅深/断点/键盘/缩放/reduced-motion真实浏览器证据不能由jsdom代替。
4. 沿卡原预算、count/race、worker=1/retries=0；首失败保留输入/argv-env-exit/日志，不先放宽或重复整套。验收前后核源码/锁/产物指纹；每次fixture exactID双absence、原基线不变、进程wait/runtime与私有材料清理后先交窗。没有真实Provider账号、后台Model调用、服务端网络Unknown注入或D28正式托管范围，不把本块通过扩为完整D26/D09或Object缺陷修复。

当前仅完成计划；未读活动业务候选，未执行Go/npm/browser/Docker/网络。独立负责人未参与业务实现。
