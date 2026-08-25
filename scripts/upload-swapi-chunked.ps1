<#
.SYNOPSIS
  Chunked upload of SWAPI deploy-bundle to server (避免 SCP 大文件 Broken pipe)
.PARAMETER Version
  版本号 / 标签。
    - 显式传入（如 v0.1.0 / test）优先级最高
    - 不传时自动从 `git tag --sort=-v:refname` 取仓库最新标签并交互确认
    - 仓库无标签时强制要求人工输入
.EXAMPLE
  .\scripts\upload-swapi-chunked.ps1
  .\scripts\upload-swapi-chunked.ps1 -Version v0.1.0
  .\scripts\upload-swapi-chunked.ps1 -Version test
#>
param(
    [string]$Version,
    [string]$RemoteHost = "root@14.103.22.215"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

# ========== 版本识别 ==========
function Read-VersionInput {
    param([string]$Default)
    if ($Default) {
        $prompt = "[INPUT] Confirm version (Enter = $Default): "
    } else {
        $prompt = "[INPUT] Enter version tag (e.g. v0.1.0 or test): "
    }
    $entered = Read-Host -Prompt $prompt
    if ([string]::IsNullOrWhiteSpace($entered)) { return $Default }
    return $entered.Trim()
}

if (-not $Version) {
    $latestTag = (git -C $ProjectRoot tag --sort=-v:refname 2>$null | Select-Object -First 1)
    if ($latestTag) {
        $latestTag = $latestTag.Trim()
        Write-Host "[INFO] Latest git tag detected: $latestTag" -ForegroundColor Cyan
        $headTag = (git -C $ProjectRoot describe --tags --exact-match HEAD 2>$null)
        if (-not $headTag) {
            Write-Host "[WARN] Current HEAD is NOT on tag $latestTag. Confirm before upload." -ForegroundColor Yellow
        }
        $Version = Read-VersionInput -Default $latestTag
    } else {
        Write-Host "[WARN] No git tag found. Manual input required." -ForegroundColor Yellow
        $Version = Read-VersionInput -Default $null
    }
}

if ([string]::IsNullOrWhiteSpace($Version)) {
    Write-Host "[ERROR] Version is empty. Aborting." -ForegroundColor Red
    exit 1
}
# 允许 semver (vX.Y.Z) 或 "test" 临时标签
if ($Version -notmatch '^(v\d+\.\d+\.\d+([.-].+)?|test)$') {
    Write-Host "[ERROR] Invalid version '$Version'. Expected semver like v0.1.0 or 'test'." -ForegroundColor Red
    exit 1
}
Write-Host "[OK] Using version: $Version" -ForegroundColor Green

# ========== 路径计算 ==========
$SourceFile = Join-Path $ProjectRoot "dist\deploy-bundle-$Version.tar.gz"
$VersionSlug = ($Version -replace '[^A-Za-z0-9]', '')      # remote 目录名只保留字母数字
$RemoteDir = "/tmp/swapi-chunks-$VersionSlug"
$ChunkSize = 50MB
$ChunkDir = Join-Path $env:TEMP "swapi-chunks-$VersionSlug"
$RemoteTarget = "/opt/deploy-bundle-$Version.tar.gz"

if (-not (Test-Path $SourceFile)) {
    Write-Host "[ERROR] Source deploy-bundle not found: $SourceFile" -ForegroundColor Red
    Write-Host "  Please build the deploy bundle first." -ForegroundColor DarkGray
    Write-Host "  Expected at: $SourceFile" -ForegroundColor DarkGray
    exit 1
}

# ========== 分块 ==========
if (-not (Test-Path $ChunkDir)) {
    New-Item -ItemType Directory -Path $ChunkDir | Out-Null
    Write-Host "Splitting $SourceFile ..."
    $fs = [System.IO.File]::OpenRead($SourceFile)
    $buffer = New-Object byte[] $ChunkSize
    $i = 0
    while (($bytesRead = $fs.Read($buffer, 0, $ChunkSize)) -gt 0) {
        $chunkPath = "$ChunkDir\chunk_{0:D3}" -f $i
        $out = [System.IO.File]::OpenWrite($chunkPath)
        $out.Write($buffer, 0, $bytesRead)
        $out.Close()
        $i++
    }
    $fs.Close()
    Write-Host "Split into $i chunks"
}

ssh $RemoteHost "mkdir -p $RemoteDir"

$chunks = Get-ChildItem $ChunkDir | Sort-Object Name
foreach ($chunk in $chunks) {
    $localSize = $chunk.Length
    $checkCmd = "stat -c %s $RemoteDir/$($chunk.Name) 2>/dev/null; true"
    $remoteSize = (ssh $RemoteHost $checkCmd) -as [long]
    if ($null -eq $remoteSize) { $remoteSize = 0 }
    if ($remoteSize -eq $localSize) {
        Write-Host "[SKIP] $($chunk.Name)"
        continue
    }
    $sizeMB = [math]::Round($localSize / 1MB, 1)
    Write-Host "[UP]   $($chunk.Name) ${sizeMB}MB"
    $retry = 0
    $success = $false
    while ($retry -lt 5) {
        scp $chunk.FullName "${RemoteHost}:${RemoteDir}/$($chunk.Name)"
        if ($LASTEXITCODE -eq 0) { $success = $true; break }
        $retry++
        Write-Host "  Retry $retry/5"
        Start-Sleep -Seconds 5
    }
    if (-not $success) { throw "Upload $($chunk.Name) failed" }
}

Write-Host "Merging into $RemoteTarget ..."
ssh $RemoteHost "cd $RemoteDir; cat chunk_* > $RemoteTarget; ls -lh $RemoteTarget; rm -rf $RemoteDir"
Write-Host "Done"
