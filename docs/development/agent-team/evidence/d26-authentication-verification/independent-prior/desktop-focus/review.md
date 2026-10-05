# D26 desktop 拖动焦点缺口

**BLOCKED：真实 desktop 验证成功后未返回登录焦点；产品需补当前题拖动起点的交互归属。** 本轮仅静读固定 input03/UI03、锁定 SDK 及原 raw，没有运行 npm、浏览器、Go、Docker 或网络，没有改生产/测试。

原日志 `/workspace/agenteam-d26-auth-author-jrfhr6h1/logs/auth-lifecycle-rotate-02.log` SHA `dac555118672f65298a19aa21c686dc3e164c4f6efce38494bca834112fe3c7e`，固定 manifest `34830f47406ccb2bc812e8c7cb30c5d0c8f8af93710e2cc7b5cf8caba8b91de9`。实际生命周期 PASS 并有该 Session 两条成功 Audit；keyboard PASS，DB challenge consumed。desktop 在 e2e:297 的登录按钮 toBeFocused 失败，按钮已实际存在但 inactive。按该固定 spec 的顺序，此前 verify200、正确提交角度/key、pass80 断言已完成；失败后没有执行最终登录与 desktop consumed 断言。两个 Go 顶层分别 PASS/FAIL，account package 26.999s；其他 no-tests 不计行为覆盖。

可达静态路径：

- 官方 GoCaptcha 2.0.7 `go-captcha-vue.es.js:894–900` 的 `.gc-drag-block` 为无 tabindex 的 div；mousedown 进入 `dragEvent`。`:716–748` 的初始按下不 focus、不 preventDefault，move 才 preventDefault，mouseup 才调 confirm。
- 固定 wrapper `RotateChallenge.vue:35–44` 将 confirm 直接送入 verify。UI03 `LoginView.vue:49–58` 仅在 busy 由 false→true 且 focusedChallenge() 时记录当前题 owner；没有实际拖动起点的记忆。
- 因而在原 range 焦点已由非 focusable 拖柄按下移到 body 的路径，confirm/busy 发生得更晚，owner 不会建立；成功清题后既无组件内焦点也无当前题 owner，不会执行登录焦点交接。键盘路径仍可在禁用前记录 range，所以与两个分支的实际差异吻合。

原 raw 没有记录 desktop 的逐事件 activeElement，**“按下时到 body”的时序是依据固定 SDK/产品事件链的静态归因，未冒称本轮直接采集到该落点**。先前独立 DOM 探针只证明 disabled 的失焦，不能拿它代替本次拖动过程证据。真实已证明的现象是验证成功后登录按钮 inactive；后续原 desktop 复验应完整保留 toBeFocused。

最小产品要求：以当前题实际内部 pointer/drag 起点建立同题交互归属，使 confirm 前已经落到 body 的真实拖动仍能获得正确成功/失败焦点接续；外部主动交互/焦点、换题、卸载都须取消旧归属，并保留现有 nextTick 同一交接对象、无新题/无 busy 的校验。仅记真实当前 panel 内的有效 primary 操作，不无条件认领任意 body 焦点，不篡改 SDK 角度或阻止正式拖动事件。

作者可先用真实 controller/官方 events.confirm 的 pure 反例，依次发生当前拖柄 pointerdown、模拟原生 body 落点、confirm，覆盖成功/失败交接及外部交互取消。该 pure 应明确为模拟；随后同原 desktop 真正鼠标拖动复验才闭合完整路径。不得删/弱化 toBeFocused、在 e2e 提前强制 focus、扩预算或将产品缺口改称 selector 问题。主线程已授权作者在原 UI/unit 范围实施 UI04，验证者仅待固定差量。

本轮七份选定原件与 manifest 逐 SHA 匹配；SDK 源 SHA `3880426d9f971ca755f2a7f880b2532f0d67f47f188c47d11217b153f0c3b632` 与 UI01 既有核读值相同，精确源码摘录和指纹在 sdk-excerpts.json、inputs.json。原日志/旧报告不改，本报告冻结后 all-stop，无资源占用。
