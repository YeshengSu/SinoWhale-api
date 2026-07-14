<#
.SYNOPSIS
  构建 SWAPI 镜像并打包 deploy-bundle
.DESCRIPTION
  1. docker build 构建 sinowhalex/swapi:<version>
  2. docker save 导出 tar
  3. 打包 deploy-bundle-<version>.tar.gz
.PARAMETER Version
  版本号，如 v0.1.0。留空则从 git tag 自动检测。
.EXAMPLE
  .\scripts\build-swapi-images.ps1 -Version v0.1.0
  .\scripts\build-swapi-images.ps1
#>
param(
  [string]$Version
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

# ========== 1. version ==========
# 标签识别策略：
#   1) 若显式传入 -Version，使用该值（最高优先级）
#   2) 否则用 `git tag --sort=-v:refname` 取仓库语义化最新标签（不受 HEAD 位置影响）
#   3) 自动识别成功后必须交互确认，避免示例值误用
#   4) Git 无标签时强制人工输入
function Read-VersionInput {
  param([string]$Default)
  if ($Default) {
    $prompt = "[INPUT] Confirm version (Enter = $Default): "
  } else {
    $prompt = "[INPUT] Enter version tag (e.g. v0.1.0): "
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
      Write-Host "[WARN] Current HEAD is NOT on tag $latestTag (uncommitted/ahead). Please confirm." -ForegroundColor Yellow
    }
    $Version = Read-VersionInput -Default $latestTag
  } else {
    Write-Host "[WARN] No git tag found in repository." -ForegroundColor Yellow
    $Version = Read-VersionInput
  }
}

if ([string]::IsNullOrWhiteSpace($Version)) {
  Write-Host "[ERROR] Version is empty. Aborting." -ForegroundColor Red
  exit 1
}
if ($Version -notmatch '^v\d+\.\d+\.\d+([.-].+)?$') {
  Write-Host "[ERROR] Invalid version format: '$Version'. Expected semver like v0.1.0." -ForegroundColor Red
  exit 1
}
Write-Host "[OK] Using version: $Version" -ForegroundColor Green

$DistDir = "$ProjectRoot\dist"
$TarFile = $DistDir + "\swapi-" + $Version + ".tar"

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  SWAPI Image Build" -ForegroundColor Cyan
Write-Host "  Version: " -NoNewline
Write-Host $Version -ForegroundColor White
Write-Host "  Image: sinowhalex/swapi" -ForegroundColor DarkGray
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# ========== 2. create dist ==========
if (-not (Test-Path $DistDir)) {
  New-Item -ItemType Directory -Path $DistDir -Force | Out-Null
}

# ========== 3. clean old tags ==========
Write-Host "[0/3] Cleaning previous build artifacts..." -ForegroundColor DarkGray

try { docker rmi "sinowhalex/swapi:$Version" 2>$null } catch {}
try { docker rmi "sinowhalex/swapi:latest" 2>$null } catch {}

# ========== 4. build image ==========
Write-Host ""
Write-Host "[1/3] Building sinowhalex/swapi..." -ForegroundColor Yellow

docker build -t "sinowhalex/swapi:$Version" -t "sinowhalex/swapi:latest" $ProjectRoot
if ($LASTEXITCODE -ne 0) {
  Write-Host "[ERROR] Image build failed" -ForegroundColor Red
  exit 1
}
Write-Host "  ok  sinowhalex/swapi:$Version" -ForegroundColor Green

# ========== 5. export tar ==========
Write-Host ""
Write-Host "[2/3] Exporting image tar..." -ForegroundColor Yellow

docker save "sinowhalex/swapi:$Version" -o $TarFile
if ($LASTEXITCODE -ne 0) {
  Write-Host "[ERROR] Tar export failed" -ForegroundColor Red
  exit 1
}
Write-Host "  ok  $TarFile" -ForegroundColor Green

# ========== 6. create deploy bundle ==========
Write-Host ""
Write-Host "[3/3] Creating deploy bundle..." -ForegroundColor Yellow

