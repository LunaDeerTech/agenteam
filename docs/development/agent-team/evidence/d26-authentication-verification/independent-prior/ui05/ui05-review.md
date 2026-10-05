# D26 UI05 窄复审

结论：**STATIC PASS，可进入原 LayoutsAndProduction 完整 top 复验。** 本结论不宣称实际缩放溢出已消除，也不替代完整 D26 验收。

固定 UI05 manifest SHA-256 `202204f07dc30851044ad34adbe8476e9c9282c5a96f0878dc32393b8deae0dd`，delta `85a84b5108e1b9281e5121331ae36ea56e468123b6b580b306ac994e3bffd767`。完整 input05 manifest 为 `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`；input04/05 各 21 源逐 SHA 实核，只有 App.vue 与已有 e2e spec 变化，其余 19 个文件完全相同。

- App.vue:41 给受保护路由 AppShell 增加 authentication-shell。AppShell 是单根 div 且无 inheritAttrs 禁用，类落在该根上；:82 scoped deep 选择器只覆盖此壳下的 system-nav，允许 flex-wrap、6px row-gap/padding-block。账号组新增 max-width:100%，配合既有 min-width:0 与姓名收缩，允许品牌/账号分行。无新增裁剪、假导航、隐藏注销、业务状态或事件改变；共享 CSS/组件及 core3/UI04 未变。
- e2e noOverflow 外的源码逐字相同。原 scrollWidth<=innerWidth 布尔检查和 expect(...).toBe(true) 保留；390px、zoom=2、后续 logout 焦点和正式资源回退仍原顺序、原断言。新增日志只在失败时读取最多 24 个元素的 tag/class/rect/client/scroll 及 viewport/zoom；不读 DOM 文本、输入值、URL 或认证材料，不改变成功路径。
- 原布局失败日志和四张缩放前截图指纹均匹配并保留。作者 10 个 raw/meta 指纹匹配：两次格式写入、format:check、npm build（含 vue-tsc 与正式 Vite dist）、spec TypeScript 五项实际命令 exit=0。这里只复用作者证据，没有独立构建或运行，也没有把 CSS 静审替换成无意义的 jsdom 布局断言。
- plan06 SHA `fd9e67ad70ca8b37b8a16a5f1e7b7cd5c629390609e5db6c194cc53661c99821` 保留 selector `^TestAccountAuthenticationWebLayoutsAndProduction$`，原 race/count1/6m、Go 2m、Playwright 45s/workers1/retries0 预算；新截图目录为 browser-images-02。计划不是执行证明。

已验上游与此前 core/UI04 静审结论按字节不变复用。包锁及五个非 dist 固定依赖的 manifest 声明未变；本轮没有读取作者活动 input/dist，故不声称重新逐字核验生成产物。缩放结果、后续尚未执行的正式回退断言与本轮资源清理都留给实际复验。

本轮实际运行 `python3 /workspace/agenteam-d26-ui05-static-v-oyw4hbmp/inspect.py`，exit=0，核 44 个固定源副本、日志/截图指纹、限定 delta 与计划。脚本中五条固定 `git show 457b1979...:<path>` 均 exit=0；详见 read-commands.json。只写本私有目录，无 npm/browser/Go/Docker/network、无仓库/作者目录或 Git 写操作。
