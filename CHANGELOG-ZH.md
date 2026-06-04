# 更新日志（简体中文）

本文件记录了项目的所有重要变更。

这是中文版更新日志。英文版请见 `CHANGELOG-EN.md`，俄文版请见 `CHANGELOG-RU.md`。

## [1.5.7-beta3] - 2026-06-04 - 修复「付费订阅」后台写入 + 加固

- **修复：** 所有 `/api/paidsub/*` 写操作（绑定、套餐、群发）此前以
  form-urlencoded 发送，而后端期望 JSON，故 beta2 中每次写入都静默失败；现已改为
  发送 JSON，恢复正常。
- **修复：** `/start` 仅在确实“未找到”时才自动注册；数据库瞬时错误不再可能在已有
  订阅之上创建并重新绑定新客户。
- **修复：** 机器人轮询循环的连接泄漏（被丢弃的 proxy/outbound 传输的空闲连接现已
  关闭）。
- **加固：** 限流器在饱和时拒绝新键；CryptoBot 的 invoice_ids 进行 URL 转义；过长的
  链接列表按 Telegram 限制硬切分；自定义问候语进行防御性截断。
- 发布说明：[`docs/releases/v1.5.7-beta3.md`](docs/releases/v1.5.7-beta3.md)。

## [1.5.7-beta2] - 2026-06-04 - Telegram 传输选择、群发与问候语

- **按模块选择 Telegram 传输方式。** 付费订阅机器人与管理员 Telegram 模块（通知/备份）
  均可分别选择经由**代理**（http/https/socks5，独立凭据）或经由已配置的 **sing-box 出站**
  （需核心运行）出网；两者独立配置。
- **向所有客户群发。** 新的 *Messages* 标签页向每个已绑定的 Telegram 用户发送一次性公告
  （限速、发送/失败统计、确认步骤）。
- **可编辑 `/start` 问候语**（*Messages* 标签页；留空则用内置默认）。
- **修复（beta1 界面）：** *Auto-registration* 的入站下拉框现在能列出入站（此前错误读取了
  API 响应）；*Bindings* 标签页新增明确的 **Add binding** 操作及清晰的空状态。
- 发布说明：[`docs/releases/v1.5.7-beta2.md`](docs/releases/v1.5.7-beta2.md)。

## [1.5.7-beta1] - 2026-06-04 - 实验性「付费订阅」Telegram 机器人

- 新增**实验性「付费订阅」模块**（默认关闭，与核心隔离）。面向客户的 Telegram
  机器人使用独立的加密令牌，让已绑定的客户获取订阅链接、各 inbound 的分享链接以及
  服务端生成的二维码，并查看当前用量（已用/上限 + 进度条、剩余天数、在线状态、累计
  流量）。
- **Telegram ID ↔ 客户绑定**在新的**付费订阅**管理页面（独立左侧菜单项）管理；不改动
  现有客户卡片和核心 `clients` 表（绑定存放在独立的表中）。
- **带试用的自助注册：** 打开机器人的未知用户可被自动注册，分配管理员选定的
  inbound 与可配置的试用期；通过全局上限与按用户的 `/start` 频率限制加以保护。
- **按套餐付费，多支付方：** 管理员定义套餐（名称、价格、+天数、+流量）；客户在机器人内
  付费/续费，订阅自动延期。可选支付方（可同时启用多个）：Telegram Stars (XTR)、
  YooKassa、Stripe、CryptoBot 以及外部支付链接。续费具备幂等性，金额在服务端按订单快照
  校验，零价套餐不会授予续费。
- 该模块位于独立的 `paidsub` 包中，由单个 `paidSubEnabled` 开关控制，拥有自己的 HTTP
  接口与数据库表（启动时幂等创建）；界面为惰性加载页面并标记为 *experimental*。生产环境
  请设置 `SUI_SECRETBOX_KEY`，使支付令牌用数据库之外保管的密钥加密（未设置时界面会提示）。
- 发布说明：[`docs/releases/v1.5.7-beta1.md`](docs/releases/v1.5.7-beta1.md)。

## [1.5.6] - 2026-06-04 - 首个稳定版 1.5.6：3x-ui 导入正确性修复

- 1.5.6 系列的首个稳定版，整合了 1.5.6-beta1..beta9——3x-ui → s-ui-x 迁移与面板恢复
  终端菜单。以下条目是 beta9 之后新增的导入正确性修复。
- Xray 的 `blackhole` 出站现在迁移为 `reject` 规则动作，而非悬空的
  `outbound: "block"` 引用。sing-box 1.11+ 已无 `block` 出站，该引用会使导入的配置在
  路由时以 “outbound not found: block” 失败；这取代了 1.5.6-beta7/beta8 中的
  `blackhole`→`block` 映射。
- 仅含 DNS 的源配置（无路由规则、无代理出站、无 endpoint）在导入时不再被跳过——
  此前其 DNS 会被静默丢弃。
- 当迁移后的路由引用 `direct`（规则或远程 rule-set 的 download detour）时，会确保存在
  内置 `direct` 出站；检查现在会查询数据库，因此 InitDB 预置的 `direct` 出站不再被
  报告为跳过的重复项。
- reject / hijack-dns 路由目标使用不冲突的哨兵值，因此被用户合法命名为
  `block`/`blocked`/`dns` 的代理会继续路由到自身，而不会被转成动作。
- 完整发布说明：[`docs/releases/v1.5.6.md`](docs/releases/v1.5.6.md)。

## [1.5.6-beta9] - 2026-06-03 - 面板域名/地址重置菜单与 SSL 强制重签

- 新增终端菜单项 *清除面板域名和地址*（同时提供 `s-ui setting -clearDomain` CLI
  标志和 `ClearWebDomainAndAddress()` 服务方法），将面板域名（`webDomain`）、监听
  地址（`webListen`）和 web URI（`webURI`）重置为默认值；当错误的域名或主机无法
  绑定的监听 IP 把你挡在面板之外时，可借此恢复访问。需手动重启面板后生效；新增该
  菜单项后，后续菜单项重新编号（原 `11..21` 变为 `12..22`），选择范围提示扩展为
  `0-22`。
- `Get SSL` 菜单现在可以对 acme.sh 已持有的证书进行重签，而不再以
  “Certificate already exists; cannot reissue” 终止：它会显示现有证书，提示
  Let's Encrypt 重复证书限额（每周 5 个），确认后执行 `acme.sh --issue --force`
  及 `--installcert`，因此 `/root/cert/<域名>/` 中的文件也会被重写。存在性检查不再
  仅检查 `acme.sh --list` 的最后一行，因此存在多个证书时也能匹配到正确的域名。
- 完整发布说明：[`docs/releases/v1.5.6-beta9.md`](docs/releases/v1.5.6-beta9.md)。

## [1.5.6-beta8] - 2026-06-03 - 3x-ui 迁移：出站、路由匹配器、TLS 证书与 DNS

- 代理出站现在会迁移为 s-ui 出站：此前只处理 WARP（WireGuard）、`freedom` 和
  `blackhole`，因此类型为 `vmess`/`vless`/`trojan`/`shadowsocks`/`socks`/`http`
  的 Xray `outbounds` 条目被静默丢弃，链式/代理出站从迁移后的面板中消失。现在每个
  这样的出站都会转换为一等的 sing-box 出站（服务器/端口、`uuid`/`password`/
  `method`/用户名密码、VLESS 的 `flow`、TLS/Reality 块以及
  `ws`/`grpc`/`http`/`httpupgrade` 传输），并注册为路由目标，因此引用它的规则会解析
  到迁移后的出站，而不是被标记为“需要手动检查”。
- 系统出站映射到其 sing-box 对应项：`freedom`→`direct`、`blackhole`→`block`，
  而 `dns` 出站变为 `hijack-dns` 路由动作（sing-box 没有 `dns` 出站）。`loopback`
  以及任何 Xray 不会产生的协议（例如 `hysteria`）会以警告形式提示手动重建，而不是
  被静默丢弃。
- 关闭路由导入时不再静默丢失：代理/WARP 出站位于源 `xrayConfig` 中，仅在路由导入
  时读取。当路由导入被关闭但源中存在出站时，计划现在会警告它们未被迁移以及如何
  迁移。
- 出站仅在为新增时创建（重复导入或计划同步不会覆盖运维人员编辑过的同名出站），并且
  导入报告新增 `outbounds`（已导入/已跳过）计数。
- 迁移的路由与 DNS 现在会应用到实时配置：它们被合并进活动的 sing-box `config`
  设置（route 规则/规则集、DNS 服务器/规则），保留已有规则并按 tag 去重。此前导入
  会写入面板从不加载的单独设置，因此导入的路由不生效——现在生效了。
- 路由规则现在覆盖更多匹配器，而不再标记为“需要手动检查”：`port`/`sourcePort`
  （含范围）、`network`、`protocol`、`source`、`inboundTag`（→`inbound`）、`user`
  （→`auth_user`），以及非 `geosite` 域名（`domain:`/`full:`/`keyword:`/`regexp:`/
  裸域名 → `domain_suffix`/`domain`/`domain_keyword`/`domain_regex`）。`geosite:`/
  `geoip:` 匹配会变为 remote `rule_set`（MetaCubeX `meta-rules-dat`），因为 sing-box
  1.12 移除了内联 `geoip`/`geosite` 字段；source `geoip` 会设置
  `rule_set_ip_cidr_match_source`。`attrs` 与 `balancerTag` 仍需手动检查（sing-box
  没有对应项）。
- Xray 的 `dns` 块会翻译为 sing-box 格式（类型化服务器
  `udp`/`tls`/`https`/`h3`/`quic`/`tcp`/`local`，按域名限定的服务器转为 DNS 规则，
  以及 `final`、查询策略与 `client_subnet`），而不再原样复制（那会生成无效块）。
  `hosts`/`fakedns` 会被标记以便手动设置。
- 非 reality 的 TLS 证书现在会迁移：`tlsSettings` 内含内联证书/密钥的入站会获得真正
  的 s-ui TLS 记录（服务器证书 + 客户端块）；仅以文件路径引用的证书会被标记为需手动
  上传，因为导入器只读取数据库，而非源主机磁盘。
