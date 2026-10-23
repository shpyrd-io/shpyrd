# Native Windows installation. Run in PowerShell 5.1 or later:
# irm https://shpyrd.io/install.ps1 | iex
& {
    $ErrorActionPreference = 'Stop'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $nativeArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($nativeArch.ToUpperInvariant()) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "Unsupported Windows architecture: $nativeArch. Use 64-bit Windows (x64 or ARM64)." }
    }
    if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set. Run this in Windows PowerShell.' }
    $repository = 'https://github.com/shpyrd-io/shpyrd'
    # Resolve the release once, then fetch the archive and checksum from that tag.
    $response = Invoke-WebRequest "$repository/releases/latest" -UseBasicParsing
    $finalUri = if ($response.BaseResponse.ResponseUri) { $response.BaseResponse.ResponseUri } else { $response.BaseResponse.RequestMessage.RequestUri }
    $version = $finalUri.AbsolutePath.TrimEnd('/').Split('/')[-1]
    if ($version -notmatch '^v[0-9][a-zA-Z0-9.+-]*$') { throw 'Could not resolve the latest shpyrd release.' }
    $archive = "shpyrd_${version}_windows_${arch}.zip"
    $release = "$repository/releases/download/$version"
    $temp = Join-Path ([IO.Path]::GetTempPath()) ('shpyrd-' + [Guid]::NewGuid().ToString('N'))
    $destination = Join-Path $env:LOCALAPPDATA 'shpyrd\bin'
    try {
        New-Item -ItemType Directory -Path $temp -Force | Out-Null
        $zip = Join-Path $temp $archive
        Invoke-WebRequest "$release/$archive" -OutFile $zip -UseBasicParsing
        $checksums = (Invoke-WebRequest "$release/checksums.txt" -UseBasicParsing).Content
        if ($checksums -is [byte[]]) { $checksums = [Text.Encoding]::UTF8.GetString($checksums) }
        $pattern = '(?m)^([a-fA-F0-9]{64})\s+\*?' + [regex]::Escape($archive) + '\r?$'
        $match = [regex]::Match([string]$checksums, $pattern)
        if (-not $match.Success) { throw "$archive is missing from checksums.txt." }
        if ((Get-FileHash $zip -Algorithm SHA256).Hash -ne $match.Groups[1].Value) { throw 'Checksum verification failed. Nothing was installed.' }
        Expand-Archive -LiteralPath $zip -DestinationPath (Join-Path $temp 'extracted')
        $binary = Get-ChildItem (Join-Path $temp 'extracted') -Filter 'shpyrd.exe' -Recurse -File
        if (@($binary).Count -ne 1) { throw 'The release must contain exactly one shpyrd.exe.' }
        New-Item -ItemType Directory -Path $destination -Force | Out-Null
        Copy-Item -LiteralPath $binary.FullName -Destination (Join-Path $destination 'shpyrd.exe') -Force
        $userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $destination) {
            [Environment]::SetEnvironmentVariable('Path', (($userPath.TrimEnd(';') + ';' + $destination).TrimStart(';')), 'User')
        }
        if (($env:Path -split ';') -notcontains $destination) { $env:Path = "$destination;$env:Path" }
        & (Join-Path $destination 'shpyrd.exe') --version
        if ($LASTEXITCODE -ne 0) { throw 'The CLI was installed but could not start.' }
        Write-Host 'Installed. Run shpyrd login to connect or create your workspace.'
    } finally {
        if (Test-Path -LiteralPath $temp) { Remove-Item -LiteralPath $temp -Recurse -Force }
    }
}
