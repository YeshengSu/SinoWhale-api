#Requires -Version 5.1
<#
.SYNOPSIS
  Check and update the upstream sync baseline recorded in upstream.lock.json.

.DESCRIPTION
  This repository is a private fork of an upstream project (see upstream.repo
  in upstream.lock.json). Upstream tags must never enter the local tag
  namespace, so the only fetch uses --no-tags and upstream versions are
  inspected exclusively through read-only ls-remote queries.

  Modes:
    (no arguments)  same as -Check
    -Check          read-only report: current baseline, upstream latest
                    release, how many releases the baseline is behind, and a
                    tag-to-commit pairing validation of the baseline record
                    (lock.commit must match the tag's real upstream target).
                    Never writes the lock file.
    -To <tag>       move the baseline to the given upstream tag after an
                    interactive confirmation, then print the manual merge
                    guide. Never merges automatically.

.NOTES
  Merging upstream changes and resolving conflicts stays a manual step;
  this script only automates "inspect, compare, record".
#>

param(
    [string]$To = "",
    [switch]$Check
)

$ErrorActionPreference = "Stop"

# Run from any working directory: everything below targets the repo root.
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$LockPath = Join-Path $RepoRoot "upstream.lock.json"

function Write-Info([string]$Message) { Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Write-Warn([string]$Message) { Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Write-Err([string]$Message)  { Write-Host "[ERROR] $Message" -ForegroundColor Red }

# PS 5.1 trap: when a native command writes to stderr while stderr is
# redirected, the lines are wrapped into ErrorRecords and become terminating
# NativeCommandErrors under ErrorActionPreference=Stop. Downgrade EAP around
# each git call and judge the result by $LASTEXITCODE instead.
function Invoke-Git([string[]]$GitArgs) {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $output = & git @GitArgs 2>$null
        return [pscustomobject]@{ ExitCode = $LASTEXITCODE; Output = @($output) }
    } finally {
        $ErrorActionPreference = $prev
    }
}

# Parse "v1.2.3" / "v1.2.3-rc.4" into comparable parts.
# Returns $null for tags that do not follow SemVer (e.g. "v0.8.8.5.1").
function Get-SemVer([string]$TagName) {
    if ($TagName -notmatch '^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$') {
        return $null
    }
    return [pscustomobject]@{
        Major = [int]$Matches[1]
        Minor = [int]$Matches[2]
        Patch = [int]$Matches[3]
        Pre   = [string]$Matches[4]
    }
}

# Precedence used to rank upstream tags. Prerelease identifiers are dot
# separated: two purely numeric segments compare numerically (rc.9 < rc.30),
# any comparison involving a segment with letters is ordinal
# (rc.12 < rc.19-i18nfix.2 < rc.30), and a version without a prerelease is
# greater than one with it (v1.0.0 > v1.0.0-rc.30).
# Returns -1, 0 or 1.
function Compare-SemVer($A, $B) {
    if ($A.Major -ne $B.Major) { return [Math]::Sign([long]$A.Major - [long]$B.Major) }
    if ($A.Minor -ne $B.Minor) { return [Math]::Sign([long]$A.Minor - [long]$B.Minor) }
    if ($A.Patch -ne $B.Patch) { return [Math]::Sign([long]$A.Patch - [long]$B.Patch) }
    $aHasPre = -not [string]::IsNullOrEmpty($A.Pre)
    $bHasPre = -not [string]::IsNullOrEmpty($B.Pre)
    if ($aHasPre -and -not $bHasPre) { return -1 }
    if (-not $aHasPre -and $bHasPre) { return 1 }
    if (-not $aHasPre -and -not $bHasPre) { return 0 }
    $aParts = $A.Pre -split '\.'
    $bParts = $B.Pre -split '\.'
    $count = [Math]::Min($aParts.Count, $bParts.Count)
    for ($i = 0; $i -lt $count; $i++) {
        $aNum = $aParts[$i] -match '^\d+$'
        $bNum = $bParts[$i] -match '^\d+$'
        if ($aNum -and $bNum) {
            $diff = [long]$aParts[$i] - [long]$bParts[$i]
            if ($diff -ne 0) { return [Math]::Sign($diff) }
        } else {
            $ord = [string]::CompareOrdinal($aParts[$i], $bParts[$i])
            if ($ord -ne 0) { return [Math]::Sign($ord) }
        }
    }
    if ($aParts.Count -ne $bParts.Count) {
        return [Math]::Sign([long]$aParts.Count - [long]$bParts.Count)
    }
    return 0
}

# Resolve the commit a tag points at. For an annotated tag the first
# ls-remote line holds the tag object SHA, which is NOT a commit; the peeled
# "^{}" line is the one that must be used.
function Get-TagCommit([string]$Tag) {
    $result = Invoke-Git @("ls-remote", "upstream", "refs/tags/$Tag", "refs/tags/$Tag^{}")
    if ($result.ExitCode -ne 0) { return $null }
    $plain = $null
    foreach ($line in @($result.Output | Where-Object { $_ })) {
        $fields = $line -split "`t"
        if ($fields.Count -lt 2) { continue }
        $ref = $fields[1]
        if ($ref -eq "refs/tags/$Tag^{}") { return $fields[0].Trim() }
        if ($ref -eq "refs/tags/$Tag") { $plain = $fields[0].Trim() }
    }
    return $plain
}

Push-Location $RepoRoot
try {
    # --- 0. parameter validation ----------------------------------------------
    if ($Check -and -not [string]::IsNullOrEmpty($To)) {
        Write-Err "-Check and -To are mutually exclusive."
        exit 1
    }
    $updateMode = -not [string]::IsNullOrEmpty($To)

    # --- 1. read the lock file --------------------------------------------------
    if (-not (Test-Path $LockPath)) {
        Write-Err "upstream.lock.json not found at $LockPath."
        exit 1
    }
    $Lock = Get-Content $LockPath -Raw | ConvertFrom-Json
    if (-not $Lock.upstream -or -not $Lock.upstream.tag -or -not $Lock.upstream.commit) {
        Write-Err "upstream.lock.json is missing upstream.tag or upstream.commit."
        exit 1
    }
    $BaselineTag = [string]$Lock.upstream.tag
    $BaselineCommit = [string]$Lock.upstream.commit
    $UpstreamRepo = [string]$Lock.upstream.repo
    $UpstreamBranch = [string]$Lock.upstream.branch
    if ([string]::IsNullOrEmpty($UpstreamBranch)) { $UpstreamBranch = "main" }
    Write-Info "Current baseline: $BaselineTag ($($BaselineCommit.Substring(0, 9)))"

    # --- 2. -To idempotency: target equals baseline => nothing to do -----------
    if ($updateMode -and $To -eq $BaselineTag) {
        Write-Info "Baseline is already $BaselineTag. Nothing to do."
        exit 0
    }

    # --- 3. ensure the upstream remote exists (idempotent) ----------------------
    $remoteProbe = Invoke-Git @("remote", "get-url", "upstream")
    $remoteUrl = $null
    if ($remoteProbe.ExitCode -eq 0 -and $remoteProbe.Output.Count -gt 0 -and $remoteProbe.Output[0]) {
        $remoteUrl = "$($remoteProbe.Output[0])".Trim()
    }
    if ($remoteUrl) {
        Write-Info "upstream remote exists -> $remoteUrl"
    } else {
        if ([string]::IsNullOrEmpty($UpstreamRepo)) {
            Write-Err "upstream.repo is missing in the lock file; cannot add the upstream remote."
            exit 1
        }
        $addResult = Invoke-Git @("remote", "add", "upstream", $UpstreamRepo)
        if ($addResult.ExitCode -ne 0) {
            Write-Err "Failed to add upstream remote -> $UpstreamRepo."
            exit 1
        }
        Write-Info "Added upstream remote -> $UpstreamRepo"
    }

    # --- 4. fetch the branch only; --no-tags is mandatory -----------------------
    $fetchResult = Invoke-Git @("fetch", "upstream", $UpstreamBranch, "--no-tags")
    if ($fetchResult.ExitCode -ne 0) {
        Write-Err "git fetch upstream $UpstreamBranch --no-tags failed (network unreachable?)."
        exit 1
    }
    Write-Info "Fetched upstream/$UpstreamBranch with --no-tags; local tag namespace untouched."

    # --- 5. read-only upstream tag list -----------------------------------------
    $tagsResult = Invoke-Git @("ls-remote", "--tags", "upstream")
    if ($tagsResult.ExitCode -ne 0) {
        Write-Err "git ls-remote --tags upstream failed (network unreachable?)."
        exit 1
    }
    $tagNames = @()
    foreach ($line in @($tagsResult.Output | Where-Object { $_ })) {
        $fields = $line -split "`t"
        if ($fields.Count -lt 2) { continue }
        $ref = $fields[1]
        if (-not $ref.StartsWith("refs/tags/")) { continue }
        if ($ref.EndsWith("^{}")) { continue }
        $tagNames += $ref.Substring("refs/tags/".Length)
    }
    $tagNames = @($tagNames | Sort-Object -Unique)
    if ($tagNames.Count -eq 0) {
        Write-Err "No tags found on upstream."
        exit 1
    }

    # --- 6. SemVer parsing; pick the latest upstream release --------------------
    $parsedTags = @()
    foreach ($name in $tagNames) {
        $semVer = Get-SemVer $name
        if ($null -eq $semVer) {
            Write-Warn "skipped $name"
            continue
        }
        $parsedTags += [pscustomobject]@{ Tag = $name; SemVer = $semVer }
    }
    if ($parsedTags.Count -eq 0) {
        Write-Err "No SemVer-parsable release tags on upstream."
        exit 1
    }
    $latest = $parsedTags[0]
    foreach ($item in $parsedTags) {
        if ((Compare-SemVer $item.SemVer $latest.SemVer) -gt 0) { $latest = $item }
    }

    # --- 7. read-only report (-Check / no arguments) -----------------------------
    if (-not $updateMode) {
        Write-Info "Upstream latest release: $($latest.Tag)"
        $baselineSemVer = Get-SemVer $BaselineTag
        if ($null -eq $baselineSemVer) {
            Write-Warn "baseline tag $BaselineTag is not SemVer-parsable; behind count skipped."
        } else {
            $behind = 0
            foreach ($item in $parsedTags) {
                if ((Compare-SemVer $item.SemVer $baselineSemVer) -gt 0) { $behind++ }
            }
            Write-Info "Behind baseline by $behind upstream release(s)."
        }
        $upstreamSha = Get-TagCommit $BaselineTag
        if ($upstreamSha -and $upstreamSha -eq $BaselineCommit) {
            Write-Info "baseline commit $($BaselineCommit.Substring(0, 9)) matches upstream $BaselineTag"
        } else {
            Write-Warn "baseline drift: lock.commit does not match upstream $BaselineTag (tampered lock or moved/deleted tag)."
            exit 1
        }
        exit 0
    }

    # --- 8. -To <tag>: move the baseline -----------------------------------------
    if ($tagNames -notcontains $To) {
        Write-Err "Tag $To does not exist on upstream."
        exit 1
    }
    $targetCommit = Get-TagCommit $To
    if (-not $targetCommit) {
        Write-Err "Failed to resolve the commit for $To."
        exit 1
    }
    Write-Info "Target: $To -> $($targetCommit.Substring(0, 9))"
    $answer = Read-Host "[INPUT] Update baseline $BaselineTag -> $To? Type y to confirm"
    if ($answer -ne "y") {
        Write-Info "Aborted. Lock file not modified."
        exit 0
    }
    $Lock.upstream.tag = $To
    $Lock.upstream.commit = $targetCommit
    $Lock.syncedAt = Get-Date -Format "yyyy-MM-dd"
    # Write UTF-8 without BOM: PS 5.1 "Set-Content -Encoding UTF8" prepends a
    # BOM, which breaks jq and other strict JSON parsers that read the lock.
    $json = $Lock | ConvertTo-Json -Depth 5
    [System.IO.File]::WriteAllText($LockPath, $json)
    Write-Info "upstream.lock.json updated: $BaselineTag -> $To."
    Write-Host ""
    Write-Host "Manual merge guide:" -ForegroundColor Cyan
    Write-Host "  git merge $targetCommit    # resolve conflicts, then commit"
    Write-Host "  git push origin main"
    exit 0
} finally {
    Pop-Location
}
