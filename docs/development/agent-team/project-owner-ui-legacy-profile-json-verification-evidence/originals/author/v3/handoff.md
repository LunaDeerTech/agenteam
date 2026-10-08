# legacy-profile-json-v3：显式 TS 一次失败后 STOP

源码保持 a0fede03ce80118de78a9ba033540bca25587288927791f2daa761c48ea30603，逐字节复用 v1 after；相对原155b基线仅授权的 target 与 duplicate callsite 两处差量。helper、所有业务断言与配置未改。

root 已接受继承旧排版例外：原 helper 的单行 if/throw（基线精确第171行、当前172行；v2 diff hunk 起点169）。本版本没有再运行 Prettier。v1 原 format FAIL、v2 formatter 原输出/差量/恢复全部按原件引用；没有改写历史 PASS。

仅一次显式 personal-settings.spec.ts 严格 TypeScript 检查：lib=ES2022,DOM，target ES2022，module ESNext，moduleResolution Bundler，strict/noEmit，完整 argv 见 types01/command.json。没有使用只 include src 的 tsconfig，也没有枚举测试。

tsc 实际 exit2（监督器 exit1），1.218863914 秒；在当前385、1038行报 TS2488，NodeListOf<Element> 缺少 Symbol.iterator。对应源码行在基线中原样存在，不在本补丁两处差量内；这里只确认该固定命令失败，不能据此声称所有类型通过或预定产品根因。

direct PID281656/start1501182 已 actual wait；adopted=[]，owned 两次扫描空、输入同、无 timeout。raw、result、command、输入前后原件完整保留。没有调整 lib、源码或 flags，没有重试。

按失败边界 source/scratch STOP。无 source edits、format、list、业务/browser、Go、build、资源或网络；原 oldprofile01 FAIL 与未验证范围保持。
