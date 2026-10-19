# Nemalo installer for Windows PowerShell 5.1 and PowerShell 7.
#
#   irm https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.ps1))) -Version 0.1.0-alpha.3
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.ps1))) -Uninstall
#
# Downloads the published release archive for this platform, verifies it against
# the release SHA256SUMS, and installs the single nemalo.exe. The install
# directory is added to the user PATH unless -NoPath is given. Nothing else is
# installed, and no library, intake folder, configuration, or state is touched.
# NEMALO_INSTALL_DIR overrides the default, %LOCALAPPDATA%\Programs\nemalo.
param(
    [string]$Version = "",
    [string]$Dir = "",
    [switch]$Uninstall,
    [switch]$NoPath
)

& {
$ErrorActionPreference = "Stop"
$Repo = "blisspixel/nemalo"

function Fail([string]$Message, [string]$Hint = "") {
    [Console]::Error.WriteLine("nemalo install: $Message")
    if ($Hint) { [Console]::Error.WriteLine("  hint: $Hint") }
    throw "nemalo install: $Message"
}

function Compare-VersionNumber([string]$A, [string]$B) {
    $A = $A.TrimStart("0")
    $B = $B.TrimStart("0")
    if ($A -eq "") { $A = "0" }
    if ($B -eq "") { $B = "0" }
    if ($A.Length -ne $B.Length) { return $A.Length.CompareTo($B.Length) }
    return [string]::CompareOrdinal($A, $B)
}

# Compare-NemaloVersion returns 1 when A is a higher semantic version than B.
function Compare-NemaloVersion([string]$A, [string]$B) {
    $pattern = '^([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.]+))?$'
    $am = [regex]::Match($A, $pattern)
    $bm = [regex]::Match($B, $pattern)
    if (-not $am.Success -or -not $bm.Success) { return 0 }
    foreach ($i in 1, 2, 3) {
        $cmp = Compare-VersionNumber $am.Groups[$i].Value $bm.Groups[$i].Value
        if ($cmp -ne 0) { return $cmp }
    }
    $ap = $am.Groups[4].Value
    $bp = $bm.Groups[4].Value
    if ($ap -eq "" -and $bp -eq "") { return 0 }
    if ($ap -eq "") { return 1 }
    if ($bp -eq "") { return -1 }
    $left = $ap.Split(".")
    $right = $bp.Split(".")
    $count = [Math]::Min($left.Length, $right.Length)
    for ($i = 0; $i -lt $count; $i++) {
        $leftNumber = $left[$i] -match '^[0-9]+$'
        $rightNumber = $right[$i] -match '^[0-9]+$'
        if ($leftNumber -and $rightNumber) {
            $cmp = Compare-VersionNumber $left[$i] $right[$i]
        } elseif ($leftNumber) {
            $cmp = -1
        } elseif ($rightNumber) {
            $cmp = 1
        } else {
            $cmp = [string]::CompareOrdinal($left[$i], $right[$i])
        }
        if ($cmp -ne 0) { return $cmp }
    }
    return $left.Length.CompareTo($right.Length)
}

if (-not $Dir) { $Dir = $env:NEMALO_INSTALL_DIR }
if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA "Programs\nemalo" }
if (-not [IO.Path]::IsPathRooted($Dir)) { Fail "install directory must be an absolute path: $Dir" }
$Dir = [IO.Path]::GetFullPath($Dir)
$Exe = Join-Path $Dir "nemalo.exe"

