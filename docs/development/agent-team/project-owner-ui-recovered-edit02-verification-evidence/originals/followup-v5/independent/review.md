# browser-v5 身份事实定位器：独立窄差量 PASS

本结论仅接受 #21 的 `projectIdentityFacts` helper 与 `ready()` 一处替换，关闭 edit02 暴露的身份事实定位器必修。可作为该小修正的独立验收；edit02 原 FAIL 保留，未证明完整 edit、layouts 或 D27 通过，也不授予资源执行权限。

冻结源 SHA-256 为 `3cb33cf8505356c6fdb971fc75af7f47b57f960de8181fbc52a2bb02ea0ef009`；freeze 为 `a878bb0c423cc8820bc3e5060ef96fad290542226d45447ae6dfa0624965ff85`，delta 为 `19b9c8d9e3dea653a4d4a13e09db10077d83e62dc37508265e2b3db58fef7bfa`，均已独立核对。

helper 从 `dl.project-facts` 中同时要求精确 `dt` 文本 Project ID 与 Owner，区分身份区域和合法的当前值 review 区域。相对定位的两个 has filter 保留严格唯一匹配；没有 `.first()`，没有删除 ID、form 可见或重新读取 enabled 断言。edit02 原始 strict 输出已直接证明两个 dl 同时存在；固定 UI 与控制器允许该状态，不需要改产品或新浏览器探针。详细状态归因及其无完整 DOM 的限制复用已冻结 edit02 独审。

移除新增 helper 并恢复 ready 一处调用后，v5 与 v4 逐字节相同。7 个 name helper 调用、31 个 description exact label、其余全 class 无内容断言与行为均保持。#20、imports、tools 与 v4 原字节相同；13 项输入清单只有 #21 hash 改变。19 UI、Go 与完整闭包的未变结论复用已接受基线，本轮没有重算大闭包。

已审作者的既有 format-check（0.731 秒）、strict TypeScript/checkJs（1.669 秒）、五 mode 各一项 list（4.349 秒）的 command、raw、result 和前后输入原件。三项均 exit 0、无超时，前后输入相同且绑定 v5；direct actualwait 有对应 PID/starttime，owned 两次扫描为空。未重跑这些检查。

本轮只读冻结原件与固定源码，写入本独立小目录；未运行浏览器、网络、fixture 或其他真实资源。v5 窄差量审查完成并停止写入；证据指纹见 evidence.json 与 manifest.json。