- WebSocket 传输会携带所有请求头，而不仅是 `Host`。
- 出站附加项：当源使用 XUDP 时设置 `packet_encoding`（`xudp`）；多服务器出站会变为
  按服务器的成员加一个 `urltest` 组；Xray `mux` 仅作提示而不启用，因为 sing-box 的
  多路复用与 Xray mux 在协议层不兼容，启用会破坏该出站。
- 网页管理后台在后台刷新期间保留未保存的修改：Basics、DNS 和路由页面此前将表单绑定到
  store 中的实时配置，因此每 10 秒的配置轮询（以及 WS 重载事件）会静默还原正在进行的
  编辑——现在它们改为编辑本地副本，直到你保存为止。
- 路由导入不再生成 sing-box 拒绝的配置：Xray 的外部 geoip 引用（`ext:<文件>:<代码>`，
  如 `ext:geoip_RU.dat:ru`）和裸 IP 之前被原样写入 `ip_cidr`，而 sing-box 无法解析
  （`ipcidr: parse: no '/'`）导致内核无法启动。现在 `ext:` 映射为 geoip 规则集，裸 IP
  补上掩码，无法解析的值则附带警告丢弃，而不会破坏整个配置。
- 迁移的 DNS 不再阻止内核启动：通过域名访问的 DNS 服务器（`https://dns.google/...`、
  `tls://...`）此前未带 `domain_resolver`，而 sing-box 1.13 会拒绝（`missing domain
  resolver for domain server address`）。现在每个域名地址的服务器都会获得
  `domain_resolver`——来自迁移的 IP 服务器，或新增的 local 引导服务器——与 s-ui 自带
  DNS 编辑器的设置方式一致；TLS/HTTP 服务器还会补上 `tls`/`headers` 块，使迁移的服务器
  与手动创建的一致。
- Trojan 入站不再使内核崩溃：入站编辑器会写入顶层 `password`，而 sing-box 的 Trojan
  入站会拒绝（`unknown field "password"`——它通过 `users` 按用户认证）。密码字段现在
  仅用于出站，构建配置时会丢弃残留的顶层 `password`（因此已有入站无需编辑即可恢复）。
- 完整发布说明：[`docs/releases/v1.5.6-beta8.md`](docs/releases/v1.5.6-beta8.md)。

## [1.5.6-beta7] - 2026-06-02 - 3x-ui 迁移：订阅链接、WARP 与导入超时

- 迁移后的客户端现在会生成订阅链接：导入器把每个客户端的 `Links` 留空，而订阅只
  读取已保存的链接，因此导入客户端的入站从不出现在订阅或二维码/链接视图中。现在
  在导入时用与面板正常保存客户端相同的生成器生成链接；主机名取自面板请求主机
  （Web）、新的 CLI 标志 `--host`，或已配置的 sub/web 域名（计划同步）。重复导入
  （merge）会保留客户端的 external/sub 链接，且现在容忍 `NULL` 的 `Links` 列，因
  此后续编辑入站会重新生成链接而不是跳过该客户端。
- Cloudflare WARP 作为 WireGuard endpoint 连同其路由规则一起迁移：3x-ui 把 WARP
  存为被规则引用的 WireGuard outbound，而 s-ui 把 WARP 建模为 endpoint 并通过
  Rules 路由。WARP outbound 现转换为 WARP endpoint（Cloudflare 对端、MTU、地址、
  reserved），其规则改为指向该 endpoint，而不再标记“需人工审核”；源
  blackhole/freedom outbound 按协议解析为 block/direct，因此 `blocked`/`direct`
  规则也会迁移。endpoint 仅在新建时创建（重复导入或计划同步不再覆盖已编辑的 WARP
  endpoint），且非恰好 3 字节的 `reserved` 会被丢弃并给出警告，使配置仍可加载。
- 导入不再以“Network Error”失败：较大的导入可能超过 Web 服务器的 30 秒写超时并在
  中途切断 HTTP 响应，尽管服务端已完成导入。现在在原始连接上解除截止时间——gzip
  中间件包装了响应 writer，使 `http.NewResponseController` 无法触及它——且仅在请求
  通过鉴权、scope 检查和限速之后，工作仍受请求上下文限制。
- 完整发布说明：[`docs/releases/v1.5.6-beta7.md`](docs/releases/v1.5.6-beta7.md)。

## [1.5.6-beta6] - 2026-06-02 - 3x-ui 迁移加固

- 修复重复导入循环：导入较大的 3x-ui 数据库耗时超过 Web 服务器的 30 秒写超时，
  因此响应在导入中途被切断——客户端收不到成功结果而重新提交，每次重试都会执行一
  次完整导入并再写一个 pre-import 备份。导入端点现在会解除该截止时间（仅在通过鉴
  权之后；实际工作仍受请求上下文限制），使客户端能收到结果，不再重复提交。
- 限制 pre-import 备份数量：仅保留最新的 10 个 `s-ui-pre-xui-import-*.db` 文件，
  缓慢或被重试的导入不再会塞满数据库目录。
- 恢复（Restore）现在会在前期就以明确提示拒绝 3x-ui / x-ui 数据库（“请使用
  Migrate from 3x-ui”），而不是稍后以晦涩的 `no such table: changes` 失败；架构迁
  移也能容忍缺失的 `changes` 表，使确实较旧的 s-ui 备份仍可恢复。
- “备份与恢复”对话框：明确 Restore 仅用于 s-ui 备份，并区分 3x-ui 快速导入与完整
  的审阅向导。
- 完整发布说明：[`docs/releases/v1.5.6-beta6.md`](docs/releases/v1.5.6-beta6.md)。

## [1.5.6-beta5] - 2026-06-02 - 3x-ui 迁移修复

- 修复内置 3x-ui 导入（`migrate-xui`）完全失败的 bug：dialect 硬编码了
  `all_time`（及 `last_online`）列，而原版 mhsanaei 3x-ui 和当前的归一化 fork
  都没有该列，因此任何真实导入都会在读取第一行之前以 `no such column: all_time`
  中止。现在源读取会感知实际存在的列（`tableColumns`/`selectColumns`，大小写不
  敏感），并为 fork 缺失的列填入默认值。已针对真实导出做端到端验证：所有
  inbound、客户端、WireGuard endpoint、reality TLS（去重）、路由与历史均可迁移。
- 修复会写入 s-ui 不识别的 3x-ui 键（`webBasePath`、`tgBotEnable`、`tgBotToken`、
  `tgBotChatId`、`tgRunTime`、`subEnable`）的设置迁移。现在键会映射到 s-ui 的规
  范名称（`webPath`、`telegram*` 等），映射也从 9 个扩展到 34 个设置（web/sub
  端点、显示开关，以及 Telegram bot 含 CPU 阈值、备份与代理）。源中没有 s-ui 对
  应项的设置会作为可见的、被跳过的 plan 项呈现，而不再被静默丢弃。
- 让跨主机/跨域名迁移变得安全：主机与域名相关的设置（监听地址、面板/订阅域名、
  磁盘上的 TLS 证书路径，以及内嵌主机的订阅 URL）在 plan 中默认跳过，以免覆盖目
  标服务器的有效配置而使其损坏；端口与路径仍会迁移，操作者可在复核步骤重新启用
  任意项。绑定到特定源监听地址的 inbound 现在会发出警告，提示它在目标主机上可能
  不存在。
- migrate-xui 向导现在默认包含设置、路由与历史；管理员导入仍为可选。
- 完整发布说明：[`docs/releases/v1.5.6-beta5.md`](docs/releases/v1.5.6-beta5.md)。

## [1.5.6-beta4] - 2026-06-02 - security & static-analysis hardening

- 将 backend 静态分析做到零告警（`staticcheck`、`golangci-lint`、`gosec`），并使
  `go vet`（nilness）、包含 `-race` 的完整 `go test ./...` 套件、`govulncheck`，
  以及前端 lint/typecheck/unit 套件全部通过。将已弃用的
  `sing/common/atomic.Int64` 迁移到 `sync/atomic`，将已弃用的
  `net.Error.Temporary()` 检查迁移为基于超时的检查，移除死代码，并将此前未检查
  的 error 返回值显式处理。
- APIv2：无效或过期的 bearer token 现在返回 HTTP `401 Unauthorized`，而不再是
  返回 HTTP `200` 并带有 `success:false` 的 body。浏览器 UI 不受影响，因为它在
  `/api` 上使用 cookie sessions，而非在 `/apiv2` 上使用 bearer token；外部 API
  使用方现在必须检查 HTTP status。
- 新增可选启用的 `sessionSameSiteStrict` setting（默认关闭），启用后签发的
  session cookies 带有 `SameSite=Strict`；拒绝 Telegram 代理 URL 中内嵌的凭据，
  以及可选 HTTP URL settings 中的 private/loopback/link-local/multicast IP 字面
  量；并加固恒定时间的 API-token-scope 比较，防止长度为 256 倍数时的截断。
- 在 settings 保存成功时发出 `settings_save_succeeded` audit event，并强制 backup
  -> restore 的表数量一致性，包括 `tls` 的 no-TLS 标记行。
- 完整 release notes：[`docs/releases/v1.5.6-beta4.md`](docs/releases/v1.5.6-beta4.md)。

## [1.5.6-beta3] - 2026-05-29 - admin management beta

- 在 Classic 与 Nexus 共用的 `/admins` 页面新增管理员创建和删除；两个操作
  都需要当前管理员密码。
- Backend 与 UI 都禁止删除当前登录管理员。删除其他管理员时会删除其 API
  token、重新加载 APIV2 token cache，并让被删除管理员的现有 browser session
  失效，因为 session validation 现在会检查用户是否仍存在。
- 新增 `admin_created` 与 `admin_deleted` audit events，`/api/users` 返回
  `isCurrent`，Nexus overview 也会映射新的 admin audit events。
- 完整 release notes：[`docs/releases/v1.5.6-beta3.md`](docs/releases/v1.5.6-beta3.md)。

## [1.5.6-beta2] - 2026-05-28 - sing-box 1.13.12 settings coverage beta

- 扩展 Classic 与 Nexus UI 中的 sing-box 1.13.12 设置覆盖；basics、rules、
  DNS、TLS、inbounds、outbounds、endpoints 和 services 复用同一组 advanced
  editor surfaces。
