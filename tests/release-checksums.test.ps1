$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../scripts/release-checksums.ps1')
$parent = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../.cache-release'))
$root = Join-Path $parent ('xroute-checksums-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
function Add-Architecture([string]$dir, [string]$arch) {
    $image = Join-Path $dir "xroute-1.7.2-linux-$arch.tar"
    [IO.File]::WriteAllText($image, "image-$arch")
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $image).Hash.ToLowerInvariant()
    @{version='1.7.2'; platform="linux/$arch"; sha256=$hash; bytes=(Get-Item $image).Length} | ConvertTo-Json | Set-Content (Join-Path $dir "xroute-1.7.2-linux-$arch.manifest.json")
    $bundle = 'xroute-1.7.2-routeros-bundle.zip'
    if ($arch -eq 'amd64') { $bundle = 'xroute-1.7.2-routeros-amd64-bundle.zip' }
    [IO.File]::WriteAllText((Join-Path $dir $bundle), 'bundle')
    [IO.File]::WriteAllText((Join-Path $dir 'xroute-1.7.2-source.zip'), 'source')
    [IO.File]::WriteAllText((Join-Path $dir 'RELEASE-NOTES.md'), 'notes')
}
try {
    Add-Architecture $root 'arm64'
    Write-XRouteReleaseChecksums $root '1.7.2'
    $first = [IO.File]::ReadAllText((Join-Path $root 'SHA256SUMS'))
    Add-Architecture $root 'amd64'
    Write-XRouteReleaseChecksums $root '1.7.2'
    $combined = [IO.File]::ReadAllText((Join-Path $root 'SHA256SUMS'))
    foreach ($line in $first.Trim().Split("`n")) {
        if (-not $combined.Contains($line.TrimEnd("`r"))) { throw 'The second architecture lost an ARM64 checksum.' }
    }
    if ($combined.Trim().Split("`n").Count -ne 8 -or -not $combined.Contains('-linux-amd64.tar') -or $combined.Contains('-install-')) { throw 'Incorrect combined checksums.' }
    [IO.File]::AppendAllText((Join-Path $root 'xroute-1.7.2-linux-arm64.tar'), 'corrupted')
    $rejected = $false
    try { Write-XRouteReleaseChecksums $root '1.7.2' } catch { $rejected = $true }
    if (-not $rejected -or [IO.File]::ReadAllText((Join-Path $root 'SHA256SUMS')) -ne $combined) { throw 'Invalid prior assets changed the shared checksum file.' }
    $single = Join-Path $root 'amd64-only'
    New-Item -ItemType Directory -Path $single | Out-Null
    Add-Architecture $single 'amd64'
    Write-XRouteReleaseChecksums $single '1.7.2'
    if ((Test-Path (Join-Path $single 'SHA256SUMS')) -or -not (Test-Path (Join-Path $single 'SHA256SUMS-amd64'))) { throw 'An AMD64-only package could replace shared checksums.' }
    [IO.File]::WriteAllText((Join-Path $single 'SHA256SUMS'), 'keep-existing')
    $rejected = $false
    try { Write-XRouteReleaseChecksums $single '1.7.2' } catch { $rejected = $true }
    if (-not $rejected -or [IO.File]::ReadAllText((Join-Path $single 'SHA256SUMS')) -ne 'keep-existing') { throw 'Existing shared checksums were overwritten.' }
    Write-Output 'MULTI_ARCH_CHECKSUMS_PASS'
} finally {
    if ([IO.Path]::GetFullPath($root).StartsWith($parent + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $root -Leaf) -like 'xroute-checksums-*' -and (Test-Path -LiteralPath $root)) {
        Remove-Item -LiteralPath $root -Recurse -Force
    }
}
