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

Hann 窗相干增益为 0.5，峰值幅度按 4/N 归一化补偿。

# 告警

云端收到 warning / critical 样本时生成告警，按 device_id + metric + level 在 5 分钟窗口内去重。
GET /api/v1/alerts?device=motor-01&include_acked=1 查询，POST /api/v1/alerts/{id}/ack 确认。

# 诊断链路

网关（Rust）先按 1 kHz 采 1024 点波形，发到云端 POST /api/v1/diagnose，云端转发给 AI 的 POST /spectrum，
拿回主频、频带能量和故障类型，落库后按故障等级自动生成告警：

- bearing_wear / misalignment -> critical
- imbalance / looseness -> warning
- normal -> 不告警

云端故障告警的 metric 写成 fault:<type>，所以不同故障不会被同一条去重键吃掉。
GET /api/v1/diagnoses?device=motor-01 查历史诊断。AI 地址用 PULSE_AI_URL 覆盖，默认 http://127.0.0.1:8000。

# 可观测性与运行时

- 每个请求打一行访问日志：方法、路径、状态码、耗时。
- GET /metrics 输出 Prometheus 文本，计数器按方法、路径、状态码聚合，告警确认路径归一化为 /api/v1/alerts/{id}/ack。
- 所有响应带 CORS 头，OPTIONS 预检直接返回 204。
- 服务设置 ReadHeader 5s、Read 15s、Write 15s、Idle 60s 超时，收到 SIGINT/SIGTERM 后 10s 内优雅退出。
- AI 服务用 ThreadingHTTPServer，支持并发请求，访问日志默认开启，可用 PULSE_ACCESS_LOG=0 关闭。
- Go 和 Python API 限制 JSON body 为 128 KiB；诊断固定 1024 点，拒绝 NaN/Infinity、无效采样率和多段 JSON。
- Prometheus 路由标签限制为已知路由，其他路径合并为 unmatched，避免路径注入造成高基数。
- Rust outbox 限制 64 MiB、单条 128 KiB；追加后 sync_data，重放保留原始路由，只收到 2xx 后才确认移除。
- Rust HTTP 客户端设置 5s 读写超时，只接受 2xx 作为成功；未知路由在连接前拒绝。
- 网关事件带 event_id，样本和诊断以 device_id + event_id 幂等；诊断重试返回已有结果，不重复调用 AI 或生成告警。
- 幂等记录跟随各自内存保留窗口，云端重启后不持久；需要跨重启保证时应替换为持久化存储。
- 设置 PULSE_DATA_DIR 后，云端将样本写入 samples.jsonl、诊断写入 diagnoses.jsonl，启动时恢复记录和 event_id 索引；Docker Compose 和 Helm 默认挂载持久卷。
- 未设置 PULSE_DATA_DIR 时使用纯内存模式，适合本地快速开发。

# 判据

历史窗口不含当前值，至少 3 个点，标准差下限 1e-6，避免单点尖峰把阈值自己撑大。
|z| >= 6 critical，|z| >= 3 warning，其余 normal。
振动 RMS 只做展示指标，不参与分级。

# 线上契约

节点输出一行一个 JSON：
{"device_id":"motor-01","metric":"vibration_rms","value":4.80,"level":"critical","temp_c":57.0}

字段固定：device_id / metric / value 必填，level 取 normal|warning|critical，
temp_c 可选，ts 缺失时由云端补 UTC 时间。

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