- 新增 `DomainResolveOptions` 编辑、route network presets、Dial/Listen/TUN
  advanced 字段、top-level certificate trust presets、rule route-options 的
  TLS fragmentation 控制、rule `client` matcher、HTTP/Mixed system proxy
  controls 以及 protocol-specific advanced options。
- Backend round-trip 会保留 top-level `certificate` 和未知 top-level
  sing-box config 字段，因此 runtime config 生成不再丢失 certificate trust
  设置。
- 保持 JSON 中不写入 default/no-op 值：`Off` 会删除字段，不写入 default
  delays 与 zero marks，拒绝空的 app/package selections，并保持
  `tls_record_fragment` 与 `tls_fragment` 互斥。
- 验证：本地通过 `npm run build`、`npm run test`、`npm run lint`、
  `go test ./...` 和 `go test -tags
  "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale"
  ./core`。

## [1.5.6-beta1] - 2026-05-27 - sing-box 1.13 UI parity beta

- 为 sing-box 1.13 TLS advanced 选项补齐一等 UI 支持，包括 curve
  preferences、client authentication/certificates、certificate public key pins
  以及 outbound kTLS controls。
- 修正 route/DNS rules 的 `interface_address` wire shape，并在 route rules、
  DNS rules 与 inline/source headless rule-set rules 中加入 network/Wi-Fi
  state matchers。
- 新增 inline rule-set editor、route `bypass` 序列化、route reject
  `reply`、Naive receive windows/UoT version selector、TUN reset mark/NFQUEUE、
  Tailscale advertise tags、OCM/CCM headers，以及 `oom-killer` service 的
  UI/backend 注册。
- 新增 representative sing-box 1.13 option-unmarshal 测试和 OOM service
  registry 回归测试。
- 验证：`npm --prefix frontend run build`、`npm --prefix frontend run test`、
  `npm --prefix frontend run lint` 和 `go test -tags
  "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale"
  ./...` 已在本地通过。

## [1.5.5] - 2026-05-26 - 1.5.5 稳定版

- 将 `v1.5.5-beta1` 到 `v1.5.5-beta4-hotfix2` 提升为稳定版 `v1.5.5`。
- 修复共享 VLESS UUID 与 Clash WebSocket Host 的订阅正确性：
  `xtls-rprx-vision` 不再导出到非 TCP 传输，Clash/Mihomo 导出会保留可用的
  `ws-opts.headers.Host`。
- 加强 backup export、restore 与 import rollback：no-TLS sentinel
  `tls.id=0` 会被安全保留，失败的 import 会重新打开 live DB，
  `settings.config` 对 DNS/routing restore 有覆盖，backup export 也不会再让
  sentinel 与真实 TLS 行冲突。
- 纳入 beta4 的 security/reliability hardening：导入管理员强制改密、更安全的
  token 处理、audit 优先级、大型 X-UI import plan 流式处理、rollback 后的
  realtime invalidation、可配置 SQLite pool、IP-monitor fail-closed 读取、
  bounded rate-limit state、realtime self-healing、retry/backoff 以及 data
  race 修复。
- 纳入 frontend hotfixes：npm lockfile、Playwright/Vite e2e 稳定性、
  reconnect chaos tests 以及 accessibility baseline timeout。
- Go 更新到 `1.26.3`，`github.com/sagernet/sing-box` 更新到 `v1.13.12`，
  release/Docker builds 使用的 cronet-go source pin 已同步。
- 验证：本地通过 `go vet ./...`、`go test -race -timeout=10m ./...`、
  release-tag `go build` 和 `git diff --check`。本地 workspace 没有 Docker；
  GitHub 会在 tag push 后运行 release/Docker workflows。

## [1.5.5-beta4-hotfix2] - 2026-05-26 - TLS sentinel 备份导出 hotfix

- **包含真实 TLS 行时的 backup 导出。**
  问题：no-TLS 哨兵行 `tls.id=0` 通过 GORM 普通 auto-increment create
  路径复制。若数据库中同时存在真实 TLS 行，SQLite 可能给哨兵行分配新的 id，
  随后复制真实行时触发 `UNIQUE constraint failed: tls.id`。
  影响：backup 导出现在会在通用表复制中跳过 `tls.id=0`，并通过
  `INSERT OR IGNORE` 显式恢复该哨兵行；no-TLS inbounds 仍保留有效父行，
  且不会与真实 TLS 配置冲突。
- 新增 regression coverage，覆盖同时包含 `tls.id=0` 和普通 TLS 行的数据库。
- 将 release metadata、README 安装示例以及手动 workflow 默认 tag 更新到
  `v1.5.5-beta4-hotfix2`。

## [1.5.5-beta4] - 2026-05-26 - 问题修复与技术债清理报告

### 1. 安全、认证与审计

- **导入时强制重置密码。**
  问题：从 x-ui 迁移管理员时，UI 提供 `reset_required` 模式，但 backend
  没有可持久化的强制改密状态，实际会落回生成新密码的场景。
  影响：用户模型新增 `force_password_reset` 状态，API 契约与 UI 对齐；该导入
  模式不再生成或暴露临时密码。
- **Token 抗攻击与旧 header Sunset。**
  问题：WebSocket token 检查存在可测量的 timing 差异，旧版 `Token` 授权
  header 没有强制停用日期，legacy API token 迁移也可能重新启用已禁用 token。
  影响：WebSocket token 消费路径改为更安全的 match-and-delete，legacy
  `Token` header 在 Sunset 后会被拒绝，token 迁移会保留原有 enabled/disabled
  状态。
- **减少系统数据泄露。**
  问题：system info 可能暴露 private/link-local server address，Telegram backup
  secret 需要更清晰的内存所有权，MigrateXui 中生成的管理员密码也太容易在屏幕上
  被看到。
  影响：system info 会过滤内部地址，Telegram backup payload/passphrase 使用后
  会清零，生成的管理员密码默认隐藏，只有显式 reveal 后才显示，并会自动清理。
- **审计优先级与信号质量。**
  问题：audit queue 压力较大时，warn/security 事件可能被普通 `info` 事件挤出；
  成功的 legacy secret decrypt 会产生噪声；stats commit 失败缺少持久记录；
  optional URL settings 也接受控制字符。
  影响：audit writer 会保留 warning/security 优先级，移除多余的 secretbox
  fallback 噪声，记录 stats commit failure，并拒绝 optional URL 中的控制字符和
  不安全输入形态。

### 2. X-UI 导入、同步与管理界面

- **尊重已保存的导入策略。**
  问题：后台 X-UI sync 使用硬编码行为，可能忽略 profile 中保存的 `OnlyNew`、
  settings/history/routing 导入以及管理员处理模式。
  影响：scheduler 会把保存的 import policy 传递给 plan/apply，因此 cron sync
  会按管理员选择的 profile 设置执行。
- **大型导入处理。**
  问题：迁移计划以前按普通 multipart field 读取，受 8 MiB 限制，较大的 panel
  不能通过同一 apply contract 导入；中断的上传也可能留下临时目录。
  影响：multipart `plan` 字段现在通过临时存储流式读取，并受 200 MiB 请求总限制；
  旧的 `xui-import-*` 临时目录会按安全的年龄规则清理。
- **导入隔离与报告准确性。**
  问题：replace 模式下的 TLS 删除错误可能在创建替换记录前被忽略，跳过的
  WireGuard endpoint 也会计入 skipped inbound。
  影响：TLS 删除错误会中止事务并安全回滚；导入报告现在单独统计 skipped endpoint。
- **Rollback 与恢复 UX。**
  问题：apply error 可能只把用户送回上一步而没有上下文，rollback 使用固定 1 秒
  delay 后 reload，其他在线会话也收不到配置已恢复的实时通知。
  影响：MigrateXui 会 inline 显示 apply error，rollback reload 前会等待
  health check，backend 在成功 rollback 后发布 `config_invalidated`。

### 3. 数据库、备份与恢复能力

- **备份与迁移安全。**
  问题：SIGHUP timeout 固定为 3 秒，WAL checkpoint 在 SQLite DB 被锁时可能中止
  backup，缺少 `settings.config` 会阻塞 versioned restore，post-migration adapt
  失败也只记录 warning。
  影响：timeout 可通过环境变量配置，WAL checkpoint 会从 `TRUNCATE` fallback 到
  `FULL`，缺少 `settings.config` 的 backup 会以 warning 恢复，post-migration
  adapt 损坏会阻止启动。
- **数据库扩展性与首次启动竞态。**
  问题：SQLite pool limits 固定，并发首次启动可能创建重复默认 settings。
  影响：SQLite pool limits 可通过环境变量调整，默认 settings 通过数据库层面的
  幂等 insert path 创建。
- **IP monitor fail-closed 行为。**
  问题：IP-monitor 路径中的短暂数据库读取错误可能在 enforce mode 下放行未知地址。
  影响：`client_ips` 读取失败会让 cache entry 被视为不可信，并切换到 fail-closed
  enforcement。

### 4. 网络、数据竞态与核心稳定性

- **OOM 防护与 realtime 自恢复。**
  问题：import-xui rate-limit state 在大量唯一 IP 请求下可能无限增长，frontend
  在短暂网络故障后也可能停留在 degraded polling mode。
  影响：rate-limit cache 使用 bounded eviction 与 expired-bucket cleanup，WebSocket
  runtime 会在 fallback mode 中主动尝试 healing reconnect。
- **Data race 修复。**
  问题：core restart timer、Telegram HTTP client 与 token-use flush 的并发访问
  可能触发 race detector failure、panic，或通过过期 DB handle 写入。
  影响：关键路径使用 mutex、single-flight 与 barrier 机制保护，token-use flush
  lifecycle 与 database reset 和 API test lifecycle 同步。
- **更智能的重试与风暴保护。**
  问题：cron sync retry 过于激进，token-use write failure 缺少 backoff circuit，
  update check 没有 ETag cache，sync error reason 被压平，WARP authorization
  headers 分散在脆弱路径中。
  影响：retry policy 使用 exponential backoff，token-use flush 有 circuit breaker，
  release checks 使用 `If-None-Match`，sync-failure summary 包含 sanitized error
  class/detail，WARP authorized headers 已集中处理。
