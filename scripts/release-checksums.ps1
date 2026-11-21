function Write-XRouteReleaseChecksums {
    param([string]$Directory, [string]$Version)
    $ErrorActionPreference = 'Stop'
    if ($Version -notmatch '^\d+\.\d+\.\d+(?:-rc\.[1-9][0-9]*)?$') { throw 'Invalid release version.' }
    $common = @("xroute-$Version-source.zip", 'RELEASE-NOTES.md')
    $sets = @{}
    foreach ($arch in @('arm64', 'amd64')) {
        $bundle = "xroute-$Version-routeros-bundle.zip"
        if ($arch -eq 'amd64') { $bundle = "xroute-$Version-routeros-amd64-bundle.zip" }
        $names = @("xroute-$Version-linux-$arch.tar", "xroute-$Version-linux-$arch.manifest.json", $bundle)
        $present = @($names | Where-Object { Test-Path -LiteralPath (Join-Path $Directory $_) -PathType Leaf })
        if ($present.Count -eq 0) { continue }
        if ($present.Count -ne $names.Count) { throw "Incomplete $arch release assets; checksums were not changed." }
        $manifest = Get-Content -Raw -LiteralPath (Join-Path $Directory $names[1]) | ConvertFrom-Json
        $image = Get-Item -LiteralPath (Join-Path $Directory $names[0])
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $image.FullName).Hash.ToLowerInvariant()
        if ($manifest.version -ne $Version -or $manifest.platform -ne "linux/$arch" -or $manifest.sha256 -ne $hash -or $manifest.bytes -ne $image.Length) {
            throw "Invalid $arch image manifest; checksums were not changed."
        }
        $sets[$arch] = $names
    }
    if ($sets.Count -eq 0) { throw 'No complete release architecture.' }
    $all = @($common) + @($sets.Values | ForEach-Object { $_ })
    $lines = @{}
    foreach ($name in $all) {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $Directory $name)).Hash.ToLowerInvariant()
        $lines[$name] = "$hash  $name"
    }
    # An AMD64-only package must never replace an ARM64 release's shared file.
    if ($sets.Count -eq 1 -and $sets.ContainsKey('amd64') -and (Test-Path -LiteralPath (Join-Path $Directory 'SHA256SUMS'))) {
        throw 'Shared SHA256SUMS requires the ARM64 assets too; use a separate AMD64 output directory.'
    }
    foreach ($arch in $sets.Keys) {
        $selected = @($common) + @($sets[$arch]) | Sort-Object -Unique
        [IO.File]::WriteAllLines((Join-Path $Directory "SHA256SUMS-$arch"), [string[]]@($selected | ForEach-Object { $lines[$_] }), [Text.UTF8Encoding]::new($false))
    }
    if ($sets.ContainsKey('arm64')) {
        [IO.File]::WriteAllLines((Join-Path $Directory 'SHA256SUMS'), [string[]]@($lines.Keys | Sort-Object | ForEach-Object { $lines[$_] }), [Text.UTF8Encoding]::new($false))
    }
}
