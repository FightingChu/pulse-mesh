$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$failed = @()

Push-Location (Join-Path $root "cloud")
try {
    go vet ./...
    if ($LASTEXITCODE -ne 0) { $failed += "go vet" }
    go test ./...
    if ($LASTEXITCODE -ne 0) { $failed += "go test" }
} finally {
    Pop-Location
}

Push-Location (Join-Path $root "ai")
try {
    py -m unittest discover -s tests -v
    if ($LASTEXITCODE -ne 0) { $failed += "python unittest" }
} finally {
    Pop-Location
}

$edge = Join-Path $root "edge"
$buildRoot = "C:\pulse-mesh-link"
$edgeBuild = $edge
if ((Test-Path (Join-Path $buildRoot "edge")) -and ($root -match '[^\x00-\x7F]')) {
    $edgeBuild = Join-Path $buildRoot "edge"
}
if ((Get-Command cargo -ErrorAction SilentlyContinue) -and (Test-Path $edge)) {
    Push-Location $edgeBuild
    try {
        if (-not $env:CARGO_TARGET_DIR) { $env:CARGO_TARGET_DIR = "C:\pulse-mesh-target" }
        cargo test
        if ($LASTEXITCODE -ne 0) { $failed += "cargo test" }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "skip cargo test: cargo not installed"
}

$firmware = Join-Path $root "firmware"
if ((Get-Command cmake -ErrorAction SilentlyContinue) -and (Get-Command g++ -ErrorAction SilentlyContinue) -and (Test-Path $firmware)) {
    $fwBuild = $firmware
    if ((Test-Path (Join-Path $buildRoot "firmware")) -and ($root -match '[^\x00-\x7F]')) {
        $fwBuild = Join-Path $buildRoot "firmware"
    }
    Push-Location $fwBuild
    try {
        cmake -S . -B build -G "MinGW Makefiles" | Out-Null
        cmake --build build | Out-Null
        if ($LASTEXITCODE -ne 0) {
            $failed += "firmware build"
        } else {
            $out = & (Join-Path $fwBuild "build\pulse_firmware.exe")
            if (-not ($out -match '"level":"critical"')) { $failed += "firmware critical sample" }
        }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "skip firmware build: cmake or g++ not installed"
}

if ($failed.Count -gt 0) {
    Write-Host "FAILED: $($failed -join ', ')"
    exit 1
}
Write-Host "ALL CHECKS PASSED"
