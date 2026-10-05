# 独立首轮原红与观测差量边界

plan-v2 的唯一真实轮未通过。`logs/independent-real-01.log` SHA `be61085f7beafadaf81ead1a18503d22196b8114b1b4629f4eac063c81163a50`：Go top 9.48s，account 9.530s，spec:95 在已确认 HTTP401 后调用 original.json()，得到 `Network.getResponseBody: No data found for resource with given identifier`。未获得 CHALLENGE_REQUIRED 的 JSON 证据，尚未进入错误 proof/焦点/换题/注销目标，不能算这些目标通过。未自动重试、未改产品。

该轮实际 4 容器/3 网络活体 nonce/ID，双次 exact absent；原 trusted2c4n 字段不变，11 PID/starttime 终局与 4 adopted waits，runtime/fixture tmp 空，567 输入不变。已先归窗。精确命令/env/exit/raw/双清位于 evidence/independent-real-01；resource-handoff.json SHA `9e8188f89424a2b37ea32ea07e42e39a54403105e0df95304d6bb4fa19944649`。

原因仍未知。作者已通过的原 e2e 使用相同 method/path 监听→click→await response→json 顺序；独立探针多出的 status 断言是同步调用。锁定 PW 的 server Response.body 本来等待 _finishedPromise；Chromium loadingFinished/loadingFailed 都可能终结该等待，再调用 Network.getResponseBody。首轮没有保存 requestfinished/requestfailed/导航事件，因此不能只靠错误推断产品响应错误、提前取消或一般版本兼容故障。固定 client 在 reader.done 后 cancel/release、最外层再 cancel 的源码事实也不能替代该轮时序证据。静读字节指纹在 evidence/cdp-first-failure-static-inputs.json。

root 明确授权 v3 仅修改私有 spec 的被动观测：按阶段精确 arm 同 method/path；原 fetch 参数保持，原 Response 立即返回，不等待 clone。每副本限 600000 字节；登录与随后 GET Session 最多两个明确 arm。通过同一 X-Request-ID 绑定原 PW response，保留一次原 CDP 读取及结果，记录无材料的 finish/fail/导航事实；任何 requestfailed、未知 CDP 错误、响应错配、异常挑战导航或错误产品 UI 分支仍失败。原真实 status/code/key/完整意图/pass/Session/CSRF/DB/Audit 与预算保留。

这是测试内 tee，确实改变流背压和取消传播，也增加微任务，不是无干预或传输尾部证明；F3/取消责任继续按既有 pure 证据分别判断。只有同一真实业务链实际通过后才能给出相应有限结论。v2 原字节与首红不改，后续单次执行由 root 重新授权，不能改称首轮绿色。

v3 首次 TypeScript03 仅两处 Window→测试 hook 类型断言 TS2352 失败，源码/原日志保留于 evidence/typescript03-rejected 与 logs/typescript-03.*；显式 unknown 类型转换后 TS04=0，增加响应 ID 绑定后 TS05=0（0.924s）。没有因此改变产品或动态断言。最终差量 evidence/ready-v2-to-v3.patch，spec `a793ae1a…`，input-v3 `d0558dd4…`，plan-v3 `59c39921…`；Go/config/observer 均不变。
