# browser-v5：ready 身份事实区域限定

edit02 原真实FAIL保留。runtime正式归因确认：固定controller在命令已确认、显式当前Get重读完成后，可以合法同时显示主身份dl与当前值review dl；raw调用edit683→confirmed272→ready249因通用.project-facts匹配2个dl触发strict失败。

仅#21新增root指定projectIdentityFacts(page)：dl.project-facts同时具有精确dt Project ID和Owner。ready仍严格toContainText(id)，form可见与重读enabled断言不动；没有first/放宽strict/删ID。全class零内容断言、其它facts断言、namehelper、全部description与其它行为原字节保持。差量除了指定helper及ready一处替换外，回填后与v4逐字节相同。

#20、19UI、Go、共享控件、锁、README未修改。v1-v4原件保持，imports/tools原字节复用v4。最终限定format-check0.731s、strict TS/checkJs1.669s、五mode各1项--list4.349s均PASS，raw/result/pre-post输入、direct actualwait和owned PID双空见checks.json。format write01未改字节，inputs_same=true。

本实例未运行真实业务浏览器/PG/资源/资产。两源STOP，待root独审与定位器小commit后另授edit03资源；v5没有真实运行结论，不称整edit或D27通过。
