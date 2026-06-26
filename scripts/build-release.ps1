Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$releaseDir = Join-Path $repoRoot "bin\release"

$originalLocation = (Get-Location).Path
$oldCGOEnabled = $env:CGO_ENABLED
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH

$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Extension = ".exe" },
    @{ GOOS = "windows"; GOARCH = "arm64"; Extension = ".exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; Extension = "" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Extension = "" }
)

try {
    Set-Location $repoRoot

    if (Test-Path $releaseDir) {
        Remove-Item -Recurse -Force $releaseDir
    }
    New-Item -ItemType Directory -Force $releaseDir | Out-Null

    foreach ($target in $targets) {
        $targetName = "$($target.GOOS)-$($target.GOARCH)"
        $outDir = Join-Path $releaseDir $targetName
        $outFile = Join-Path $outDir "manila-server$($target.Extension)"

        New-Item -ItemType Directory -Force $outDir | Out-Null

        $env:CGO_ENABLED = "0"
        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH

        Write-Host "Building $targetName -> $outFile"
        & go build -trimpath -ldflags "-s -w" -o $outFile ./cmd/server
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $targetName"
        }
    }

    Write-Host "Release binaries written to $releaseDir"
}
finally {
    Set-Location $originalLocation
    $env:CGO_ENABLED = $oldCGOEnabled
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
}