- **IPv6-safe system info 与共享 API route registry。**
  问题：system info 在短 interface flag/address 数据上可能 panic，包括特殊的
  IPv6-only 环境；import-xui routes 也可能在 v1/v2 API 注册之间漂移。
  影响：网络接口按内容和长度检查，import-xui endpoints 从同一个 route spec 注册到
  `/api` 与 `/apiv2`。

## [1.5.5-beta3] - 2026-05-22 - DNS 与路由 backup config 的恢复安全性

- 保存 config 时现在会补回缺失的 `settings.config` 行；restore 会拒绝已经
  丢失该 sing-box config 的 versioned S-UI 数据库备份，而不是成功导入一个
  没有 DNS 和路由规则的数据库。
- 新增 restore 回归覆盖，确认导出并重新导入 `settings.config` 后 DNS
  服务器和路由规则仍然保留。
- Release、Windows 与 Docker workflow 的默认 tag 更新为
  `v1.5.5-beta3`。

## [1.5.5-beta2] - 2026-05-22 - no-TLS inbound 的备份恢复安全性

- 备份导出现在会显式保留 no-TLS inbound 的 `tls(id=0)` 哨兵行，确保
  `tls_id=0` 的 foreign key 在备份中仍然有效。
- Restore 会在 migration foreign-key check 之前补回该 no-TLS parent，
  因此这个 prerelease 之前生成的备份不再应因
  `Foreign key check failed: inbounds=1` 被拒绝。
- 当数据库导入失败时，rollback 后会重新打开 live DB，而不是让运行中的
  panel 持有已关闭的 DB handle；SQLite sessions 在 swap 后跟随当前 DB，
  settings 在 DB 短暂不可用时也会返回错误而不是 panic。
- 新增 regression coverage，覆盖 no-TLS backup foreign key、migration
  sentinel repair，以及 restore 被拒绝后的 rollback/reopen。
- Release、Windows 与 Docker workflow 的默认 tag 更新为
  `v1.5.5-beta2`。

## [1.5.5-beta1] - 2026-05-22 - 共享 VLESS UUID 与 Clash WS Host 的订阅正确性

- 当同一个 client UUID 被多个 VLESS inbound 共用时，将
  `xtls-rprx-vision` flow 从非 TCP 传输中剥离。涉及面板 sing-box
  config（`fetchUsersByCondition`）、JSON 订阅
  （`sub/jsonService.go`）以及可分享链接（`vlessLink`）。与
  Xray-core 仅允许在 TCP 上使用该 flow 的契约一致，因此 TCP+REALITY
  inbound 与 gRPC+TLS（或 WS）inbound 可以共用同一个 UUID，不再破坏
  非 TCP 一侧（alireza0/s-ui#1127）。
- 修复 Clash `ws-opts.headers` 不再丢失 WebSocket `Host`。之前对 map
  结构的 header 使用 `[]interface{}` 类型断言会静默丢弃 header，导致
  Mihomo 经过严格 CDN / Nginx 上游时握手失败。当未显式设置 Host 时，
  导出器现在会回退到 TLS `server_name`，确保上游看到的 Host 与 SNI
  匹配（alireza0/s-ui#1126）。
- 在 `service/inbounds_vless_flow_test.go`、
  `util/genLink_vless_flow_test.go` 与
  `sub/clashService_ws_host_test.go` 中新增 regression coverage。
- Release、Windows 与 Docker workflow 的默认 tag 更新为
  `v1.5.5-beta1`。

## [1.5.4] - 2026-05-22 - stable Nexus UI line + localization cleanup

- 将 `1.5.4-beta1` 到 `1.5.4-beta5` 提升为稳定版 `1.5.4`。
- 稳定版包含 beta 线中的 opt-in Nexus UI mode、取消重复 read toast 的 hotfix、
  更紧凑的 Nexus Overview、systemd installer secretbox key bootstrap，以及
  reserved `/ws` path segment 边界修复。
- 完成发布前的本地化收尾：Persian Telegram、Audit、maintenance、backup 与
  IP-limit 字符串；Vietnamese 在 Telegram、Audit、settings、networking、DNS、
  TLS、rules 与 stats 的机器翻译清理；Simplified/Traditional Chinese 剩余的
  maintenance path 字符串；以及 Russian 术语润色。
- Release、Windows 与 Docker workflow 的默认 tag 更新为 `v1.5.4`。

## [1.5.4-beta5] - 2026-05-22 - reserved path prefix hotfix

- 对没有尾随 `/` 的 framework path，reserved path validation 现在按路径
  segment 边界匹配，而不是按任意字符串前缀匹配。
- `/wsub/` 这类自定义 path 不再与保留的 `/ws` 冲突；`/ws`、`/ws/`
  以及 `/ws/` 下的子路径仍会被阻止。
- 新增 `/ws` boundary behavior 的 regression coverage，覆盖保存 panel
  与 subscription path settings 的校验规则。
- Release、Windows 与 Docker workflow 的默认 tag 更新为
  `v1.5.4-beta5`。

## [1.5.4-beta4] - 2026-05-22 - installer secretbox key bootstrap

- 通过 `install.sh` 的 systemd 安装现在会在没有 installer-managed key
  时为加密设置生成稳定的 `SUI_SECRETBOX_KEY`。
- 新生成的 secretbox key 在安装时仅显示一次，保存到 root-only
  `/etc/s-ui/secretbox.env`，并通过 installer-owned systemd drop-in 加载。
- 升级会保留现有 installer-managed key 而不会轮换；uninstall 会随
  systemd install state 一起移除该 drop-in。
- 已为 systemd users 记录 installer-managed key 路径及保留要求。
- Release、Windows 与 Docker workflow 的默认 tag 更新为
  `v1.5.4-beta4`。

## [1.5.4-beta3] - 2026-05-22 - Nexus Overview density refinement

- Nexus dark surface palette 改为更深的 navy 基调，并拆分 teal/violet
  accents；classic themes 保持不变。
- 移除 standalone Traffic overview panel 与重复的 Health KPI，同时保留
  Live traffic KPI spark，并改用紧凑的 live status sample window。
- Overview 改为更紧凑的三列 primary row，并压缩 Top clients、Recent
  events 与 Protocol summaries，使 dark LTR `en` dashboard 可在
  `1440x900` 单视口内展示。
- 该 refinement 保持 frontend-only：没有 backend/API/CSRF/CSP drift，
  也没有 runtime/dev dependency 变更。
- 已通过 frontend test/lint/build gates、Nexus source/build artifact
  external-origin gates、`TestAdminSecurityHeaders`，以及 LTR `en` 与
  RTL `fa` 在 desktop、narrow desktop、tablet、mobile 宽度下的 viewport
  coverage。
- Release、Windows 与 Docker workflow 的默认 tag 更新为 `v1.5.4-beta3`。

## [1.5.4-beta2] - 2026-05-21 - Nexus Overview cancel toast hotfix

- 取消重复 frontend read 请求时不再显示 failed notification。Nexus Overview
  启动时可能触发重叠 dashboard reads，共享 axios dedupe 会按设计取消较旧
  请求；现在该正常取消不会再以 `CanceledError: canceled` toast 呈现。
- 新增 frontend regression coverage，确保 cancel 静默处理，同时真实 request
  error 仍会显示 failed notification。
- Release、Windows 与 Docker workflow 的默认 tag 更新为 `v1.5.4-beta2`。

## [1.5.4-beta1] - 2026-05-21 - Nexus UI mode opt-in beta

- 新增可选的 `nexus` UI mode，与现有 `classic` 界面并存。Classic 仍为默认，
  Nexus 作为每个浏览器自己的 localStorage 偏好保存。
- 新增 UI mode contract、`VITE_ENABLE_NEXUS` kill switch、CSP-safe pre-mount
  anti-FOUC bootstrap、authenticated layout host、模式切换控件以及 Nexus
  本地化字符串。
- 新增 Nexus shell、响应式 sidebar/topbar、RTL `fa` 支持、Nexus design
  tokens/themes，以及基于现有数据源构建的固定 Nexus Overview dashboard。
- 保持 backend/API/CSRF/CSP surface 不变：没有新增 endpoint、没有新增
  WebSocket flow、没有 inline script、Nexus source 中没有 external-origin
  literal，也没有 runtime/dev dependency 变更。
- 该 beta 已通过 `npm run test`、`npm run lint`、`npm run build`、
  external-origin gates、supply-chain invariance，以及 LTR `en` 和 RTL `fa`
  在 desktop、narrow desktop、tablet、mobile 宽度下的 Nexus viewport checks。
- Release、Windows 与 Docker workflow 的默认 tag 更新为 `v1.5.4-beta1`。

## [1.5.3] - 2026-05-21 - 稳定版 + Telegram 备份频率 UX

- 将发布线从 `1.5.3-beta` 提升为稳定版 `1.5.3`。
- Telegram 数据库备份频率现在可通过预设和自定义分钟/小时间隔配置，
  同时继续写入现有 `telegramBackupCron` 设置。
- 已保存的自定义 cron 表达式仍可通过 Advanced cron 模式继续编辑。
- Release、Windows 与 Docker workflow 的默认 tag 更新为 `v1.5.3`。

## [1.5.3-beta] - 2026-05-20 - 聚合修复 + 上游对齐 (#1114)

### Multi-chat 交付总览 (P0-P5)

#### 安全性

- [P0] 强化 SSRF 过滤与 dial 阶段二次地址校验；收紧备份恢复时的
  路径/符号链接校验。
- [P1] 强化 CSRF/session 生命周期，包括 logout/logout-all 后 token 更新，
  以及更严格的 WS token 处理。
- [P2] 扩展 secret/settings 安全检查与迁移护栏。
- [P3] 增加 listen fallback 审计，并统一 restart 路径的一致性。

#### 可靠性 / 数据完整性

- [P0] 修复 tracker/session options/audit writer 相关竞态路径。
- [P1] 稳定 realtime fallback 行为与前端单元测试 harness。
- [P2] 增加 reset hooks、tracker wait guards 与 foreign-key 迁移检查。
- [P3] 统一重启调度，并通过初始 DI 切片减少全局副作用。
- [P4] 将剩余 service runtime globals 移入 DI-compatible runtime，同时保留
  zero-value service 兼容性。
- [P5] 完成 logging backend 清理，且不改变 API endpoint 行为。

#### API 与运行时行为

