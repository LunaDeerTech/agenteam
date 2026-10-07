# Production01 五源独立 STATIC

结论：NEEDS_REVISION（R1、R2）；其余四源及handler正常路径限定STATIC可复用。仅冻结 #1/#2/#6/#7/#9，manifest SHA6925716025ad18161d6811c6196785956abdee94d7f11e7aedb48b3717f81d40；五snapshot逐hash同。未读活动8测试、未Go/业务测试/资源/Git。这里的反例为固定源码控制流推导，不冒称runtime FAIL。

R1（blocking，root采纳）：http_project.go:153–165 setter直接调用read/write函数。meetingSummaryRequestIO.finish:219–224先设deadline再Close/join；若setter panic，外层defer会在finish再次panic而提前越过实际Body.Close/stop/join，非ErrAbortHandler还可能被原Recover补Problem。取消AfterFunc:194–196同调用若panic则没有本goroutine recover。须在本卡私有adapter将setter panic安全归一error并保持所有尾部，或同等私有完整收尾；不改旧Summary/Update。

R2（blocking，已交root与作者）：http_project.go:185–187的业务writer包住无Unwrap的projectHTTPIOWriter。WriteProblem经stateOf在该层截断，原响应state.code/已提交防二写门禁不可见；正常middleware下新口错误日志丢domain code。已接受Update06仅controller用IO adapter，业务writer直包原writer。应保持原业务输出wrapper链并补正常middleware+Problem日志/状态反例，无需改公共框架。

其余结论：

- 完整页全部估算先于任一View/Capabilities Validate及DTO复制。服务error在encode之前返回，Unknown不进cap分支；HEAD走同路径，仅写体处省略。当前Owner完全由原五查询同Tx授权，无HTTPSQL/额外查询。
- 8MiB使用checked余量和room除法；strings/map的len先挡，随后text精确Go HTML/控制字符/U+2028/U+2029，raw按<=64KiB、6倍上界且乘前除法核界。原objectJSON:515首门禁拒绝nil/零长，raw预估0不会变null成功。静态固定wire字段算术（扣除另计字符串/raw/数组）：caps247≤384，Provider367≤512，Model429≤512，Available250≤384，覆盖最大版本/TokenCount、非空credential及Project scope；最终Marshal后再核cap。不是执行Go或RSS保证。
- View.Validate继承scope/ref同scope、Project chat协议/type、时间关系及完整配置；目录精确七字段不加载System配置，scope/名字/caps单独验证，nullable数组转换/深复制正确。三列表重复ID/配置排序、游标页关系/长度核对；无新TTL。
- Query原始32768B、decoded8192B cursor、单解码/重复/非法UTF8/NUL/limit/ForceQuery及空Body检查符合卡。前置能力有界解析本身无早Flush/写头；实际尾部仍需上述修复及动态验证。
- app/account.go相对接受Update06仅Model Projects同一实例注入、新handler构造及最外精确路由组合；原初始化、Update work/Audit/Outbox与退出门禁字节保持。
- schema五GET+五HEAD，HEAD全部无content，GET/HEAD参数一致；固定DTO闭合、动态对象合法扩展、七字段目录、能力关系/unique/effort字符条件及无业务总数上限。177 refs/16唯一引用均可在同schema/common固定源定位；这只是静态结构检查，不是schema引擎或真实body通过。

后继生产03只审R1/R2单文件delta；完整13源、compile/offline/native/PG与同bytes标准schema未验证。原01问题与源码保持冻结。
