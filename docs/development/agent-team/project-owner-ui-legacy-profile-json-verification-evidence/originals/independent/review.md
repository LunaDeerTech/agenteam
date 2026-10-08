限定 PASS／STOP。独立接受额外单路径 `tests/account-captcha-web/e2e/personal-settings.spec.ts` 的取证修正，以及作者固定 v4 类型检查原件；不接受为旧 profile 真实轮或完整 D27 通过。

候选 SHA `a0fede03ce80118de78a9ba033540bca25587288927791f2daa761c48ea30603` 与 Git367 基线 `155bba1a7bf85ac5020c97b68dd45ade920b0e5016e6dd1f97d7a0202b1699ba` 实际比对，精确只有两处替换：新增闭集 PATCH `/api/v1/me`、400 target；duplicate 提交改用既有 `settingsJSONFor` 并读取返回对象的 `field_errors`。两次替换能逐字节重建候选；其余基线字节全保，400、`/username`／`ALREADY_EXISTS`、aria-invalid、当前读取、取消／清空、头像及最终事实等原断言与预算未削减。

既有 native fetch／JSON helper 的 4694 字节实际相等，SHA `08ef788bb6814e5a52e5103d2ae1080406a0f1cf3108ec31e2804a34c8d4b4b4`。调用仍返回原生同一 promise／Response，不增加请求或重试；观察器限定同源路径／方法且断言恰好一次，实际 response 与 clone 状态都须为 400，JSON 对象与原 field_errors 仍须通过。失败观察仍导致断言失败；finally 恢复 fetch 并 join clone 的 read／cancel／release。该 clone 会 tee 正文，不能用来证明未受观察的流尾或 Cookie owner 尾部。

v1–v4 四 freeze 及所列 41 个原件均逐 SHA／bytes 核同。原失败保持：v1 format-check01 exit1；v2 formatter exit0 只把基线第171行／候选172行的旧 if／throw 拆行，原输出保存后恢复为候选原字节；v3 types01 exit2，旧 NodeList spread 报 TS2488。root 明确继承这一旧格式例外，未补记格式 PASS，也未再格式化。

v4 相对 v3 的唯一 argv 差量为 lib 加 `DOM.Iterable`，与固定 web/tsconfig.json 一致；显式 personal spec、strict、noEmit、Bundler 及其他参数原样保持。作者实际 exit0，1.473605731 秒，无诊断。PID283343／start1517411 已 actual wait，adopted=[]，记录的 owned 两次扫描为空，24 个离线输入前后相同。监督器源码的 wait／watcher.join 与记录相符；此处是原件独核，不是独立重跑或全机清零证明。v2 写格式轮 inputs_same=false 如实保留，唯一变化是目标文件。

本修正关闭已识别的单路径取证问题，没有发现额外必修。原 oldprofile01 的 CDP body FAIL 不撤销；新原生正文、field_errors、UI／当前读取／头像／最终事实仍待真实后继。现有 UI19／Go04／工具闭包沿已接受依据复用，本次未重扫完整955输入；final05 绑定、后续资源准备与授权仍待另审。两首旧 auth 轮的复用仍以最终单源差量／配置／fixtures／工具不变为前提，不替代独立实际 A/B。README22 和完整 D27 未接受。

本次仅只读冻结原件、指定源码／Git基线及必要 SHA 比对，写入本独审目录；未运行 Node、浏览器、Go、filegate、资源或主机扫描。详细固定引用、历史退出／PID／输入边界见 evidence.json。源与本报告 STOP。