- [P0] 强化 trusted proxy 解析与导入错误分类安全性。
- [P1] 收紧 realtime/session/CSRF 流程与 Telegram 错误分类。
- [P2] 统一重负载数据路径中的 batching 与 timeout 行为。
- [P3] 增加初始 `slog` adapter 路径，支持从 `op/go-logging` 渐进迁移。
- [P4] 将 `slog` 提升为 logger facade；`op/go-logging` 仅保留在 deprecated
  compatibility API 后方。
- [P5] 移除 deprecated `logger.InitLogger`/`logger.GetLogger`；logger facade
  输出完全迁移到标准库 `log/slog`，并保留 panel/core log-buffer。
- [P5] 从 `go.mod` 与 `go.sum` 移除 legacy 模块
  `github.com/op/go-logging`。
- [P4] 为 `github.com/sagernet/sing-box v1.13.11` 增加可检查的 tracker
  revalidation policy。
- [P4] 增加可检查的 SemVer release/version policy，并防止 migration code
  降级未来的 `settings.version`。

#### 前端

- [P1] 修复 `frontend/vitest.config.ts` 中的 Vitest harness 配置。
- [P1/P2] 对齐 CSRF 缓存清理、请求去重边界与 realtime degraded-mode 行为。

#### 测试与验证

- Baseline 与分阶段报告：
  - `plans/lint-baseline.txt`
  - `plans/lint-baseline-normalized.txt`
  - `plans/fix-validation.txt` (P0)
  - `plans/p1-validation.txt` (P1)
  - `plans/p2-validation.txt` (P2)
  - `plans/p3-architecture-validation.txt` (P3)
  - `plans/p4-architecture-debt-validation.txt` (P4)
  - `plans/p5-logging-cleanup-validation.txt` (P5)
- 每个阶段都包含定向检查与最终命令通过集，详见对应验证文件。

### 可追溯性 (multi-chat policy)

- 每条完成项都加阶段标签：`[P0]`、`[P1]`、`[P2]`、`[P3]`、`[P4]`、`[P5]`。
- 按统一格式追加引用：`(ref: <commit|PR|chat-id>)`。
- 跨阶段事项使用组合标签，例如 `[P1/P2]`。
- 延后架构项单独维护，不与已完成项混写。

### 升级提示（聚合窗口）

- 将 P0->P5 视为一个发布窗口；升级前先做完整 SQLite 备份。
- 在生产发布前先于 staging 验证 session/CSRF/realtime 与 listen fallback
  行为变化。
- 以上分阶段 validation 文件可作为升级验证证据。
- 如果外部 Go 集成曾导入 `logger.InitLogger` 或 `logger.GetLogger`，需要迁移到
  `logger.Init(logger.Level*)`、`logger.Slog(source)` 或 `slog.Default()`。

### 回滚（聚合窗口）

- 回滚时恢复窗口前 SQLite 快照与旧二进制/镜像。
- 若回滚跨越 session/token 行为变更，降级后应失效当前活跃会话并轮换
  管理员凭据。

### 延后架构债务

- [P5] P5 范围内没有 deferred 项。legacy `op/go-logging` 依赖与 deprecated
  logger compatibility API 已移除。

### 后续 multi-chat 发布模板

- 固定使用域分组：Security、Reliability/Data integrity、API/Runtime、
  Frontend、Tests。
- 每条变更都加 phase 标签并追加 traceability 引用。
- 对聚合窗口明确提供 `Upgrade notes` 与 `Rollback`。

### 修复

- TUIC 订阅/分享链接与 Clash 导出现在会包含 `udp_relay_mode`，保留已配置的
  值，并在生成链接未设置时默认使用 `quic`。

### 新增

- 支持定时与手动将 SQLite 数据库加密备份到 Telegram。备份密码短语只在
  Telegram 标签页配置，功能默认关闭。新增设置及默认值：
  `telegramBackupEnabled="false"`、`telegramBackupPassphrase=""`、
  `telegramBackupCron=""`、
  `telegramBackupExcludeTables="stats,client_ips,audit_events,changes"`、
  `telegramBackupMaxSizeMB="45"`。新增手动触发路由：
  `POST /api/telegram/backup/run` 与 `POST /apiv2/telegram/backup/run`。
- Restore 现在会自动识别上传文件中的 `SUI-TGBKP\x00` backup envelope，
  并在 Backup & Restore 中显示 Backup passphrase 字段。明文 `.db`
  上传仍然不需要该字段。
- 现有 Backup 按钮可通过「Encrypt with Telegram backup passphrase」
  复选框可选下载同一加密 envelope。复选框默认未选中，明文下载行为保持
  不变，现有 `getdb` 端点使用新的非破坏性 query 参数
  `encryptTelegramBackup=true`。
- 主发布二进制现在包含 `s-ui decrypt-backup`，可用于离线解密 envelope。
  不需要单独的 artifact。
- `docs/scope-matrix.md` 现在记录 `tg_backup_run` 操作。

### 变更

- BREAKING：旧版 `POST /api/telegram/backup` 与
  `POST /apiv2/telegram/backup` 现在委托给新的 Telegram backup service。
  所有响应都移除了 `backupKey`，要求 `telegramBackupEnabled=true`，
  成功响应新增 `trigger="manual"`。没有兼容过渡期。严格迁移步骤：
  升级后在 Telegram 标签页启用 `telegramBackupEnabled`，否则旧版调用会
  返回 HTTP 503，`errorClass=disabled`。
- `util/secretbox` 新增 `EncryptBytes` 与 `DecryptBytes`，用于按字节处理
  secret。
- `api/rateLimit.go` 新增一个由四个手动触发路由共享的 Telegram backup
  限速 bucket：60 秒内 3 次，并返回 `Retry-After`。
- 新增 audit event 类型：`tg_backup_sent`、`tg_backup_failed`、
  `tg_backup_passphrase_changed`、`tg_backup_manual_encrypted`、
  `tg_backup_restore_failed`。

### 升级提示

- 升级前请备份 SQLite 数据库。如果使用 systemd，请先 `systemctl stop s-ui`，
  复制 `s-ui.db` 以及任何 `-wal`/`-shm` 旁车文件，再启动服务。
- Telegram database backup 会保持关闭，直到在 Telegram 标签页启用
  `telegramBackupEnabled` 并配置 Backup passphrase。
- 调用旧版 Telegram backup 端点的集成需要处理已移除的 `backupKey` 字段，
  以及在启用设置前新的 HTTP 503 `disabled` 响应。

### 回滚

- 如需回滚，请恢复升级前的 SQLite 备份，并切回之前的二进制或镜像。
- 加密的 `.db.aes` 文件仍可用创建它们时的 passphrase 解密；任何包含
  `s-ui decrypt-backup` 的二进制都可以执行。

## [1.5.2-beta-hotfix2] - 2026-05-18 - 移除 client_ips 旧版唯一索引

### 修复

- 在 3x-ui 迁移前的自动备份过程中出现的
  `UNIQUE constraint failed: client_ips.client_name, client_ips.ip`。
  自 1.5.x 起 `client_ips.ip` 仅作为旧版 backfill 字段，新行为空；真正
  的唯一键是 `(client_name, ip_hash)`。模型上仍保留着过期的
  `gorm:"index:idx_client_ips_client_ip,unique"`，导致
  `database/backup.go` 通过 `AutoMigrate` 在临时备份库中重建了这个坏
  索引，于是当某个客户端在 `client_ips` 中存在多行 `ip` 为空的记录时，
  分块复制就会失败。该 hotfix 之后，模型上唯一的唯一索引为
  `(client_name, ip_hash)`。

### 变更

- `database/model/model.go` — 从 `ClientIP.ClientName` 与
  `ClientIP.IP` 上移除了
  `idx_client_ips_client_ip,unique` 标签。
- `cmd/migration/1_5.go` — `1.5` 分支的 schema 迁移会删除过期的
  `idx_client_ips_client_ip`，并创建部分非唯一索引
  `idx_client_ips_client_legacy_ip ON client_ips(client_name, ip)
  WHERE ip IS NOT NULL AND ip != ''` 以便保持旧版查询性能。该迁移
  完全幂等（`DROP INDEX IF EXISTS` / `CREATE INDEX IF NOT EXISTS`）：
  已经升级到 `1.5.2-beta` 的部署在下次启动、迁移 runner 重新进入
  `1.5` 分支时会再次干净地运行它。
- `database/db.go: ensureIndexes` — 在每次 `InitDB` 时也会删除该旧版
  唯一索引。这为绕过 `MigrateDb` 的场景（例如在面板外恢复旧版备份）
  提供运行时兜底，并确保 `GetDb("")` 构建的临时备份库不会再带上这
  个坏索引。

### 备注

- 没有新增列、表、设置、接口、scope 或环境变量。与上一 hotfix 中的
  分块备份 helper 一并生效。
- 回归测试：
  - `cmd/migration/migration_1_5_test.go` 在 `to1_5` 重新创建过期索引
    时失败，并验证一个客户端可以拥有多行空 `ip`。
  - `database/db_test.go: TestInitDBDropsObsoleteClientIPUniqueIndex`
    在已存在旧版唯一索引的旧形式数据库上启动 `InitDB`，并验证它将
    其移除。
  - `database/backup_test.go: TestGetDbHandlesHashedClientIPsWithEmptyLegacyIP`
    通过 `GetDb("")` 完整转移同一客户端下多行 `ip_hash` 且 `ip` 为空
    的记录。

## [1.5.2-beta-hotfix] - 2026-05-18 - 备份分块与 SPA 升级安全

### 修复

- 在 `stats`、`client_ips`、`audit_events`、`changes` 或 `clients`
  表较大的部署上执行数据库备份与 3x-ui 迁移时出现的
  `too many SQL variables` 错误。`database/backup.go` 的备份不再产生
  单条超过 SQLite 编译期限制（`mattn/go-sqlite3` 中的
  `SQLITE_MAX_VARIABLE_NUMBER = 999`）的多行
  `INSERT VALUES (...)`。这同时解除了 `WritePreImportBackup` 与
  3x-ui 迁移在真实生产规模数据库（`stats` 行数 ≈ 40k+）上的阻塞。
