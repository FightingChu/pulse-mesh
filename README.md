# PulseMesh - 工业巡检边缘 AI 平台

工业设备健康监测：振动 / 温度 / 电流 -> 边缘判断 -> 云端聚合 -> 手机告警。

架构：ESP32 节点(C++) -> Rust 网关 -> Go 云端(K8s) -> Python AI -> Flutter App

## 目录

- `firmware/` C++17，RMS 计算 + 三级判级，输出 JSON 行
- `edge/` Rust std-only，转发云端，断网落 `cache.jsonl`
- `cloud/` Go 1.27 stdlib-only，`/health`、`POST/GET /api/v1/samples`
- `ai/` Python 3.14 stdlib-only，历史基线 z-score 异常检测
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

## 验证状态

- 已跑通：Go 11 个测试、Python 10 个测试、Rust 4 个测试、C++ 固件构建与 critical 判级、云端+网关端到端与离线回放
- 未验证：Flutter（本机未装）、Docker / K8s 部署（本机无 Docker）

工具链安装见 `docs/dev-setup.md`。中文路径会让 MinGW / Rust 链接器读不到文件，用 ASCII junction 构建。

Flutter 补齐后：
```
cd mobile
flutter pub get
flutter run
```
