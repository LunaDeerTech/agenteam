# D26 input04 布局首红：有限静态定位

输入固定于 `457b1979c9d6563740543b2011eedc06cce34c71` 加 fixture-input-04 的 21 源；manifest SHA-256 为 `918ea0883bb77fab0beedcc48cfbbeaa4d38a32230525bdf2fb92090f5af0bdd`。原始日志 `auth-revocation-layouts-01.log` 的 SHA-256 为 `f9c5f9b1dc1f9400a59c2cf00f16823afb8ab44c9ac583aae4b819e173be8bf3`。

原真实轮中 RevocationAndExpiry 通过（12.47s，两子例）；LayoutsAndProduction 在 390px、根元素 CSS zoom=2 后的 noOverflow 断言失败（原 spec:505，top 6.63s）。根据原顺序，之前两主题×四宽度、真实长名渲染及四张安全截图已经完成；缩放后的 logout 焦点、材料存储、正式 API/静态资源回退和 debug 隔离尾部尚未执行。不能把整组或整个 D26 写成通过。

确定的静态缺口是认证顶栏没有适应窄可用空间的换行路径：input04 App.vue:82 的 account-actions 是单行 flex；固定 base.css:104 的 system-nav 同为单行 flex，brand 在 :114 为 flex:none，导航空节点仍有 padding 和相邻 gap。SystemNav.vue 保留品牌、nav 和账号 slot 三个直接子元素。UiButton 的三状态标签共用 grid 单元，visibility:hidden 状态仍参与固有尺寸；不能假设 logout 会任意缩到零。基础 body 没有全局 min-width，HomeView 也已 min-width:0。

这解释了 100% 能容纳、放大后可能横向溢出的生产路径；原 raw 只记录布尔 false，没有溢出元素的 rect/scroll 数值，四张截图均在 zoom 前。因此这是固定源码的静态归因，不是动态记录的唯一 offender 或已测量的最小宽度。

最小修复可留在原 21 路径内：App 的认证壳局部允许 header 内容换行，并约束账号组的可用宽度，保留品牌、实际注销入口和既有姓名省略。无需改共享 CSS/UiButton，不应增加 overflow 裁剪、降低缩放、删除原断言或绕过实际控件。修复是否足够仍须原 LayoutsAndProduction 完整 top 真实复验；失败时仅采有界安全几何有助于后续定因。

本次只做 Python 字节/JSON 核对、固定 git show 和静态阅读；没有运行 npm、浏览器、Go、Docker 或网络命令，没有写仓库或作者目录。输入指纹、实际读取命令和检查结果见本目录 inputs.json、read-commands.json、checks.json。资源清理为上游报告，本静审未重新核验。