- 升级后浏览器残留的旧 `index.html` 不再使「Clients」页面卡住。
  `/<base>/assets/*` 对缺失文件返回真实的 404，不再回退到 SPA
  fallback，因此浏览器不会再对 JS 模块请求收到 `text/html`，避免出现
  `Failed to load module script` / `Failed to fetch dynamically imported
  module`。`index.html` 现在以 `Cache-Control: no-cache, no-store,
  must-revalidate` 提供；带哈希文件名的资源仍为
  `public, max-age=31536000, immutable`。
- Vue Router 监听 `vite:preloadError`，在动态导入因旧 chunk 哈希而失败
  时执行一次受保护的 `window.location.reload()`（`sessionStorage`
  标记防止循环刷新），让仍打开旧版本的页签自动加载新构建。
- `service/client.go`（`addbulk`、`editbulk`、`ResetClients`、
  `DepleteClients`）以及 `database/importxui/history_routing.go`
  （历史流量导入）通过新的 `database/bulk.go`
  helper（`SafeSQLiteBatchSize`、`CreateInBatchesSafe`、
  `SaveInBatchesSafe`）将批量 `Save`/`Create` 切成小批。Reset/deplete
  任务和历史 `stats` 导入在拥有数千客户端的部署上不再失败。

### 备注

- 没有架构迁移、新增接口、scope 或环境变量。
- `database/backup_test.go` 新增回归用例：约 43k 行 `stats` 加 5k
  `client_ips`，验证 `GetDb("")` 能完整往返迁移所有行。

## [1.5.2-beta] - 2026-05-18 - 3x-ui 迁移套件

### 新增

- 3x-ui 配置导入：`s-ui import-xui` 命令行、`POST /api/import-xui`
  HTTP 接口，以及备份与恢复（Backup & Restore）弹窗中独立的
  「Migrate from 3x-ui」部分。导入在单一事务中执行，自动备份，支持
  `merge`/`replace`/`skip` 三种策略，并写入 `xui_import` 审计事件。
- 位于 `/migrate-xui` 的完整迁移向导：按对象 plan/apply，校验
  `Source.Hash`，通过 WebSocket 推送 `xui_import_progress` 事件，
  JSON 预览，回滚到自动备份，并支持 JSON/Markdown 报告下载。报告
  仅保存在 `audit_events.details`。
- 远程 3x-ui 数据源：`--remote ssh://...` 与 `--remote http://...`
  （xuihttp），以及用于增量定时同步的 `s-ui sync-xui` 子命令。SSH
  使用 host-key TOFU 与 `xui_known_hosts` 表；HTTP 支持 3x-ui 的
  登录流程。
- 加密的 `xui_sync_profiles`（AES-GCM，密钥来自基于
  `config.GetSecret()` 的 HKDF-SHA256，可通过 `XUI_PROFILE_KEY_FILE`
  覆盖），架构迁移 `cmd/migration/1_7.go`，cron 任务 `xuiSyncJob`
  以及用于管理同步配置的 `/migrate-xui/schedule` 页面。
- 历史流量的尽力而为导入（`client_traffics`/`outbound_traffics`
  → `stats` 聚合）以及 Xray routing 规则导入（`geosite:*`/`geoip:*`、
  block、direct）转换为 sing-box `route.rules`/`dns.servers`。
  Balancers 仅作为警告输出。
- 新增 `xui_remote` token scope，所有远程/同步接口都要求该 scope；
  本地 `/api/import-xui*` 接口保持 `database`/`admin`。
  `XUI_DISABLE_REMOTE=1` 可关闭远程数据源与 cron 模式。

### 注意

- `test-db/` 包含本地 3x-ui 导入用的真实生产数据，已不再纳入仓库
  （见 `.gitignore`）。依赖该目录的测试会在 CI 上自动跳过；本地运行时
  请确保 `test-db/` 中存在所需 fixture。

## [1.5.1-beta] - 2026-05-17 - 修复加固与 UI 完善

### 安全性

- Telegram 通知改用带界限的异步队列，配合 retry/backoff 与可审计的
  overflow/failure 事件，因此登录及其它处理逻辑不再因 Telegram 网络故障
  而被阻塞。
- Telegram 事件 payload、审计详情、changes 历史以及备份 caption 都会经过
  redaction：bot token、proxy 凭据、API token、备份密钥不会写入日志、审计、
  changes 或 caption。
- Realtime WebSocket 握手强制执行 Origin allow-list、按 IP 的握手限速、
  一次性 token 防重放、ping/pong 心跳、idle 关闭以及 session 轮换时的
  close-all 语义。
- `GET /api/security/audit` 对 API token 请求要求 admin scope，并增加端点
  限速、cursor 分页、`event`/`severity` 过滤的合法性校验。
- `POST /api/telegram/test` 对 API token 请求要求 admin scope，写入的审计
  事件只包含 `success`/`errorClass` 元数据。
- 为面板和订阅服务器新增了 security headers 中间件，订阅响应使用
  `Cache-Control: no-store`。
- 全新安装生成的管理员密码不再写入应用日志；密码只会保存到
  `<dataDir>/initial-admin.txt`，文件使用仅所有者可读写的权限，启动输出中
  只包含文件路径。
- `s-ui admin -show` 不再输出已存储的密码 hash；现在只显示 username
  和重置密码的提示。
- 前端会在 logout、logout-all 以及 realtime session-rotation close 后清除
  缓存的 CSRF token，下一次变更请求会重新获取 token。
- `install.sh` 现在会下载 release 中的 `*.sha256` 文件，并在解压前通过
  `sha256sum -c` 校验 Linux tarball。
- 新增 PR CI workflow，会运行 Go vet/race tests 以及前端 lint/unit/build
  检查。
- 管理员 Web session 现在使用 SQLite 后端的服务端存储；浏览器 cookie
  只包含已签名的 session ID，session 数据保存在本地 `sessions` 表中。

### 隐私与订阅

- 客户端 IP 历史默认以加盐 SHA-256 哈希存储，未显式开启时不展示原始 IP，
  保留期由 cron GC 维护。
- IP 限制默认仍为 `monitor` 模式；`enforce` 模式只会拒绝新的超限连接，
  不会断开已有会话。
- 设计中的所有订阅设置均已持久化，并在 link、JSON、Clash 订阅响应中实际
  生效。订阅路径会按保留前缀校验，header 经过统一净化，按 IP 的订阅限速
  可配置。
- `POST /api/rotateSubSecret` 用于轮换每个客户端的订阅 secret，并写入审计
  事件。当 `subSecretRequired=true` 时，旧的按名称的 URL 返回 404。

### Telegram 与可观测性

- Telegram egress 可使用受校验的 HTTP/HTTPS/SOCKS5 代理设置，相关凭据按
  secret-aware 方式存储。错误类别归一为 `unauthorized`、`chat_not_found`、
  `rate_limited`、`network`、`unknown`。
- 已实现 CPU 滞后告警、计划性 Telegram 报告以及加密的 Telegram 数据库
  备份导出，所有功能保持 opt-in。
- 可观测性历史改为受界限的桶 (`2s`、`30s`、`1m`、`5m`)，由 cron 采样，
  API 参数 `metric`/`bucket`/`since` 均经过校验。
- `GET /api/logs` 接受受界限的 `count`、`level`、`source` 与子串
  `filter`；`GET /api/version` 执行 fail-soft 的 1 小时缓存 GitHub release
  检查。
- 数据库导入/导出现支持 64 MiB 上限、SQLite magic 校验、临时 staging、
  只读 `PRAGMA integrity_check` 以及审计事件。

### 前端

- 新增 realtime 前端 store，包含 websocket 重连/降级状态以及 polling
  回退。
- 新增 secret-aware 设置字段，显示 `••• stored •••` 占位符，且不会把
  占位符当作 secret 提交。
- 新增 IP 历史 modal，原始 IP 默认遮蔽，向管理员展示原始 IP 前需要确认。
- 新增 Telegram 设置与 Audit 视图。Audit 视图使用 cursor 分页与服务端
  `event`/`severity` 过滤。

### 打包与 CI

- Docker 构建现在包含与 `release.yml` 同步的 `CRONET_GO_VERSION` 参数，并
  记录在缺少按 commit 发布的上游资产时，临时回退到带日期说明的最新
  prebuilt `libcronet` 资产。
- Docker 镜像默认 `TZ` 现在与面板默认值 `Europe/Moscow` 保持一致。
- 手动 release workflow 现在默认使用 tag `v1.5.1-beta`。
- 容器 entrypoint 不再在启动前重复执行自动迁移；需要手动只迁移运行时可使用
  `SUI_MIGRATE_ONLY=1`。
- 迁移 runner 现在只会在事务成功 commit 后执行 SQLite WAL checkpoint，修复
  从 `1.4.x` 升级到 `1.5.1-beta` 时可能出现的 `database table is locked`。
- 管理员前端不再依赖用于 base path 的 inline script，因此严格的 Content
  Security Policy 可以生效，自定义 web path 也会正确用于 API、CSRF 和
  realtime fallback 请求。

### 测试

- 为 secret 设置迁移、redaction、IP 监控缓存/enforce 行为、审计过滤与
  限速、订阅 header 注入与 legacy URL 404 行为、realtime Origin/replay
  token/heartbeat、迁移以及前端 websocket/IP helper 增加或扩展了回归
  覆盖。
- 当前工作目录中已通过：`go vet ./...`、`go test ./...`、
  `npm run test:unit`、`npm run build`、`npm run lint`。Race 测试需要
  CGO 和 C 编译器，本机 Windows 工作目录目前缺少 `gcc`。

### 升级提示

- 升级前请备份 SQLite 数据库。如果使用 systemd，请先 `systemctl stop s-ui`，
  复制 `s-ui.db` 以及任何 `-wal`/`-shm` 旁车文件，再启动服务。
- 旧版 `/apiv2/*` `Token` header 仍可用，但属于过渡期。请在 Sunset 之前
  将客户端切换到 `Authorization: Bearer <token>`：
  `Sat, 15 Aug 2026 00:00:00 GMT`。
- 除支持 polling 回退的 realtime websocket 与 monitor-only IP 跟踪外，
  其它新功能默认关闭。

## [1.5.0] - 2026-05-15 - 安全基线与 realtime 平台

### 安全性

