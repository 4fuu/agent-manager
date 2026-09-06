[CmdletBinding()]
param(
    [Parameter(Position=0)][string]$Version = "latest",
    [string]$InstallDir = $(if ($env:AGENT_MANAGER_INSTALL_DIR) { $env:AGENT_MANAGER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\agent-manager" })
)
$ErrorActionPreference = "Stop"
$repo = "4fuu/agent-manager"
$fixture = $env:AGENT_MANAGER_FIXTURE_DIR
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

if ($fixture -and -not [IO.Path]::IsPathRooted($fixture)) { throw "AGENT_MANAGER_FIXTURE_DIR must be an absolute local path" }
if ($Version -eq "latest") {
    if ($fixture) { $Version = (Get-Content -Raw (Join-Path $fixture "LATEST")).Trim() }
    else {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        $Version = (Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$repo/releases/latest").tag_name
    }
}
$Version = $Version -replace '^v',''
if ($Version -notmatch '^(\d{4})\.([1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\.(0|[1-9]\d*)$') { throw "invalid version '$Version' (expected YYYY.MDD.REVISION)" }
$null = [datetime]::new([int]$Matches[1], [int]$Matches[2], [int]$Matches[3])
if (-not [Environment]::Is64BitOperatingSystem -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { throw "unsupported target: Windows requires amd64" }

$archive = "agent-manager-$Version-windows-amd64.zip"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("agent-manager-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $tmp | Out-Null
try {
    $archivePath = Join-Path $tmp $archive
    $manifestPath = Join-Path $tmp "SHA256SUMS"
    if ($fixture) {
        Copy-Item (Join-Path $fixture $archive) $archivePath
        Copy-Item (Join-Path $fixture "SHA256SUMS") $manifestPath
    } else {
        $base = "https://github.com/$repo/releases/download/v$Version"
        Invoke-WebRequest -UseBasicParsing "$base/$archive" -OutFile $archivePath
        Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS" -OutFile $manifestPath
    }
    $hashes = @(Get-Content $manifestPath | ForEach-Object {
        if ($_ -match '^([0-9A-Fa-f]{64})  (.+)$' -and $Matches[2] -ceq $archive) { $Matches[1].ToLowerInvariant() }
    })
    if ($hashes.Count -ne 1) { throw "malformed checksum manifest or missing exact entry for $archive" }
    $actual = (Get-FileHash -Algorithm SHA256 $archivePath).Hash.ToLowerInvariant()
    if ($actual -cne $hashes[0]) { throw "checksum verification failed for $archive" }
    $unpack = Join-Path $tmp "unpack"
    Expand-Archive -LiteralPath $archivePath -DestinationPath $unpack
    $candidate = Join-Path $unpack "agent-manager.exe"
    if (-not (Test-Path -LiteralPath $candidate -PathType Leaf) -or -not (Test-Path (Join-Path $unpack 'README.md') -PathType Leaf) -or -not (Test-Path (Join-Path $unpack 'docs') -PathType Container)) { throw "archive layout is invalid" }
    $reported = & $candidate --version 2>$null
    if ($LASTEXITCODE -ne 0 -or ($reported -join "`n") -notmatch ('^agent-manager ' + [regex]::Escape($Version) + '( \(|$)')) { throw "downloaded executable failed its version test or reports the wrong version" }

    New-Item -ItemType Directory -Force $InstallDir | Out-Null
    $destination = Join-Path $InstallDir "agent-manager.exe"
    $staged = Join-Path $InstallDir (".agent-manager-" + [guid]::NewGuid().ToString('N') + '.exe')
    Copy-Item $candidate $staged
    try {
        if (Test-Path -LiteralPath $destination) {
            $backup = $staged + '.previous'
            [IO.File]::Replace($staged, $destination, $backup)
            Remove-Item -LiteralPath $backup -Force -ErrorAction SilentlyContinue
        } else { [IO.File]::Move($staged, $destination) }
    } catch {
        Remove-Item -Force -ErrorAction SilentlyContinue $staged
        throw "could not activate agent-manager.exe (is it running?); the existing installation was preserved: $($_.Exception.Message)"
    }

    if (-not $fixture) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $parts = @($userPath -split ';' | Where-Object { $_ })
        if ($parts -notcontains $InstallDir) {
            try { [Environment]::SetEnvironmentVariable('Path', (($parts + $InstallDir) -join ';'), 'User'); Write-Host "Added $InstallDir to your user PATH; open a new terminal." }
            catch { Write-Warning "Could not update user PATH. Add '$InstallDir' manually." }
        }
    }
    Write-Host "Installed agent-manager $Version to $destination"
    Write-Host "Next: agent-manager runtime-install`n      agent-manager doctor`n      agent-manager serve"
} finally { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
