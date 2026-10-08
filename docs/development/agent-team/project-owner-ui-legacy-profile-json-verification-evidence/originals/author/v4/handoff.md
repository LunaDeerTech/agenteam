# legacy-profile-json-v4：限定类型检查通过，STOP

唯一仓库改动仍为 tests/account-captcha-web/e2e/personal-settings.spec.ts，最终 SHA a0fede03ce80118de78a9ba033540bca25587288927791f2daa761c48ea30603。基线 SHA155bba1a7bf85ac5020c97b68dd45ade920b0e5016e6dd1f97d7a0202b1699ba。base/after/delta 复用 v1 实体；最终字节精确等于基线加两次授权替换：PATCH/me400 target，以及 duplicate 复用 settingsJSONFor/对象 field_errors。其余 helper、400/ALREADY_EXISTS 与后续 UI/facts/avatar 断言、预算及重试原字节保持，没有修改其他仓库路径。

v1 format-check01 原 FAIL 保留。v2 formatter 唯一改动是基线 helper 单行 if/throw 的换行，已保存原输出和 delta 并恢复。root 已明确接受该既存格式例外（基线171行、当前172行，v2 diff hunk 起点169），本版不再格式化，也不回填格式 PASS。

v3 显式 TS 原失败完整保留：ES2022,DOM 缺少旧 personal spec NodeList spread 所需 DOM.Iterable。v4 按 root 唯一授权仅将 --lib 补为 ES2022,DOM,DOM.Iterable，与固定 web/tsconfig.json 一致；源码、strict/noEmit/Bundler 和其余全部 argv 不动。不是放宽类型或改写旧 DOM 行。

v4 单次显式 personal spec 类型检查 PASS，tsc/监督器 exit0、1.473605731 秒，无诊断。direct PID283343/start1517411 actual wait，adopted=[]、owned 双空、输入相同、无 timeout；完整 argv/raw/result/before-after 输入见 types01。web/tsconfig 与历史引用在本轮前后指纹相同。

限定源码与本版 scratch 已 STOP，交 root/ind 独审。没有业务/browser、Go、build、list、suite、资源或外网；原 oldprofile01 FAIL 保持。该检查不是实际 profile 或完整 D27 PASS，真实后继资源仍未授权。
