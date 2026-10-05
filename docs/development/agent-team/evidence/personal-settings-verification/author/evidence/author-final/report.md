# D26 个人设置作者交验

作者自测已完成，最终业务独立验收待主线程组织。固定业务基线 `9a710f272026b41ef69852bbeb41cb7670b500a8`，仅卡内24路径；没有消费活动 Resolver/Artifact，也没有改包锁、Go生产、API、迁移、共享UI或3份验后文档。最终24源及正式dist仍为 `fixture-input-01`（SHA `20d8fc5e3ec3ad3f5eb477345fd76d002eec057b4a0a2d0fad875e354c1a99fd`），精确路径/SHA/size在本目录 manifest.json。

## 检查与真实覆盖

- `web-check-02`：format、9文件110个纯测试、vue-tsc、正式Vite构建均exit0。
- `fixture-go-compile-01`：固定Go1.27.1 offline readonly integration race compile exit0（无fixture启动）；`fixture-go-vet-01` exit0。
- 最终专属浏览器TS检查及配置语法检查均exit0。实际argv/env/exit/raw由manifest逐项链接；类型检查使用已有web Node类型，没有安装依赖。
- 新增4完整Go tops/4真实浏览器cases均PASS：资料/头像、主题/导航、密码换发、权限/生产布局。
- 原认证4项覆盖为6个真实browser cases。原lifecycle与desktop先通过；同轮keyboard在旧spec的 `Response.json()` 发生 `Network.getResponseBody: No data found`，原父组及driver仍记FAIL。独立限定归因后，主线程仅授权一次原样keyboard子例，实际PASS；不改代码/旧断言/预算，不加clone/tee或CDP干预。其余revocation/expiry/layouts原组通过。这里是分项组合覆盖，不能写成一次原父组全绿。

所有真实命令沿原 `scripts/test-objects.sh`，race/count1/每包6m、Go top2m、PW45s/1worker/0retry。精确选择器、每轮raw与实际清理在 coverage.json、manifest.json 的 runs。没有跳过选中场景；未匹配的其它Go包 `no tests to run` 不计覆盖。

## 已修缺陷与原件

- core F1：改密Unknown且actual owner结束后仍可用旧CSRF注销；增加同一password Session确认屏障，原红→最终纯回归保持。
- core F2：初次Unknown后的原请求重试遇普通not_started错误会丢原意图；保留原Unknown历史及同key/完整File意图，直到确认成功/失效/明确放弃。
- UI-F1：严格改密200后GET失败，显式检查成功时重新加载抹掉已确认反馈；资料读取与确认事实分开，读失败仍明确保留已确认事实。
- UI-F2：同用户后续真正新Session可复活旧改密反馈；私有source identity/requestGeneration加一次承接identity限定A→B，B→C退休，合法同B重验保留。
- 早期TypeScript分支类型、UI测试button标签选择和URL清理时点、spec TS命令缺现有类型路径的原失败均保留，准确区分测试准备与产品缺陷。core F2中间修后源和fixture首TS源为**事后按原记录SHA精确重建**，不是当时备份；provenance原件在对应evidence目录。core F1原失败日志只含纯测试仿真值/随机测试key，非真实会话材料，不将其转述为真实泄漏。

core02、UI03及fixture readiness已独立静审通过；本报告不代替最终独立动态结论。

## 资源与范围

五轮实际执行各观察4容器/3网络的nonce/label/exactID，逐轮两次absent；原2容器/4网络不变，运行目录/fixture临时文件/观察进程为0。24源及20固定依赖/dist项运行前后全相同。首组observer未采到Node PID/starttime，只能引用Go launcher的真实cmd.Wait0；后四轮补actual exe识别，不能反填首组。收养wait只针对本observer实际后代、原PID/starttime，不扩大kill/wait。

已查看安全窄屏截图：`../browser-images-01/personal-settings-narrow.png`（SHA `621bde6e238432837b623ac0067674be9f72ffb2222bfa17292128b70d5ae83b`）。密码等材料只在私有内存/0600 IPC运行目录，trace/video/body日志关闭，运行目录已清理。

普通用户由真实随机同源邀请/inspect/redeem建立；只读PG观察主题预览的version/theme/命令/Audit，无SQL造用户或权限。头像三格式真实重编码/metadata/bytes、拒绝后原头像、改密新旧Session及后继CSRF均有实际后态。Unknown/迟到仅纯可控Promise验证，不声称真实COMMIT或网络故障。

测试拥有的同源dist服务不等于生产SPA托管或实际Vite代理验收。Object已知guard缺陷、暂停Artifact/其它未绑定能力及完整D26边界保持。所有Go/browser/Docker已停止、窗口已交回，24业务源停写；3份验后文档等待另授。
