# 开发环境

本机已验证组合：Windows 11 + Go 1.27.3 + Python 3.14.7 + Rust 1.99.1 (gnu) + MinGW-w64 GCC 16.2 + CMake 4.4.4。

## 安装

```
winget install --id Rustlang.Rustup -e
winget install --id BrechtSanders.WinLibs.POSIX.UCRT -e
winget install --id Kitware.CMake -e
rustup toolchain install stable-x86_64-pc-windows-gnu --profile minimal
```

国内下载 rustup 组件建议先设镜像：

```
$env:RUSTUP_DIST_SERVER='https://mirrors.tuna.tsinghua.edu.cn/rustup'
$env:RUSTUP_UPDATE_ROOT='https://mirrors.tuna.tsinghua.edu.cn/rustup/rustup'
```

MSVC 工具链需要 Visual Studio Build Tools；本机走 GNU 路线，省两个 G 的下载。

## 中文路径的坑

MinGW 的 `cc1plus.exe`、`ld.exe` 和 Rust 链接器都不认 UTF-8 路径，`E:\项目开发\...` 会被读成乱码，
报 `No such file or directory`。两个解法，任选：

1. 建 ASCII junction，从 junction 路径构建（本机采用）：
```
New-Item -ItemType Junction -Path C:\pulse-mesh-link -Target E:\项目开发\pulse-mesh
```
2. 把仓库放到纯 ASCII 路径，比如 `C:\work\pulse-mesh`。

从 junction 构建时，Rust 产物目录建议也指到 ASCII 路径：

```
$env:CARGO_TARGET_DIR='C:\pulse-mesh-target'
```

## 命令

```
cd cloud; go test ./...
cd ai; py -m unittest discover -s tests -v
cd edge; rustup override set stable-x86_64-pc-windows-gnu; cargo test
cd firmware; cmake -S . -B build -G "MinGW Makefiles"; cmake --build build
```

firmware 构建后产物为 `build/pulse_firmware.exe`，直接跑会输出 JSON 行，第 43 行是 critical 尖峰。
