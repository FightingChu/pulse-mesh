# 架构

节点(C++) 采样振动 RMS + 温度 + 电流，tick=42 模拟故障，输出 JSON 行。
网关(Rust) 聚合转发 Go 云端，云端挂了就写 cache.jsonl 离线缓存。
云端(Go) /api/v1/samples 收数，/health 探活，内存 Store 保留最近 10000 条。
AI(Python) 用历史基线做流式异常检测：z = (当前值 - 历史均值) / 历史标准差。
App(Flutter) 拉 /api/v1/samples 列表展示，离线显示 offline。

# 频谱分析

节点以 1 kHz 采样 1024 点，加 Hann 窗后做 radix-2 FFT，输出主频、峰值幅度和三个频带能量占比。
频带划分为 0-10 Hz、10-100 Hz、100-500 Hz，C++ 与 Python 实现同一套判据：

- high_band > 0.25 -> bearing_wear
- 主频 / 转频 在 1.8-2.2 -> misalignment
- 主频 / 转频 在 0.8-1.2 -> imbalance
- low_band > 0.5 -> looseness
- 其余 -> normal

两端使用对称 Hann 窗，窗系数之和为 `(N-1)/2`（`N>2`）。单边谱的峰值幅度按 `4/(N-1)` 乘以 FFT 幅值；奈奎斯特频点没有独立的负频率分量，只乘 `2/(N-1)`。Python 使用未舍入的主频和频带占比判定故障；仅在返回数据时将主频舍入到两位小数、峰值幅度和频带占比舍入到四位。因此显示值恰好等于阈值时，应以 `fault` 字段为准。

# 告警

云端收到 warning / critical 样本时生成告警，按 device_id + metric + level 在 5 分钟窗口内去重。
GET /api/v1/alerts?device=motor-01&include_acked=1 查询，POST /api/v1/alerts/{id}/ack 确认。

窗口去重在内存中使用独立的设备、指标、等级字段，不通过分隔符拼接；合法标识符中的 `|` 不会令不同告警互相抑制。现有 JSONL 格式保持不变，启动时从告警原始字段恢复窗口索引，兼容旧 `seen_key` 记录。

告警列表返回独立快照，包含确认时间 `acked_at` 的副本；调用方修改快照不会改变引擎内的确认记录或其他快照，内存模式和日志恢复后行为一致。

# 诊断链路

网关（Rust）先按 1 kHz 采 1024 点波形，发到云端 POST /api/v1/diagnose，云端转发给 AI 的 POST /spectrum，
拿回主频、频带能量和故障类型，落库后按故障等级自动生成告警：

网关设备、采样数、采样周期、采样率和转频分别由 PULSE_EDGE_DEVICE_ID、PULSE_SAMPLE_COUNT、
PULSE_SAMPLE_INTERVAL_MS、PULSE_SAMPLE_RATE_HZ、PULSE_ROTATION_HZ 配置。诊断采样率至少 240 Hz，
以捕获 120 Hz 轴承特征；AI 和云端 API 使用相同下限。采样点数 1-100，采样率需满足转频 Nyquist 条件。默认一轮 10 点、每点 200 ms。

Rust 波形合成先计算频率与采样率之比，再生成逐点相位，避免合法大频率配置在中间乘法中溢出成 `NaN`；普通配置仍保留基频、二次谐波和 120 Hz 轴承分量。

- bearing_wear / misalignment -> critical
- imbalance / looseness -> warning
- normal -> 不告警

云端故障告警的 metric 写成 fault:<type>，所以不同故障不会被同一条去重键吃掉。
GET /api/v1/diagnoses?device=motor-01 查历史诊断。AI 地址用 PULSE_AI_URL 覆盖，默认 http://127.0.0.1:8000。

# 可观测性与运行时

