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
    # Releases are currently prereleases, which /releases/latest excludes. The
    # list endpoint returns the newest release first, prereleases included.
    # A GITHUB_TOKEN, when set, is sent only to api.github.com, to avoid the
    # unauthenticated rate limit on shared networks such as CI runners.
    $headers = @{}
    if ($env:GITHUB_TOKEN) { $headers["Authorization"] = "Bearer $($env:GITHUB_TOKEN)" }
    try {
        $releases = Invoke-RestMethod -UseBasicParsing -Headers $headers -Uri "https://api.github.com/repos/$Repo/releases?per_page=1"
    } catch {
        Fail "could not query the release list" "pass -Version VERSION to skip the lookup"
    }
    $Version = @($releases)[0].tag_name
    if (-not $Version) { Fail "no published release found" }
}
$Version = $Version -replace '^v', ''
if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$') { Fail "invalid version $Version" }

$asset = "nemalo_${Version}_windows_${arch}.zip"
$base = "https://github.com/$Repo/releases/download/v$Version"
$work = Join-Path ([IO.Path]::GetTempPath()) ("nemalo-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $work | Out-Null
try {
    Write-Host "Installing Nemalo $Version (windows/$arch) into $Dir"
    $archive = Join-Path $work $asset
    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $archive
        Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $work "SHA256SUMS")
    } catch {
        Fail "could not download the release assets from $base" "check the version and your connection"
    }
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