$BundleDir = $DistDir + "\deploy-bundle-" + $Version
$BundlePkg = $DistDir + "\deploy-bundle-" + $Version + ".tar.gz"

# clean old bundle
if (Test-Path $BundleDir) { Remove-Item -Recurse -Force $BundleDir }
if (Test-Path $BundlePkg) { Remove-Item -Force $BundlePkg }

New-Item -ItemType Directory -Path $BundleDir -Force | Out-Null
New-Item -ItemType Directory -Path ($BundleDir + "\scripts") -Force | Out-Null

# copy config files
Copy-Item "$ProjectRoot\docker-compose.deploy.yml" $BundleDir
Copy-Item "$ProjectRoot\.env.production.example" $BundleDir
Copy-Item "$ProjectRoot\scripts\verify-services.sh" ($BundleDir + "\scripts\")

# move image tar into bundle
Move-Item $TarFile $BundleDir

# ========== 7. create install.sh ==========
$installScript = @'
#!/usr/bin/env bash
# SWAPI one-click install script
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION="__VERSION__"
echo "=== SWAPI Deploy Bundle v${VERSION} ==="
echo ""
echo "[1/4] Loading Docker image..."
docker load -i "$DIR/swapi-${VERSION}.tar"
echo "[2/4] Configuring env..."
if [ ! -f "$DIR/.env.production" ]; then
  cp "$DIR/.env.production.example" "$DIR/.env.production"
  echo "  Created .env.production from template."
  echo "  >> Please edit .env.production to set passwords:"
  echo "     nano $DIR/.env.production"
  echo "  >> Then re-run: bash $DIR/install.sh"
  exit 0
fi
echo "[3/4] Deploying..."
DEPLOY_VERSION="${VERSION}" docker compose -p swapi --env-file "$DIR/.env.production" -f "$DIR/docker-compose.deploy.yml" up -d
echo "[4/4] Verifying..."
sleep 15
bash "$DIR/scripts/verify-services.sh"
echo ""
echo "=== SWAPI Deploy Complete! ==="
echo "  API:   https://api.sinxwhalex.com/api/status"
echo "  Admin: https://api.sinxwhalex.com"
'@

$installScript = $installScript -replace '__VERSION__', $Version
Set-Content -Path ($BundleDir + "\install.sh") -Value $installScript -Encoding ASCII

# ========== 8. package ==========
Write-Host "  Packaging bundle..." -ForegroundColor Yellow

Push-Location $DistDir
try {
  tar -czf $BundlePkg ("deploy-bundle-" + $Version)
  Write-Host "  Bundle created: $BundlePkg" -ForegroundColor Green
} catch {
  Write-Host "  WARN: tar -czf failed, creating zip instead" -ForegroundColor Yellow
  Compress-Archive -Path $BundleDir -DestinationPath ($BundlePkg -replace '\.tar\.gz$', '.zip')
  $BundlePkg = $BundlePkg -replace '\.tar\.gz$', '.zip'
}
Pop-Location

$bundleSize = [math]::Round((Get-Item $BundlePkg).Length / 1MB, 1)
Write-Host ""
Write-Host "============================================" -ForegroundColor Green
Write-Host "  Build Complete!" -ForegroundColor Green
Write-Host "  Version: $Version" -ForegroundColor Green
Write-Host "  Image: sinowhalex/swapi:$Version" -ForegroundColor DarkGray
Write-Host "  Deploy bundle: " -NoNewline
$bs = $BundlePkg + "  (" + $bundleSize + "MB)"
Write-Host $bs -ForegroundColor White
Write-Host "============================================" -ForegroundColor Green
Write-Host ""
Write-Host "Next step: upload to server" -ForegroundColor Cyan
Write-Host "  scp $BundlePkg root@14.103.22.215:/opt/" -ForegroundColor White
Write-Host ""
Write-Host "Then on server:" -ForegroundColor Cyan
Write-Host "  ssh root@14.103.22.215" -ForegroundColor White
Write-Host "  cd /opt && tar -xzf $(Split-Path $BundlePkg -Leaf)" -ForegroundColor White
Write-Host "  cd deploy-bundle-$Version && bash install.sh" -ForegroundColor White
