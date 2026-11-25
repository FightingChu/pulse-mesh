# 固件算法与主机模拟器

`pulse_firmware` 是主机端模拟程序，输出遥测 JSON 行并调用频谱算法；尚未接入真实 ESP32 采样驱动。

每次运行通过标准库 `std::random_device` 生成批次前缀，遥测 `event_id` 再附加采样序号，频谱记录使用独立后缀。同一条输出重传应保留原始 ID；重新采样生成新 ID。板端移植时需要用硬件提供的启动标识/随机源替换主机模拟器逻辑。

RMS 先按最大绝对值缩放再累加平方，避免有限大数的平方溢出和微小信号的平方下溢；空输入和全零输入仍返回零，非有限输入返回非有限结果，由 JSON 序列化拒绝输出。

频谱分析在去除直流分量后按最大绝对值归一化，再进入 `float` FFT 工作区，最后还原峰值幅度。该处理避免变换中间溢出或微小信号转换为零，频率、频带能量比例和故障判据不变；原有输入范围检查和工作区大小保持不变。

## 验证

构建和算法测试沿用 CMake/CTest：

```powershell
cmake -S . -B build -G "MinGW Makefiles"
cmake --build build --parallel
ctest --test-dir build --output-on-failure
```

在装有 Python 的环境中检查实际输出（从 `firmware` 目录运行，`python` 替换为已安装解释器路径）：

```powershell
$env:PULSE_FIRMWARE_EXE = (Resolve-Path build/pulse_firmware.exe).Path
python -m unittest discover -s test -p test_output.py -v
```

输出回归测试启动模拟器两次，验证 JSON 可解析、每条事件 ID 唯一、不同运行不复用 ID，并检查第 43 条采样仍报告 `critical`。MinGW 中文路径问题的处理见 `../docs/dev-setup.md`。
