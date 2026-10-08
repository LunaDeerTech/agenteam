browser-v4 名称定位器修复限定独立验收 PASS，STOP。可作为只涉及 #21 的完整小块交付；edit01 原 FAIL 保留，真实 edit/layouts 或完整 D27 不在本结论内。

冻结 source SHA 0c498b269c1b52d76ef18f2e249eba3714d643b1aa4dbd353f06e60953444cf0，freeze SHA 9688d909627f3e8253c68e989ebecd7cac8b6f16d84b9196b9297ba854f006d9，delta SHA a38a421b04f7b0100287ec805ecfe4e56536d0c580aa85b3e724a626eed40f23。全文差量只增加 projectName(page) 的精确 textbox/name helper，并替换七处名称查询（edit3、layouts4）。原 fill、值、focus、焦点样式和 readonly 断言均保留；未增加 first/模糊查询或削弱行为。31 处 description 定位不变。

#20 字节/SHA与v3一致；19 UI与Go04两源按接受manifest定点hash全同，API输入和两lock文件不变。imports/tools原字节复用，13项显式输入仅#21改变，未重算大闭包。

固定原件中的format-check实际exit0/0.602s、strict TS/checkJs exit0/1.499s、五mode各列举1项 exit0/4.284s均核实；各命令direct实际wait、inputs-before/after完全等于v4输入、owned两扫空。列举不等于业务测试执行。

复用刚完成的独立label-semantics02：原required/aria-hidden星号结构的exact label匹配0，exact textbox无障碍名称匹配1且fill成功；三组控制及description匹配通过，1.483s含actualwait/owned双清。该证据足以关闭此定位器缺陷，不补edit01缺失的DOM，也不替代后继真实业务/布局验证。首次启动路径失败与第二次授权后继原件保持。

本次只读冻结差量及原始结果，没有重复浏览器、资源、Node/Go检查或产品写入。原件指纹、检查命令和精确接受边界见evidence.json。无活动命令或写者。
