# PulseMesh - 工业巡检边缘 AI 平台

工业设备健康监测：振动 / 温度 / 电流 -> 边缘判断 -> 云端聚合 -> 手机告警。

架构：ESP32 节点(C++) -> Rust 网关 -> Go 云端(K8s) -> Python AI -> Flutter App

仓库：[GitHub](https://github.com/FightingChu/pulse-mesh) | [Gitee](https://gitee.com/cwyay/pulse-mesh)

## 目录

- `firmware/` C++17，RMS 计算 + 三级判级，输出 JSON 行
- `edge/` Rust std-only，转发云端，断网落 `cache.jsonl`
- `cloud/` Go 1.27 stdlib-only，`/health`、`POST/GET /api/v1/samples`
- `cloud/internal/alerts` 告警引擎，5 分钟去重 + 确认接口
- `GET /metrics` Prometheus 计数器，请求日志 + CORS + 优雅关闭
- API 限制 128 KiB 请求体；网关 outbox 有容量上限、同步写盘和 2xx 确认重放
- 网关事件带 `event_id`，云端对样本和诊断请求幂等去重
- 设置 `PULSE_DATA_DIR` 开启 JSONL 持久化；Compose/Helm 已挂载 `pulse-data` 卷
- AI 熔断冷却保护；云端最多并发处理 4 个诊断请求
- JSONL 后端自动修复断电尾行；Kubernetes 持久化配置按单副本部署
- `/health/live` 和 `/health/ready` 分离进程存活与存储可用状态
- `ai/` Python 3.14 stdlib-only，z-score 异常检测 + FFT 频谱故障分类
- `mobile/` Flutter，样本列表 + 离线状态
- `infra/` docker-compose + K8s Deployment
- `tools/verify.ps1` 一键跑全部检查
- `docs/dev-setup.md` 工具链安装 + 中文路径坑

## 跑起来

云端：
```
cd cloud
go run ./cmd/api
curl http://127.0.0.1:8080/health
curl -X POST http://127.0.0.1:8080/api/v1/samples -H "Content-Type: application/json" -d "{\"device_id\":\"motor-01\",\"metric\":\"vibration_rms\",\"value\":4.8,\"level\":\"critical\"}"
```

AI：
```
cd ai
py app/main.py
curl http://127.0.0.1:8000/health
```

频谱诊断：
```
curl -X POST http://127.0.0.1:8000/spectrum -H "Content-Type: application/json" \
  -d '{"device_id":"motor-01","values":[1024 个采样点],"sample_rate_hz":1000,"rotation_hz":25}'
```

告警：
```
curl "http://127.0.0.1:8080/api/v1/alerts?device=motor-01"
curl -X POST http://127.0.0.1:8080/api/v1/alerts/alert-1/ack
```

云端诊断（转发 AI 并自动告警）：
```
curl -X POST http://127.0.0.1:8080/api/v1/diagnose -H "Content-Type: application/json" \
  -d '{"device_id":"motor-01","values":[1024 个采样点],"sample_rate_hz":1000,"rotation_hz":25}'
curl "http://127.0.0.1:8080/api/v1/diagnoses?device=motor-01"
```

验证：
```
pwsh tools/verify.ps1
```

没有硬件时造数据：
```
cd cloud
go run ./cmd/simulator -ticks 30 -interval 500ms
```

端到端：
```
cd cloud; go run ./cmd/api            # 终端 A
cd edge;  cargo run                   # 终端 B，输出 forwarded tick=...
curl "http://127.0.0.1:8080/api/v1/samples?device=motor-01"
```
停掉云端再 `cargo run`，数据落 `cache.jsonl`；云端恢复后下次启动先回放再发新数据。

## 判据

历史窗口不含当前值，至少 3 点，标准差下限 `1e-6`。
`|z| >= 6` critical，`|z| >= 3` warning，其余 normal。

频谱按主频与转频比值和频带能量分类；Hann 窗峰值按 `4/N` 归一化。

## 验证状态

- 已跑通：Go 41 个测试、Python 22 个测试、Rust 13 个测试、C++ 固件 CTest、云端+网关端到端、离线回放、网关波形诊断转 AI 并自动告警
- 未验证：Flutter（本机未装）、Docker / K8s 部署（本机无 Docker）

工具链安装见 `docs/dev-setup.md`。中文路径会让 MinGW / Rust 链接器读不到文件，用 ASCII junction 构建。

Flutter 补齐后：
```
cd mobile
flutter pub get
flutter run
```