- 在 Admins 面板中新增「一次性失效所有管理员 web 会话」操作。该操作会轮换
  session generation 并清除发起者自己的 cookie；API token 不会被吊销。
- 新增基于 AES-GCM/HKDF 的 secretbox 助手用于敏感设置。新的 secret-aware
  设置在设置了 `SUI_SECRETBOX_KEY` 时使用该 key 加密，否则使用旧的
  `settings.secret` 兼容 key 并在启动时给出告警。
- secret-aware 设置在 `api/settings` 中以 `<key>HasSecret` 形式遮蔽；保存
  空值会保留之前存储的 secret。
- 新增 `audit_events` 表、redaction 助手、保留期设置以及
  `/api/security/audit` 端点。登录、登出、logout-all-admins、修改凭据、
  创建/删除 API token 等动作会写入经过 redaction 的审计事件。
- 为浏览器 `/api/*` 写操作添加了 CSRF 防护。`GET /api/csrf` 颁发与会话绑定
  的 token，前端通过 `X-CSRF-Token` 提交，无效或过期时返回 HTTP 403。
  `/apiv2/*` 的 Bearer token 请求不受影响。
- API token 已从明文迁移为使用每实例 `installSalt` 的 salted SHA-256
  哈希；新 token 仅展示一次，DB 仅保存 hash 与 prefix，可在 Admins UI 中
  启用或禁用。
- `/apiv2/*` 现在以 `Authorization: Bearer <token>` 作为主要的 API token
  传输方式。旧的 `Token` header 仍可用，会写入审计事件，并返回
  `Deprecation` 与 `Sunset: Sat, 15 Aug 2026 00:00:00 GMT`。
- 新增按客户端的订阅 secret，支持 `/sub/<secret>`、`/sub/json/<secret>`、
  `/sub/clash/<secret>`、`/json/<secret>`、`/clash/<secret>` 路由；旧的
  `/sub/<name>` 在 `subSecretRequired=true` 之前仍可用。
- 订阅端点会净化响应 header、校验配置的订阅路径，并按 IP 进行限速。

### API

- 在保留原有一层 `/api/<action>` 端点的同时，新增了用于 1.5.0 安全、
  通知、可观测性、批量出站检查的 grouped 路由占位。
- 新增 `GET /api/observability/history`、
  `GET /api/observability/core-history`、`GET /api/version`。
- 新增 `POST /api/checkOutbounds` 用于受界限的批量出站检查：并发 8、
  单出站超时 5s、整体超时 60s、并配有 HTTPS/公网 IP 目标校验器。
- 新增默认关闭的 Telegram 通知服务以及 `POST /api/telegram/test`。Bot
  token 与代理相关设置均为 secret-aware；登录、logout-all-admins、core
  重启事件仅在显式开启 Telegram 时才会通知。
- 新增带身份认证的 realtime WebSocket 基础设施，路径为
  `/api/realtime/ws-token` 与 `/api/realtime/ws`，使用一次性 token、
  受界限的客户端队列、按用户/按 IP 的连接数上限以及前端 polling 回退。
  `logoutAllAdmins` 会以 close code `4401` 关闭活跃 realtime socket。
- 新增批量客户端 IP 监控 `client_ips`，支持按客户端的 `limitIp` 与
  `ipLimitMode`、last-online/IP 数量元数据、Admins 中可审计的清除动作以及
  Clients UI 控件。`monitor` 是默认模式；`enforce` 仅拒绝新的超限连接，
  不会断开已建立的连接。

### 本地化

- `install.sh` 与 `s-ui` 管理菜单也将中文作为 **3. 中文** 选项提供；
  `SUI_LANG=zh` 适用于非交互式安装。

## [1.4.3] - 2026-05-15 - sing-box 运行时升级

本次发布将内嵌的 sing-box 运行时从 `v1.13.4` 升级到 `v1.13.11`，面板、
REST API、前端表单与数据库 schema 均保持不变。

### 运行时

- 升级 `github.com/sagernet/sing-box` 至 `v1.13.11`。
- 接受配套的上游依赖集合，包括 `sing v0.8.9`、`sing-tun v0.8.9`、
  `sing-quic v0.6.1`，以及 NaiveProxy 所需的 2026 年 4 月 `cronet-go`
  模块。
- 将 Linux release 工作流锁定至完整的 `cronet-go` commit
  `e4926ba205fae5351e3d3eeafff7e7029654424a`，避免 release 构建使用短
  commit 前缀来检出源码。

### 兼容性与安全性

- 不需要数据库迁移；存储中的 inbound/outbound/endpoint/service JSON
  与 `sing-box v1.13.11` 保持兼容。
- 没有新增 Web UI 字段，因为 `sing-box 1.13.5` 至 `1.13.11` 仅包含
  修复与运行时更新，包括 fake-ip DNS 修复、NaiveProxy 升级和 process
  searcher 回归修复。
- 生产环境升级应部署完整的 release 归档或重新构建的镜像，使更新后的
  `libcronet.so`/`libcronet.dll` 与新二进制保持一致。

### 验证

- `go mod verify`
- `go test ./...`
- `go test -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" ./...`

## [1.4.2-beta] — 2026-05-14 — 安全与可靠性加固

本次发布大幅重写了认证、事务与运行时控制流，将外部订阅 fetcher 加固
为可抵御 SSRF，并将 Go 模块路径重命名为
`github.com/deposist/s-ui-x`。

完整的后端测试套件 (`go test`、`go test -race`、
`go test -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale"`)
以及完整的前端流水线 (`npm ci`、`npm run build`、`npm run lint`、
`npm audit --audit-level=high`) 全部通过。

### 亮点

- 明文密码替换为 bcrypt；首次成功登录时已有账户会自动迁移。
- 首次安装时随机生成管理员密码，并在应用日志中只输出一次（不再使用
  `admin/admin`）。
- 登录限速器（每 15 分钟内 5 次失败 / 封锁 15 分钟），内存使用受界。
- 双语 (英 / 俄) `install.sh` 与 `s-ui` 管理菜单；首次运行时可选择，
  通过菜单项 **21. Language** 切换，保存在 `/etc/s-ui/lang`。默认语言
  为英文。
- 面板默认时区从 `Asia/Shanghai` 改为 `Europe/Moscow`。
- 默认前端 locale 从简体中文改为英文（已有安装会保留 `localStorage`
  中保存的 locale）。
- 外部订阅 URL fetcher 拒绝私有/loopback/link-local 目标，并在 dial
  阶段重新校验解析得到的 IP，阻止 DNS-rebinding 攻击。
- 配置保存不再因 commit/start 失败而让面板与 sing-box 状态不一致。
- core 生命周期、在线统计、last-update 记账以及 v2 token 存储完全
  race-free。
- 前端恢复 code splitting；剩余位置已移除 `v-html`；`AbortController`
  替换被废弃的 `axios.CancelToken`。

### 破坏性 / 行为变化

- **模块路径**：`github.com/alireza0/s-ui` → `github.com/deposist/s-ui-x`。
  源码使用方需更新 import；预编译二进制不受影响。
- **默认管理员密码**：在全新数据库上会生成 24 个字符的随机密码。请在
  应用日志中查找
  `created initial admin user. username=admin password=...` 一行。
  **已有数据库会保留其原有管理员**，不会被重置。
- **`X-Forwarded-For`**：除非 `SUI_TRUSTED_PROXIES` 列出了直接客户端，
  否则该 header 会被忽略。设置后，链路从 **右向左** 遍历，第一个
  非可信 hop 胜出。此前会返回最左侧（容易被伪造的）值。
- **登录封锁**：同一 IP 在 15 分钟内 5 次失败会被封锁 15 分钟。
- **订阅 fetcher TLS**：移除了 `InsecureSkipVerify`。自签名源现在必须
  使用系统 store 信任的证书。
- **订阅 fetcher 私有目标**：默认阻止。设置
  `SUI_ALLOW_PRIVATE_SUB_URLS=true` 可重新启用（例如同主机的
  `127.0.0.1` 源）。
- **订阅 fetcher 大小上限**：响应大于 4 MiB 会被拒绝。
- **Cookie store**：cookie 现在为 `HttpOnly`、`SameSite=Lax`，并在请求
  通过 HTTPS（直接或经由发送 `X-Forwarded-Proto: https` 的可信代理）
  时设置 `Secure`。
- **前端 dedupe**：仅 `GET`/`HEAD`/`OPTIONS` 会被去重；并发的写操作
  互不取消。

### 安全

| 严重程度 | 变更 |
| --- | --- |
| 高 | 用 bcrypt hash 替换明文密码存储 (`util/common/password.go`)。已有条目通过 `bcrypt:` 前缀或 `$2[aby]$` 成本标识识别。 |
| 高 | 懒迁移：用未哈希密码成功登录时，DB 记录会更新为 bcrypt hash。 |
| 高 | 移除 `admin/admin` 默认值；首次运行的管理员密码由 `common.Random(24)` 随机生成并仅记录一次 (`database/db.go.initUser`)。 |
| 高 | 引入登录限速器 (`api/rateLimit.go`)，定期清理状态，最多跟踪 4096 个 key 以防止内存无界增长。 |
| 高 | 加固 session cookie：`HttpOnly` + `SameSite=Lax` + 在 HTTPS 上的 `Secure` (`api/session.go`)。 |
| 高 | 仅在设置 `SUI_TRUSTED_PROXIES` 时才使用 `X-Forwarded-For`；解析器从右向左遍历链路，返回第一个非可信 hop，而不再返回容易被伪造的最左值 (`api/utils.go`)。 |
| 高 | 在 `service/config.go.GetChanges` 与 `service/config.go.CheckChanges` 中将不安全的 SQL 字符串拼接替换为参数化查询。 |
| 高 | 在 `service/inbounds.go.fetchUsersByCondition` 的 inbound 用户查询 SQL 构造中加入静态标识符 allow-list，避免未来新的 inbound 类型成为 SQL 注入向量。 |
| 高 | 移除外部订阅获取的默认 TLS 校验绕过 (`util/subToJson.go`)。 |
| 高 | 外部订阅 URL 校验：仅 HTTP/HTTPS，默认阻止 `localhost`/private/link-local/multicast/unspecified；通过 `SUI_ALLOW_PRIVATE_SUB_URLS=true` opt-in；响应限制在 4 MiB。 |
| 高 | 抗 DNS rebinding 的 dialer：自定义 `http.Transport.DialContext` 会重新校验每个解析到的 IP，并直接连接已校验地址，阻止恶意 DNS 在校验和 dial 之间替换记录。 |
| 中 | 在 `WarpService.getWarpInfo`/`RegisterWarp`/`SetWarpLicense` 中将 `error` 吞没替换为显式的状态码与 JSON 解析检查；将手工 JSON 拼接替换为 `encoding/json`，避免转义问题。 |
| 中 | Domain validator middleware 现在不区分大小写，并正确处理裸 IPv6 host。 |