- 每个请求打一行访问日志：方法、路径、状态码、耗时。
- 访问日志对方法、解码后路径加引号并转义控制字符，防止编码换行伪造记录；panic 日志同样转义请求字段和错误文本，单独保留诊断堆栈。
- 恢复中间件保留 `http.ErrAbortHandler` 的主动中止语义，交由 HTTP 服务端中断响应，不追加 JSON 或误报 panic 堆栈；普通 panic 仍返回 JSON 500。
- 日志和请求计数保留实际写出的首个最终状态码，后续重复 `WriteHeader` 不覆盖；100/103 等临时响应不作为最终状态，101 协议切换除外。
- GET /metrics 输出 Prometheus 文本，计数器按方法、路径、状态码聚合，告警确认路径归一化为 /api/v1/alerts/{id}/ack。
- 所有响应带 CORS 头，OPTIONS 预检直接返回 204。
- OPTIONS 预检同样记录访问日志并按 204 计数，不进入业务处理；对 `/metrics` 的预检不算 scrape，正常 scrape 仍只计数一次。
- 服务设置 ReadHeader 5s、Read 15s、Write 15s、Idle 60s 超时，收到 SIGINT/SIGTERM 后 10s 内优雅退出。
- AI 服务用 ThreadingHTTPServer，支持并发请求，访问日志默认开启，可用 PULSE_ACCESS_LOG=0 关闭。
- Python AI 自定义访问日志复用标准 HTTP 处理器的控制字符转义表，避免请求行中的终端控制序列生效；保留时间戳、普通 Unicode 文本及日志关闭开关。
- Go 和 Python API 限制 JSON body 为 128 KiB；诊断固定 1024 点，拒绝 NaN/Infinity、无效采样率和多段 JSON。
- Prometheus 路由标签限制为已知路由，其他路径合并为 unmatched，避免路径注入造成高基数。
- Prometheus 方法标签只保留标准 HTTP 方法，其他方法统一计入 `OTHER`，包括 `/metrics` 自身的请求；避免自定义方法名撑大计数表或通过分隔符混淆 path/status 标签，不改变路由响应状态。
- Rust outbox 限制 64 MiB、单条 128 KiB；追加后 sync_data，重放保留原始路由，只收到 2xx 后才确认移除。
- 缓存读取、重放和删除前快照核对共用 64 MiB 文件上限：先检查文件大小，再限制实际读取量，避免文件异常增长导致无界加载；超限返回读取错误、停止重放且保留原文件，恰好达到上限仍可读取。
- 网关追加缓存前检查非空文件是否以换行结束；尾行未结束时返回 `InvalidData` 并保持文件字节不变，不把新记录拼进残缺数据，也不自动截断或删除旧记录。完整但缺换行的尾行仍可按原规则重放，确认清空缓存后恢复追加；残缺尾行需先恢复数据。
- 同进程的 outbox 读写共用互斥锁；重放期间不持有锁，收到整份快照的 2xx 确认后，只有文件内容仍等于快照才删除。若同时追加了新记录，则保留整份缓存并在下次幂等重放；多个网关进程不得共用同一缓存路径。
- Rust HTTP 客户端设置 5s 读写超时，只接受 2xx 作为成功；未知路由在连接前拒绝。
- Rust 网关读取响应头最多 8 KiB，完整头部必须在该范围内结束；上限包含末尾空行，不受 TCP 分包边界影响，超限响应不确认离线缓存，响应正文大小不计入头部限制。
- Rust 网关跳过 100/103 等临时响应，只有完整的最终 2xx 响应头才确认缓存；101 协议切换、缺失最终响应或最终错误均保留缓存。临时和最终响应头共用 8 KiB 读取预算。
- 网关响应状态行要求 HTTP 版本、三位状态码和原因短语之间使用 ASCII 空格，不将 Unicode 空白或制表符当作字段分隔符；畸形状态行不确认缓存，原因短语内容不参与成功判定。
- 网关事件带 event_id，样本、诊断和由其触发的告警以 device_id + event_id 幂等；同一事件即使超过告警时间去重窗口或服务重启，也不会重复生成告警。
- 永久告警事件索引以 device_id + event_id 的 SHA-256 定长摘要保存，旧 JSONL 明文键启动时兼容迁移到摘要。
- 持久化模式完整重放样本和诊断 event_id；运行时去重 map 和展示记录有界，重启后仍可识别 journal 中历史事件。
- 告警 event_id journal 也完整重放；内存永久去重索引使用固定 32 字节摘要，避免保留完整设备和事件字符串。
- 设置 PULSE_DATA_DIR 后，云端将样本、诊断、告警分别写入 samples.jsonl、diagnoses.jsonl、alerts.jsonl；启动时恢复记录、event_id 索引、告警 ack 与去重窗口；Docker Compose 和 Helm 默认挂载持久卷。
- JSONL 后端是单副本写入模式；Kubernetes 使用 Recreate 和一个 RWO 持久卷，不能直接扩为多副本。
- 设置 PULSE_DATA_DIR 时，云端获取 OS 独占锁；第二个进程不能同时打开同一数据目录，进程崩溃后锁由操作系统自动释放。
- 数据目录锁支持重复和并发 `Close`，只释放一次底层句柄；已关闭锁的再次清理不影响后续新持有者，nil 和零值锁关闭均为空操作。
- 启动时对缺换行的尾行先验证 JSON：完整记录补换行并同步落盘，即使它是文件唯一一条记录也保留；残缺记录截断后同步落盘。运行中写入或 fsync 失败会回滚该条记录，避免污染后续日志。
- 每个 JSONL 文件上限 256 MiB；达到上限时写 API 返回 507，不丢弃旧数据。
- Prometheus 暴露各 store 的记录数、日志字节数和持久化写失败计数，store 标签固定为 samples / diagnoses / alerts。
- 未设置 PULSE_DATA_DIR 时使用纯内存模式，适合本地快速开发。
- AI 连续 3 次发生网络、超时或 5xx 错误后熔断 15 秒，冷却后仅允许一个探测请求；AI 的 4xx 输入错误不计入熔断。
- AI 客户端自身的超时（等待响应头或读取正文）计入熔断；只有传入的调用方 context 已取消或到期且错误匹配时，才不计入 AI 故障。
- 熔断器按状态代次跟踪在途请求；开路后旧请求的迟到成功、取消或失败不改变新状态，也不会释放当前恢复探测的名额。
- Python AI HTTP 服务最多 8 个 worker，最多排队 16 个连接；饱和时返回 503/Retry-After: 1，socket 请求读取超时 5 秒，可用 PULSE_AI_MAX_WORKERS 设置 1-64 worker。
- AI 服务构造时要求处理线程和过载拒绝线程上限均为范围内整数，拒绝布尔、浮点及 NaN 等非法配置；校验发生在绑定端口前，防止非整数信号量失去并发上限。
- AI 请求超时必须为正有限数值，并满足当前平台的 socket 和线程等待限制；启动前校验，避免意外禁用超时、启用非阻塞模式或在接收连接时才报错。默认 5 秒和合法小数秒配置保持不变。
- 接收连接后若 socket 超时设置失败，AI 服务主动关闭该连接并重新抛出原始异常，不遗留连接或占用处理线程名额；后续连接仍正常接收。
- 过载拒绝线程构造或启动失败时，释放对应名额、移除线程登记并关闭连接，保留原始异常，不影响后续过载请求的处理。
- 云端同时最多运行 4 个频谱诊断请求，超额返回 429 和 Retry-After，避免波形计算与等待 AI 无界堆积。
- 网关将每 10 条遥测合并到 POST /api/v1/samples/batch，批量上限 100；服务先校验整批，再逐条幂等写入，响应分别报告 stored / duplicates / alerts。
- GET /api/v1/samples 与 GET /api/v1/diagnoses 支持 limit=1..100，默认 100，拒绝超界参数。
- 样本、告警和诊断列表先严格解析查询字符串；非法百分号编码或未转义分号返回 JSON 400，不再静默丢弃设备筛选或 limit。合法编码的 Unicode、加号和分号设备名继续正常筛选。
- 持久化批量先编码整批，再一次追加并 fsync，成功后才改内存状态；降低磁盘同步次数并避免半批写入。
- `/health/live` 只报告进程存活；`/health/ready` 检查样本、诊断、告警存储可用性。Kubernetes 分别用作 liveness 和 readiness probe。
- 样本、诊断、告警存储的 `Close` 可重复调用；关闭后所有写入入口（含空批次、重复事件和告警确认）返回 `journal.ErrClosed`，API 映射为 503，readiness 返回 `not_ready`。列表和计数保留已有内存快照；诊断 `Get` 关闭后不再命中，诊断请求在调用 AI 前检查存储可用性。
- `/metrics` 响应在快照前计入本次 scrape；外层请求日志不重复增加该计数。

