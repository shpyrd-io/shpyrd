# Offline integration test, runnable with PowerShell 5.1 and 7 on Windows.
# On other hosts it validates parsing, downloads, checksums and extraction;
# persistent user PATH assertions run only on Windows.
$ErrorActionPreference = 'Stop'
$scriptPath = Join-Path $PSScriptRoot '../apps/website/public/install.ps1'
$tokens = $null; $parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
$root = Join-Path ([IO.Path]::GetTempPath()) ('shpyrd-script-test-' + [Guid]::NewGuid().ToString('N'))
$oldLocal = $env:LOCALAPPDATA; $oldArch = $env:PROCESSOR_ARCHITECTURE; $oldNativeArch = $env:PROCESSOR_ARCHITEW6432
$oldPath = $env:Path; $oldUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$onWindows = [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT
try {
    New-Item -ItemType Directory -Path $root | Out-Null
    $source = Join-Path $root 'main.go'
    @'
package main
import "fmt"
func main(){fmt.Println("shpyrd test version")}
'@ | Set-Content $source -Encoding ascii
    $fixture = Join-Path $root 'shpyrd.exe'
    & go build -o $fixture $source
    if ($LASTEXITCODE -ne 0) { throw 'Could not build test binary.' }
    $fixtureZip = Join-Path $root 'fixture.zip'
    Compress-Archive -LiteralPath $fixture -DestinationPath $fixtureZip
    $fixtureHash = (Get-FileHash $fixtureZip -Algorithm SHA256).Hash
    $testState = [pscustomobject]@{Requested=@();Tamper=$false}
    function Invoke-WebRequest {
        param([string]$Uri, [string]$OutFile, [switch]$UseBasicParsing)
        $testState.Requested += $Uri
        if ($Uri.EndsWith('/latest')) { return @{BaseResponse=@{ResponseUri=[uri]'https://github.com/shpyrd-io/shpyrd/releases/tag/v1.2.3'}} }
        if ($Uri.EndsWith('/checksums.txt')) {
            $sum = if ($testState.Tamper) { '0' * 64 } else { $fixtureHash }
            return @{Content="$sum  shpyrd_v1.2.3_windows_amd64.zip`n$sum  shpyrd_v1.2.3_windows_arm64.zip`n"}
        }
        if ($Uri -match '/shpyrd_v1.2.3_windows_(amd64|arm64).zip$') { Copy-Item $fixtureZip $OutFile; return }
        throw "Unexpected request $Uri"
    }
    # ZIP mode bits are not retained on macOS; native Windows needs no chmod.
    function Copy-Item {
        [CmdletBinding()]
        param([Parameter(Position=0)][string]$Path,[Parameter(Position=1)][string]$Destination,[string]$LiteralPath,[switch]$Force)
        $inputPath = if ($LiteralPath) { $LiteralPath } else { $Path }
        Microsoft.PowerShell.Management\Copy-Item -LiteralPath $inputPath -Destination $Destination -Force:$Force
        if (-not $onWindows -and $Destination.EndsWith('.exe')) { & chmod +x $Destination }
    }
    foreach ($architecture in @('AMD64','ARM64')) {
        $env:PROCESSOR_ARCHITECTURE = $architecture; $env:PROCESSOR_ARCHITEW6432 = ''
        $env:LOCALAPPDATA = Join-Path $root $architecture
        New-Item -ItemType Directory -Path $env:LOCALAPPDATA | Out-Null
        & $scriptPath
        $bin = Join-Path $env:LOCALAPPDATA 'shpyrd\bin'
        if (-not (Test-Path (Join-Path $bin 'shpyrd.exe'))) { throw 'Binary missing.' }
        if (($env:Path -split ';') -notcontains $bin) { throw 'Current PATH missing installation.' }
        if ($onWindows -and ([Environment]::GetEnvironmentVariable('Path','User') -split ';') -notcontains $bin) { throw 'Persistent PATH missing installation.' }
        & $scriptPath
        if (@($env:Path -split ';' | Where-Object { $_ -eq $bin }).Count -ne 1) { throw 'Duplicate PATH entry on reinstall.' }
    }
    $testState.Tamper = $true
    $env:LOCALAPPDATA = Join-Path $root 'tampered'
    $rejected = $false
    try { & $scriptPath } catch { $rejected = $_.Exception.Message -like '*Checksum*' }
    if (-not $rejected -or (Test-Path (Join-Path $env:LOCALAPPDATA 'shpyrd\bin\shpyrd.exe'))) { throw 'Tampered download was not rejected.' }
    $env:PROCESSOR_ARCHITECTURE = 'x86'
    $rejected = $false
    try { & $scriptPath } catch { $rejected = $_.Exception.Message -like '*Unsupported*' }
    if (-not $rejected) { throw 'Unsupported architecture was accepted.' }
    Write-Host 'PASS: parser, x64/ARM64 downloads, checksum rejection, extraction, execution, PATH and repeat installation.'
} finally {
    $env:LOCALAPPDATA = $oldLocal; $env:PROCESSOR_ARCHITECTURE = $oldArch; $env:PROCESSOR_ARCHITEW6432 = $oldNativeArch
    $env:Path = $oldPath
    if ($onWindows) { [Environment]::SetEnvironmentVariable('Path', $oldUserPath, 'User') }
    Remove-Item function:Invoke-WebRequest -ErrorAction SilentlyContinue
    Remove-Item function:Copy-Item -ErrorAction SilentlyContinue
    if (Test-Path $root) { Remove-Item $root -Recurse -Force }
}