### 可靠性 / 数据完整性

- 备份导出现在包含 `services` 与 API `tokens` 表 (`database/backup.go`)。
- 备份导入（UI：**Backup → Restore**）也会自动运行 schema 迁移与
  post-migration adapter (`database.AdaptToCurrentVersion`)。旧备份
  (S-UI 1.0/1.1/1.2/1.3 布局、明文密码、缺失 `services`/`tokens` 表、
  缺失 `version` 行) 会即时升级到当前形态。如果迁移失败，之前的运行
  数据库会被恢复并向面板返回错误，磁盘上不会出现半迁移状态。
- Schema 迁移 (`cmd/migration`) 现在返回 error 而不是调用 `log.Fatal`，
  错误的导入不再会杀死面板进程；version 行采用 upsert 而非依赖已存在
  的行。
- 同样的 migration + adaptation 流水线也会在面板启动 (`app.Init`) 时
  运行，因此把新的二进制放到已有的 1.x 数据库上首次启动会自动升级。
- 新增 `database.AdaptToCurrentVersion`，幂等的 post-migration 步骤：
  - 用 bcrypt 重新哈希任何明文密码（本 fork 之前的旧备份是明文）；
  - 重新应用新的 `idx_stats_lookup`/`idx_changes_lookup`/`idx_clients_name`
    索引；
  - 将 `settings.version` 提升到构建版本，以便下次迁移 runner 直接
    短路。
- 数据库路径构造改用 `filepath.Join` 而不是字符串拼接。
- 数据库初始化为最热的查询创建 `idx_stats_lookup`、`idx_changes_lookup`
  与 `idx_clients_name` 索引 (`database/db.go.ensureIndexes`)。
- SQLite 连接池调优：`SetMaxOpenConns(8)`、`SetMaxIdleConns(4)`、
  `SetConnMaxLifetime(time.Hour)`，DSN 中已有 `_busy_timeout=10000` 与
  `_journal_mode=WAL`。这避免了写入统计时的 `SQLITE_BUSY` 风暴。
- 检查 `service.config.Save`、`service.stats.SaveStats` 与
  `service.client.DepleteClients` 中的事务提交；提交失败现在会逐级
  上报，而不再被静默吞掉。
- 配置保存只有在 DB 成功 commit 之后才会改变 sing-box 运行时状态。
  此前的行为可能导致 runtime 已变更但 DB 已回滚。
- 用户触发的 core 重启 (`RestartCore`) 绕过 cron 冷却，使 API 反映
  真实启动状态。cron `CheckCoreJob` 仍尊重冷却。
- Inbound 重启与 `GetSingboxInfo` 现在对并发的 core stop/start 是 nil-safe
  的（之前在 `corePtr.GetInstance().ConnTracker()` 上可能 panic
  `nil pointer dereference`）。
- Race-detector clean 的同步：
  - API token (`api/apiV2Handler.go`，现在是 `map[string]TokenInMemory`，
    O(1) 查找)。
  - 在线统计 (`service/stats.go.onlineResources`) — 读端在 `RWMutex`
    保护下获得 deep copy。
  - core 运行状态与实例指针 (`core/main.go.Core`)。
  - last-update 记账 (`service/config.go.LastUpdate`)。
- HTTP 服务器为面板与订阅服务器都设置了 `ReadHeaderTimeout`、
  `ReadTimeout`、`WriteTimeout`、`IdleTimeout` 与
  `tls.Config.MinVersion = tls.VersionTLS12`。

### 前端 / 工具链

- 通过同步 `package-lock.json` 修复 `npm ci`。
- 将 ESLint 迁移到 flat config (`frontend/eslint.config.mjs`)。
- Lint 脚本只报告不自动修复 (`"lint": "eslint ."`)。
- `npm audit --audit-level=high` 报告 0 漏洞。
- 将 axios 设置移至导出的 instance；用 `AbortController` 替换被废弃的
  `CancelToken`。Dedupe 仅限于幂等读。
- 从 `Logs.vue`、`RuleImport.vue`、`Main.vue` 中的 IP 列表以及 gauge
  tile (`components/tiles/Gauge.vue`) 中移除不安全的 `v-html`。
- 修复 `enableTraffic=false` 未传播到 store、`loadClients` 在结果为空
  时崩溃，以及 `Main.vue.reloadData` 中未使用的过滤状态请求列表。
- 重新启用 Vite code splitting；构建产物使用 `[hash].js`/`[hash].css`
  文件名。

### 本地化与默认值

- `install.sh` 与 `s-ui` 管理菜单现在为双语（英文 / 俄文）。首次运行
  时会询问语言；选择保存在 `/etc/s-ui/lang` 并在后续运行中复用。
  `SUI_LANG=en|ru` 可在交互或 CI 中覆盖。
- 添加菜单项 **21. Language**，无需编辑文件即可切换 UI 语言。
- 面板默认 `timeLocation` 从 `Asia/Shanghai` 改为 `Europe/Moscow`。
- 前端默认 locale（以及 Vuetify locale）从 `zhHans` (简体中文) 改为
  `en`。`localStorage` 中保存的用户选择仍被尊重，已有浏览器会保持其
  语言。

### 仓库 / 打包

- Go 模块重命名为 `github.com/deposist/s-ui-x`；所有内部 import
  已更新。
- `frontend/go.mod` 让根目录的 `go` 命令避开 `frontend/node_modules`。
- README、`install.sh`、`s-ui.sh`、`docker-compose.yml` 已更新指向
  `https://github.com/deposist/s-ui-x` 与
  `ghcr.io/deposist/s-ui-x`。

### 测试

新增回归测试：

- `util/common/password_test.go` — 哈希、明文检测、迁移标记。
- `util/subToJson_test.go` — URL 校验拒绝 `file://`、`localhost`、
  RFC1918、IPv6 loopback；opt-in 恢复私有目标。
- `util/subToJson_dial_test.go` — dialer hook 在校验后拒绝 loopback
  地址；opt-in 允许它们。
- `service/setting_test.go` — `subURI` 的默认端口省略。
- `database/backup_test.go` — 备份包含 `services` 与 `tokens`。
- `database/adapt_test.go` — 导入时旧的明文密码重新哈希正确、幂等并
  提升 `settings.version`。
- `api/rateLimit_test.go` — 达到最大失败数即封锁、重置可清空状态、
  并发访问。
- `api/utils_test.go` — XFF 解析矩阵 (不可信客户端、最右非可信 hop、
  全部可信回退、来自不可信客户端的伪造 XFF)。

### 验证

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `go test -count=1 ./...` | ✅ |
| `go test -count=1 -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale" ./...` | ✅ |
| `go test -race -count=1 ./...` | ✅ (需要 CGO 与 C 编译器，例如 `C:\msys64\ucrt64\bin\gcc.exe`) |
| `npm ci` | ✅ |
| `npm run build` | ✅ |
| `npm run lint` | ✅ |
| `npm audit --audit-level=high` | ✅ (0 漏洞) |

## 升级指南 (TL;DR)

可以直接原地升级，不会丢失数据，也无需重新配置服务器。每次面板启动时
DB schema 都会自动迁移 (`app.Init` → `cmd/migration` →
`database.AdaptToCurrentVersion`)，已有的 settings/inbounds/outbounds/
clients/tokens 保持不变，明文管理员密码会在下一次登录时自动迁移到
bcrypt。来自旧 S-UI 版本 (1.0/1.1/1.2/1.3) 的备份可以直接通过面板
恢复，并在同一流程中升级到当前 schema。

1. 以防万一先做备份：
   - 通过面板：**Backup → Backup**，保存生成的 `s-ui_*.db`；
   - 或者直接复制文件：`cp /usr/local/s-ui/db/s-ui.db /root/s-ui.db.bak`。
2. 停止服务：`systemctl stop s-ui`。
3. 用新构建替换二进制或 docker 镜像：
   - 手动：将新的 tarball 解压到 `/usr/local/s-ui/`；
   - docker：将镜像 tag 改为 `ghcr.io/deposist/s-ui-x` 并执行
     `docker compose pull && docker compose up -d`。
4. 启动服务：`systemctl start s-ui`。
5. 像往常一样登录。当前你的密码以明文存储；面板会在第一次成功登录时
   透明地完成哈希。

升级后建议确认：

- 如果面板位于 reverse proxy 后面，并且你依赖 `X-Forwarded-For` (例如
  IP 审计日志)，请将 `SUI_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16,…`
  设置为代理所在的 CIDR。如果不设置，XFF 会被忽略，审计日志显示的将
  是代理 IP 而不是真实客户端。
- 如果你从私有端点 (`http://127.0.0.1:…/sub` 等) 拉取外部订阅，请设置
  `SUI_ALLOW_PRIVATE_SUB_URLS=true`。
- 如果你之前使用旧的安装/更新脚本 (`deposist/s-ui`)，请一次性获取新版：
  `wget -O /usr/bin/s-ui https://raw.githubusercontent.com/deposist/s-ui-x/main/s-ui.sh && chmod +x /usr/bin/s-ui`。

## 回滚

如果出现问题，恢复备份就足够了：

1. `systemctl stop s-ui`。
2. `cp /root/s-ui.db.bak /usr/local/s-ui/db/s-ui.db`。
3. 恢复之前的二进制，或将 `docker compose` 切回之前的镜像 tag。
4. `systemctl start s-ui`。

`users.password` 列中的 bcrypt 前缀向前向后都与旧二进制兼容：旧二进制
只是无法匹配已哈希的密码，此时可用 `s-ui admin -reset` 恢复一个已知
凭据。数据是安全的；回滚时只需要在 CLI 重置一次管理员密码。