# 判据

历史窗口不含当前值，至少 3 个点，标准差下限 1e-6，避免单点尖峰把阈值自己撑大。
|z| >= 6 critical，|z| >= 3 warning，其余 normal。
振动 RMS 只做展示指标，不参与分级。

# 线上契约

节点输出一行一个 JSON：
{"device_id":"motor-01","metric":"vibration_rms","value":4.80,"level":"critical","temp_c":57.0}

字段固定：device_id / metric / value 必填，level 取 normal|warning|critical，
temp_c 可选，ts 缺失时由云端补 UTC 时间。

单条和批量样本入口要求显式提供数值 `value`；缺失、`null`、布尔或字符串均返回 400，数值 `0` 合法。批量请求会在全部输入解码与校验通过后才写样本和告警，避免无效项被悄悄转换成零值。

样本 `ts` 在写入前还须能按 RFC 3339 重新编码；`+24:00` 等不可序列化时区返回 400，不写样本或告警，避免污染内存列表或触发持久化 500。合法时区偏移及纳秒精度保留，缺失时间仍自动补 UTC。

Rust 网关编码样本、批量样本和诊断请求时统一转义字符串中的引号、反斜杠和 JSON 控制字符，保留 Unicode 内容，避免标识符破坏 JSON 字段或离线记录边界；云端仍按原有规则校验字段合法性。

