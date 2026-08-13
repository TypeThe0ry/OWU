$ErrorActionPreference = 'Stop'

$macosRoot = Split-Path -Parent $PSScriptRoot
Push-Location $macosRoot
try {
    if (-not (Get-Command swift -ErrorAction SilentlyContinue)) {
        throw 'Swift is not installed or is not available on PATH. Run this check on macOS with Xcode 15.3+ or Swift 5.10+.'
    }

    swift package describe
    if ($LASTEXITCODE -ne 0) { throw 'swift package describe failed.' }

    swift build
    if ($LASTEXITCODE -ne 0) { throw 'swift build failed.' }

    swift test --parallel
    if ($LASTEXITCODE -ne 0) { throw 'swift test failed.' }
}
finally {
    Pop-Location
}