# The user PATH is edited in the registry directly so entries such as
# %USERPROFILE%\bin stay unexpanded and an ExpandString value keeps its kind.
# [Environment]::SetEnvironmentVariable would rewrite it as expanded REG_SZ.
function Get-UserPathParts {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Environment")
    if (-not $key) { return @() }
    try {
        $value = $key.GetValue("Path", "", [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    } finally { $key.Close() }
    if (-not $value) { return @() }
    return @(([string]$value).Split(";") | Where-Object { $_ })
}

function Set-UserPathParts([string[]]$Parts) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey("Environment")
    try {
        $kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
        if ($key.GetValueNames() -contains "Path") { $kind = $key.GetValueKind("Path") }
        $key.SetValue("Path", ($Parts -join ";"), $kind)
    } finally { $key.Close() }
    # Setting and clearing a variable broadcasts the settings change so new
    # terminals observe the edited PATH without signing out.
    [Environment]::SetEnvironmentVariable("NEMALO_INSTALL_REFRESH", "1", "User")
    [Environment]::SetEnvironmentVariable("NEMALO_INSTALL_REFRESH", $null, "User")
}

function Test-SameDir([string]$A, [string]$B) {
    return [Environment]::ExpandEnvironmentVariables($A).TrimEnd("\") -ieq $B.TrimEnd("\")
}

if ($Uninstall) {
    if (Test-Path -LiteralPath $Exe) {
        Remove-Item -LiteralPath $Exe -Force
        Write-Host "removed $Exe"
    } else {
        Write-Host "nothing to remove at $Exe"
    }
    if ((Test-Path -LiteralPath $Dir) -and -not (Get-ChildItem -LiteralPath $Dir -Force)) {
        Remove-Item -LiteralPath $Dir -Force
    }
    $parts = Get-UserPathParts
    $kept = @($parts | Where-Object { -not (Test-SameDir $_ $Dir) })
    if ($kept.Count -ne $parts.Count) {
        Set-UserPathParts $kept
        Write-Host "removed $Dir from the user PATH"
    }
    Write-Host "Libraries, intake folders, configuration, and state were not changed."
    return
}

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch {}

$arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64" -or $env:PROCESSOR_ARCHITEW6432 -eq "ARM64") {
    $arch = "arm64"
} elseif (-not [Environment]::Is64BitOperatingSystem) {
    Fail "unsupported 32-bit Windows" "releases provide amd64 and arm64"
}

if (-not $Version) {
    # /releases/latest excludes prereleases. The list endpoint's order is not
    # semantic version order: on 2026-10-13 its first item was v0.1.0-alpha.9
    # after v0.1.0-alpha.10 was published. Choose the highest version.
    # A GITHUB_TOKEN, when set, is sent only to api.github.com, to avoid the
    # unauthenticated rate limit on shared networks such as CI runners.
    $headers = @{}
    if ($env:GITHUB_TOKEN) { $headers["Authorization"] = "Bearer $($env:GITHUB_TOKEN)" }
    # A list that names no release is asked for again, twice: the list API has
    # briefly answered with none while releases existed.
    $best = ""
    foreach ($attempt in 1..3) {
        try {
            # PowerShell may return the JSON array as one value. Pipe it so each
            # release is its own object; a nested array would hide every tag.
            $releases = @(Invoke-RestMethod -UseBasicParsing -Headers $headers -Uri "https://api.github.com/repos/$Repo/releases?per_page=100" | ForEach-Object { $_ })
        } catch {
            Fail "could not query the release list" "pass -Version VERSION to skip the lookup"
        }
        if ($releases.Count -ge 100) {
            Fail "the release list is too long to choose a version" "pass -Version VERSION"
        }
        foreach ($release in $releases) {
            if ($release.draft) { continue }
            $tag = [string]$release.tag_name
            if ($tag -notmatch '^v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?)$') { continue }
            $candidate = $Matches[1]
            if ($best -eq "" -or (Compare-NemaloVersion $candidate $best) -gt 0) { $best = $candidate }
        }
        if ($best) { break }
        if ($attempt -lt 3) { Start-Sleep -Seconds (2 * $attempt) }
    }
    if (-not $best) { Fail "no published release found" }
    $Version = $best
}
$Version = $Version -replace '^v', ''
if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$') { Fail "invalid version $Version" }

$asset = "nemalo_${Version}_windows_${arch}.zip"
$base = "https://github.com/$Repo/releases/download/v$Version"
# Save-Url downloads one URL to a new file. A dropped connection, a timeout,
# or a server error is tried again up to three times with a growing pause,
# each time from an empty file. A missing release and a failure to write the
# file are reported as such, since a retry cannot help. An empty response
# is a failure.
function Save-Url([string]$Uri, [string]$Path, [string]$Missing) {
    for ($attempt = 1; ; $attempt++) {
        Remove-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
        $problem = "the response was empty"
        try {
            Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Path -TimeoutSec 120
            if ((Test-Path -LiteralPath $Path) -and (Get-Item -LiteralPath $Path).Length -gt 0) { return }
        } catch [System.IO.IOException] {
            Fail "could not write $Path" "the temporary folder may be full; set TEMP to a folder with free space"
        } catch {
            $status = $null
            if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
            if ($status -eq 404) { Fail $Missing "check the version, or omit -Version for the newest" }
            $problem = $_.Exception.Message
        }
        Remove-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
        if ($attempt -ge 3) { Fail "could not download $Uri ($problem)" "check the network and retry" }
        Start-Sleep -Seconds (2 * $attempt)
    }
}

# A temporary folder that cannot be created falls back to the local
# application data folder.
$work = $null
foreach ($parent in @([IO.Path]::GetTempPath(), $env:LOCALAPPDATA)) {
    if (-not $parent) { continue }
    $candidate = Join-Path $parent ("nemalo-install-" + [Guid]::NewGuid().ToString("N"))
    try {
        New-Item -ItemType Directory -Path $candidate -ErrorAction Stop | Out-Null
        $work = $candidate
        break
    } catch {}
}
if (-not $work) { Fail "could not create a temporary folder" "set TEMP to a folder with free space" }
try {
    Write-Host "Installing Nemalo $Version (windows/$arch) into $Dir"
    $archive = Join-Path $work $asset
    Save-Url "$base/SHA256SUMS" (Join-Path $work "SHA256SUMS") "release $Version was not found at $base"
    Save-Url "$base/$asset" $archive "release $Version has no archive $asset"
    $entries = @(Get-Content -LiteralPath (Join-Path $work "SHA256SUMS") | ForEach-Object {
        if ($_ -match '^(?<digest>[0-9A-Fa-f]{64}) [ *](?<file>\S+)$' -and $Matches.file -eq $asset) { $Matches.digest.ToLower() }
    })
    if ($entries.Count -ne 1) { Fail "SHA256SUMS does not list exactly one valid digest for $asset" }
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLower()
    if ($actual -cne $entries[0]) {
        Fail "checksum mismatch for $asset (expected $($entries[0]), got $actual)" "the download may be corrupt; retry"
    }
    Write-Host "Verified SHA-256 $actual"

    $extract = Join-Path $work "extract"
    Expand-Archive -LiteralPath $archive -DestinationPath $extract
    $candidate = Join-Path $extract "nemalo.exe"
    if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) { Fail "the archive does not contain nemalo.exe" }
    & $candidate version | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "the downloaded executable does not run on this system" }

    New-Item -ItemType Directory -Force -Path $Dir | Out-Null
    $staged = Join-Path $Dir (".nemalo.install." + [Guid]::NewGuid().ToString("N") + ".exe")
    Copy-Item -LiteralPath $candidate -Destination $staged
    Move-Item -LiteralPath $staged -Destination $Exe -Force
    Write-Host "Installed $Exe ($(& $Exe version))"
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}

$onPath = @(Get-UserPathParts | Where-Object { Test-SameDir $_ $Dir }).Count -gt 0
if (-not $onPath -and -not $NoPath) {
    Set-UserPathParts (@($Dir) + (Get-UserPathParts))
    Write-Host "Added $Dir to the user PATH. Open a new terminal to use it."
} elseif (-not $onPath) {
    Write-Host "Left the user PATH unchanged (-NoPath)."
}
if (-not (($env:Path.Split(";") | Where-Object { $_ -and (Test-SameDir $_ $Dir) }))) {
    $env:Path = "$Dir;$env:Path"
}
Write-Host "Next: nemalo doctor"
}
