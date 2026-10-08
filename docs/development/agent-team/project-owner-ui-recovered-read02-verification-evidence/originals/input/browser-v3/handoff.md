# browser-v3：read01 deleting 断言返修

read01 原真实失败保留。正式契约初核为 TEST_EXPECTATION_TOO_STRONG：D09 deleting Resolve 返回真实409 PROJECT_NOT_ACTIVE、零候选正确；D27允许不可用而不固定409标题。原 spec 错误要求“项目不可用”。本实例已只读核该轮 response-035 安全原bytes/sidecar与hash，定位信息在 read01-observation.json；不重写该轮结果或声称它通过。

仅修改 #21 read 场景：改验既有“项目信息读取失败”，并按当前响应 X-Request-ID 绑定 fixture 安全原件，核实际 GET Resolve409/PROJECT_NOT_ACTIVE、request-id、body SHA与零候选。对该次导航观察窗口要求只有一次Project Resolve、零其它Project GET及零写；页面无form、ProjectNav、项目设置/基本信息内容入口、详情字段。复用既有 same-body schema/public-client 最终检查，无新网络probe、响应clone或私密材料输出。

#20完全复用v2原字节。五场景中其它流程、Go04/协议、API/Session/controller与19个ui-v1源码不变；deleting200DTO受控拒发布测试保持原样。v1/v2所有冻结源/manifest/检查原件保留。imports/tools原字节复用v2（沿v1），未换依赖。

本版仅限定离线：format-check .593s、strict TS/checkJs 1.508s、五mode各1项 --list 4.241s，全部实际退出、输入前后同、direct actual wait、owned PID双清。格式write01 input_same=false为授权格式化，原结果保留。见 checks.json。

本轮没有运行真实browser/PG/listener，也没有触及web/dist。两源STOP，待root另授新版本资源窗口；browser-v3的真实结果全部pending，不能自动retry或据本次离线判整卡接受。
