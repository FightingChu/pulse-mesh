# 架构

节点(C++) 采样振动 RMS + 温度 + 电流，tick=42 模拟故障，输出 JSON 行。
网关(Rust) 聚合转发 Go 云端，云端挂了就写 cache.jsonl 离线缓存。
云端(Go) /api/v1/samples 收数，/health 探活，内存 Store 保留最近 10000 条。
AI(Python) 用历史基线做流式异常检测：z = (当前值 - 历史均值) / 历史标准差。
App(Flutter) 拉 /api/v1/samples 列表展示，离线显示 offline。

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
