# ui-v1 独立静态与受控子能力审查

结论：**PASS，未发现本小块未关闭的必修缺陷**。可将本次冻结的前端源作为独立静态＋受控行为小块交付；不是 D27 全卡、真实浏览器或生产 SPA 托管接受。审查者未参与产品实现，产品零写入。

固定输入为作者 `ui-v1/freeze.json` 的完整 19 源，manifest SHA-256 `b7a9ef34a10d0230b541d0daffb19d765d023aa27469a9ca144952f5d1bd9f64`。源副本、仓库文件、作者最终三轮输入、独验前后指纹均一致。独验共绑定 37 个必要源码（19 冻结源＋18 个已接受 Git `7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2` 来源），加工具/锁/探针共 52 个运行输入，详见 `run02/source-binding.json` 与 `run02/result.json`。后续提交只可复用这些字节。

独立检查了 Session Project 分支、原 Cookie owner/finally、权限与身份谓词、workspace controller、App 注入/聚合确认、正式 router、ProjectNav 和五个 Project view，以及相关测试差量。原 System admin/denied 和 personal 门槛保留；普通 Human Project 请求仍由正式 API 判断 Owner。`system-user-directory.spec.ts` 仅改合法新增导航期待。API 与纯 return 子能力复用此前 7db 的 258 项独验；本次 `auth.ts` 新增导航注册/守卫，纯 route/return 解析字节没有改变，没有重新运行该 API 矩阵。

## 本次实际受控检查

`python /workspace/scratch/owner-ui-verification/ui-v1/run_checks.py run02` 实际 exit 0，2.006 秒，29/29 项通过。Node v24.19.0、Vitest 4.1.11、jsdom，45 秒预算；direct actual wait 完成、无 adopted wait、两次 owned PID 空，输入前后相同。原始 argv、stdout/stderr、Vitest JSON、进程与指纹记录均在 `run02/`。29 项为 11 个共享 owner/身份场景与 18 个 workspace 场景，不把参数内多个断言重复计为测试。

- 两种响应 body cancel 尾部（成功/拒绝）均保持 Cookie owner；14 个旧 mutator 逐一返回 busy，Logout/restore 零新派发，实际尾部结束后才允许新 Project 读取。反向 personal owner 同样拒绝 Project 更新且不留下私有 intent。
- Project 403/404 与 System denied 双向隔离；当前 401/CSRF 失效统一清身份；旧 success/401/403 尾部在 leave 后不发布旧内容，实际 owner 阻止重叠恢复，随后新 Session 可正常读取。
- 非法 raw 路径零 Project 请求；Resolve 候选未发布，独立稳定 ID Get 的 403/404 清内容；切路由旧 Resolve actual tail 结束后才进行新对象 Resolve→Get。
- 版本冲突保原 draft/expected_version，先 fresh Get，再显式采用并可取消；同 identity checking 保草稿，Session 或 CSRF 变化清草稿。重复离开确认不产生写入。
- Unknown 粘性、受控 `in_progress` 禁原重放、`not_observed` 不降格、原 body/key 在后来草稿变化后仍保持；lookup 历史回执与当前 Get 分开，只有当前 Get 决定标准名称地址；合法同版本 no-op 回执被确认。
- Project 本地确认不清旧四用途/会议 Summary 两个独立未决 intent；其原聚合链取消保留两槽，明确同意才丢两槽。既有两个实际 UI draft 与聚合确认另复用同输入的作者全量组件回归，未把本探针中的两个私有 intent 称作真实页面两份 draft。

这些 HTTP/lookup/版本事实均来自受控 Fetch，使用正式客户端解码与真实 Session/controller；不是 PG 提交、服务 Unknown、生命周期或真实网络三态证据。cancel 测试使用真实 Web Response/ReadableStream，但不启动网络。

## 可复用的作者最终证据

`author-binding.json` 已独立核对以下三轮的实际命令、原结果、raw SHA、前后输入、19 源一致及其余 161 个输入匹配 Git 7db，原结果包含 actual wait 和两扫空：

- `integrated-unit-all01`：52 文件、2077/2077，通过，26.377 秒；包含旧全部 System/personal/Selection/Summary 兼容回归与新页面公共交互。
- `integrated-build01`：完整 `vue-tsc --noEmit` 后 Vite production build，通过，12.117 秒。
- `integrated-format-check01`：19 源 Prettier，通过，1.449 秒。

以上为已核输入的作者证据复用，不称独立重跑。另核 `dist-ui01` 与 53 项 assets manifest 字节相同（manifest SHA `00bd636e8446c8e75690bcf07745975bd7a4f9b906172de4d3ae55ab10393752`），无 Debug chunk/源码导入命中；只是静态构建产物检查。19 源含未跟踪文件的行尾/空白检查通过，定点 `git diff --check 7db -- <19 paths>` exit 0。

## 原始失败及限制

`run01` 原件保留：28/29，exit 1，2.021 秒，输入同/两扫空。唯一失败在独立探针调用未激活旧 System 页的 aggregate 后期待 true；现有 controller 的 `activeRoute` 条件正确返回 false。仅在探针补正式 `afterNavigation('/system/model-selection', '')` 上下文，未改产品；原失败版探针保存在 `run01/ownership.independent.spec.ts`，与当轮输入 hash 对应。这不是产品红，也没有作者修复提交。作者既有失败轮未删除或重写。

本次未运行浏览器、Go、PG、MinIO、listener、生产资源或停止探针。没有独立声称视觉、键盘/焦点、窄屏、动效、真实登录/撤权、真实响应丢失、26 项数据库分页、真实 HTTP 同 bytes schema/client、旧 16 个真实 top 或新 5 个真实 top 已通过；均待独立 runtime/browser 阶段。D28 发布/生产 fallback 等历史停止项保持。

本报告与 `result.json` 冻结后停止写入；永久归档可后续单独交付，不阻止已验源码的小块提交。