诊断入口的 `values` 数组必须由 1024 个有限数值组成；任意位置的 `null`、布尔或字符串都返回 400，不会调用 AI、写诊断或生成告警，已存在 event_id 的重试也必须通过这项校验。显式数值 `0` 仍是合法采样。

云端诊断入口保留显式 `rotation_hz: 0`，与网关及 Python AI 的零转频语义一致，不再替换为 25 Hz；省略或 `null` 继续使用原有 25 Hz 默认值，采样率默认和范围检查保持不变。

AI 响应必须显式提供数值 `dominant_hz`、`peak_amplitude`、`low_band`、`mid_band`、`high_band`；缺失、`null` 或非数值特征按上游故障返回 502 并计入熔断，不写诊断或生成告警。全零特征合法；存量诊断日志格式不变。

Python `/assess` 和 `/spectrum` 的 `values` 必须是数值数组，拒绝布尔、字符串、`null`、嵌套对象和无法表示为有限浮点数的超大整数，返回 JSON 400。频率参数同样只接受有限数值，不再把字符串或布尔值强制转成浮点数；省略时保留默认值，显式数值零仍按各字段的原有范围规则处理。

Python AI 使用标准 JSON 解码器处理首尾空白，仅接受空格、制表符、回车和换行；垂直制表符、不换行空格等非 JSON 空白返回 400，不进入分析。合法字符串内部的 Unicode 内容保持不变，多个 JSON 值仍被拒绝。

有限输入仍可能在均值、方差、RMS 或频谱能量计算中溢出。Python AI 在分级前拒绝无法得到有限结果的计算，返回含 `numeric range` 的 JSON 400，不把无效结果判为 `normal`；响应序列化禁止输出 `NaN` / `Infinity`。

Python AI 在解析 JSON 和执行分析前核对实际收到的请求体长度；连接提前结束、字节数不足 `Content-Length` 时返回 JSON 400 并关闭连接，即使已收到的部分恰好是完整 JSON，也不予处理。显式零长度请求保留原有行为，长度正确的请求仍支持 HTTP keep-alive。

Python AI 的 GET 路由拒绝带正文或有歧义的请求长度：非零或非法 `Content-Length`、重复长度头及 `Transfer-Encoding` 返回 JSON 400 并关闭连接，避免未消费的正文被当作下一条请求。无长度头或合法零长度 GET 仍可复用连接，健康检查与未知路由状态不变。

正常和过载处理共用 `Content-Length` 数值解析：仅接受 ASCII 十进制数字和两端的 HTTP 空格 / 制表符，允许前导零，拒绝正负号、下划线及其他空白。正常路径对非法格式返回 JSON 400；过载路径不再按非法长度继续读取正文，直接关闭连接。

过载路径在继续读取正文前检查全部请求头；重复 `Content-Length`（含相同值）或任何 `Transfer-Encoding` 都直接关闭连接并释放拒绝线程名额，不再依据首个长度头等待正文。无请求体的健康检查和合法零长度请求仍返回 503/Retry-After。

# 验证

Go：go test ./...（httpapi 4 个 + ingest 4 个）
Python：py -m unittest discover -s tests -v（10 个）
一键：pwsh tools/verify.ps1

# MVP 计划

1. 本机跑通 Go + Python，curl 验证收数和判分。
2. 装 Rust 后 cargo run 网关，断云端验证离线缓存。
3. 装 C++ 工具链后 cmake 构建固件模拟器。
4. 装 Flutter 后跑 App，看 motor-01 曲线和告警。
5. 上 K8s + Prometheus，再做振动频谱分类。
