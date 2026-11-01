$ErrorActionPreference = 'Stop'

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
if ($architecture -eq 'x64') { $architecture = 'amd64' }
if ($architecture -ne 'amd64' -and $architecture -ne 'arm64') {
    throw "Unsupported CPU architecture: $architecture"
}

$asset = "english-guard_windows_$architecture.zip"
$base = 'https://github.com/rebrudiy/codex-english-guard/releases/latest/download'
$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $temporary | Out-Null

try {
    $archive = Join-Path $temporary $asset
    $checksums = Join-Path $temporary 'checksums.txt'
    Invoke-WebRequest "$base/$asset" -OutFile $archive
    Invoke-WebRequest "$base/checksums.txt" -OutFile $checksums
    $line = Get-Content $checksums | Where-Object { $_ -match "^[0-9a-fA-F]{64}  $([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $line) { throw "Release checksum for $asset is missing" }
    $expected = $line.Substring(0, 64).ToLowerInvariant()
    $actual = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw 'Release checksum mismatch' }

    Expand-Archive -Path $archive -DestinationPath $temporary
    & (Join-Path $temporary 'english-guard.exe') install @args
    if ($LASTEXITCODE -ne 0) { throw "Installer failed with exit code $LASTEXITCODE" }
}
finally {
    Remove-Item -LiteralPath $temporary -Recurse -Force -ErrorAction SilentlyContinue
}
