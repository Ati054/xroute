[CmdletBinding()]
param(
    [string]$RouterOSDirectory = "dist/routeros",
    [string]$PublicSourceDirectory = "dist/public-source",
    [string]$OutputDirectory = "dist/github-release",
    [ValidateSet("arm64", "amd64")][string]$Architecture = "arm64",
    [switch]$SkipWorkspaceCleanup,
    [switch]$Force
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$projectRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$package = Get-Content -Raw -LiteralPath (Join-Path $projectRoot "package.json") | ConvertFrom-Json
$version = [string]$package.version
if ($version -notmatch '^\d+\.\d+\.\d+(?:-rc\.[1-9][0-9]*)?$') {
    throw "package.json must contain a stable or numbered RC version."
}

function Resolve-ProjectPath([string]$Path) {
    if ([System.IO.Path]::IsPathRooted($Path)) {
        return [System.IO.Path]::GetFullPath($Path)
    }
    return [System.IO.Path]::GetFullPath((Join-Path $projectRoot $Path))
}

function Remove-ReleasePath([string]$Path, [string]$AllowedRoot) {
    $resolvedPath = [System.IO.Path]::GetFullPath($Path)
    $resolvedRoot = [System.IO.Path]::GetFullPath($AllowedRoot).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    if (-not $resolvedPath.StartsWith($resolvedRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove a path outside the release output directory: $resolvedPath"
    }
    if (Test-Path -LiteralPath $resolvedPath) {
        Remove-Item -LiteralPath $resolvedPath -Recurse -Force
    }
}

$packageCompleted = $false
Push-Location -LiteralPath $projectRoot
try {
    & git diff --quiet --exit-code
    if ($LASTEXITCODE -ne 0) { throw "Commit tracked changes before packaging the GitHub release." }
    & git diff --cached --quiet --exit-code
    if ($LASTEXITCODE -ne 0) { throw "Commit staged changes before packaging the GitHub release." }

    $routerOSRoot = Resolve-ProjectPath $RouterOSDirectory
    $publicSourceRoot = Resolve-ProjectPath $PublicSourceDirectory
    $outputRoot = Resolve-ProjectPath $OutputDirectory
    New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null

    $archiveName = "xroute-$version-linux-$Architecture.tar"
    $archivePath = Join-Path $routerOSRoot $archiveName
    $archiveChecksumPath = "$archivePath.sha256"
    $manifestPath = Join-Path $routerOSRoot "xroute-$version-linux-$Architecture.manifest.json"
    $installerName = "xroute-$version-install-$Architecture.rsc"
    $installerPath = Join-Path $routerOSRoot $installerName
    $routerOSScripts = Join-Path $routerOSRoot "routeros"
    $sourceArchivePath = Join-Path $publicSourceRoot "xroute-$version-source.zip"
    foreach ($requiredPath in @($archivePath, $archiveChecksumPath, $manifestPath, $installerPath, $routerOSScripts, $sourceArchivePath)) {
        if (-not (Test-Path -LiteralPath $requiredPath)) {
            throw "Missing release input: $requiredPath"
        }
    }

    $archiveHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
    $declaredArchiveHash = ((Get-Content -Raw -LiteralPath $archiveChecksumPath).Trim() -split '\s+')[0].ToLowerInvariant()
    if ($archiveHash -ne $declaredArchiveHash) {
        throw "The container archive does not match its checksum file."
    }
    $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    if ([string]$manifest.version -ne $version -or [string]$manifest.platform -ne "linux/$Architecture" -or [string]$manifest.sha256 -ne $archiveHash) {
        throw "The image manifest does not match the release archive."
    }
    $installerSource = Get-Content -Raw -LiteralPath $installerPath
    foreach ($identity in @((':local releaseVersion "' + $version + '"'), (':local releaseArch "' + $Architecture + '"'), (':local imageSHA256 "' + $archiveHash + '"'), (':local imageBytes ' + (Get-Item -LiteralPath $archivePath).Length))) {
        if (-not $installerSource.Contains($identity)) { throw "The guided installer does not match the image manifest." }
    }
    # Private variables must never enter the recursive copy of offline scripts.
    $maintainedNames = @(Get-ChildItem -LiteralPath (Join-Path $projectRoot "routeros") -Filter '*.rsc' -File | ForEach-Object Name)
    foreach ($file in Get-ChildItem -LiteralPath $routerOSScripts -Recurse -File) {
        $relative = [System.IO.Path]::GetRelativePath($routerOSScripts, $file.FullName).Replace('\', '/')
        if (($relative -notin $maintainedNames) -and (($relative -notlike 'legacy/*') -or ($relative.Substring(7) -notin $maintainedNames))) {
            throw "Unexpected file in public installer scripts: $relative"
        }
    }

    $bundleName = "xroute-$version-routeros-bundle.zip"
    if ($Architecture -eq "amd64") { $bundleName = "xroute-$version-routeros-amd64-bundle.zip" }
    $bundlePath = Join-Path $outputRoot $bundleName
    $stagingRoot = Join-Path $outputRoot ".bundle-staging"
    foreach ($target in @($bundlePath, "$bundlePath.sha256", (Join-Path $outputRoot $archiveName), (Join-Path $outputRoot "$archiveName.sha256"), (Join-Path $outputRoot $installerName), (Join-Path $outputRoot "$installerName.sha256"), (Join-Path $outputRoot ([System.IO.Path]::GetFileName($manifestPath))), (Join-Path $outputRoot ([System.IO.Path]::GetFileName($sourceArchivePath))), (Join-Path $outputRoot ([System.IO.Path]::GetFileName($sourceArchivePath) + ".sha256")), (Join-Path $outputRoot "RELEASE-NOTES.md"), (Join-Path $outputRoot "SHA256SUMS"), $stagingRoot)) {
        if (Test-Path -LiteralPath $target) {
            if (-not $Force) { throw "Release output already exists; use -Force to replace it: $target" }
            Remove-ReleasePath $target $outputRoot
        }
    }

    New-Item -ItemType Directory -Path $stagingRoot | Out-Null
    $uploadFolder = Join-Path $stagingRoot 'xroute'
    New-Item -ItemType Directory -Path $uploadFolder | Out-Null
    Copy-Item -LiteralPath $archivePath, $archiveChecksumPath, $manifestPath -Destination $uploadFolder
    Copy-Item -LiteralPath $installerPath -Destination (Join-Path $uploadFolder 'setup.rsc')
    Copy-Item -LiteralPath $routerOSScripts -Destination (Join-Path $stagingRoot "routeros") -Recurse
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs/INSTALL.md") -Destination $stagingRoot
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs/INSTALL-RU.md") -Destination $stagingRoot
    Copy-Item -LiteralPath (Join-Path $projectRoot "LICENSE") -Destination $stagingRoot
    Compress-Archive -Path (Join-Path $stagingRoot "*") -DestinationPath $bundlePath -CompressionLevel Optimal
    Remove-ReleasePath $stagingRoot $outputRoot

    Copy-Item -LiteralPath $archivePath, $archiveChecksumPath, $manifestPath, $installerPath, $sourceArchivePath -Destination $outputRoot
    Copy-Item -LiteralPath (Join-Path $projectRoot "docs/releases/$version.md") -Destination (Join-Path $outputRoot "RELEASE-NOTES.md")

    $bundleHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $bundlePath).Hash.ToLowerInvariant()
    $sourceCopy = Join-Path $outputRoot ([System.IO.Path]::GetFileName($sourceArchivePath))
    $sourceHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $sourceCopy).Hash.ToLowerInvariant()
    [System.IO.File]::WriteAllText("$bundlePath.sha256", "$bundleHash  $bundleName`n", [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::WriteAllText("$sourceCopy.sha256", "$sourceHash  $([System.IO.Path]::GetFileName($sourceCopy))`n", [System.Text.UTF8Encoding]::new($false))
    $installerCopy = Join-Path $outputRoot $installerName
    $installerHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $installerCopy).Hash.ToLowerInvariant()
    [System.IO.File]::WriteAllText("$installerCopy.sha256", "$installerHash  $installerName`n", [System.Text.UTF8Encoding]::new($false))

    $checksumTargets = @(
        $bundlePath,
        (Join-Path $outputRoot $archiveName),
        (Join-Path $outputRoot ([System.IO.Path]::GetFileName($manifestPath))),
        $sourceCopy,
        $installerCopy,
        (Join-Path $outputRoot "RELEASE-NOTES.md")
    )
    $checksumLines = foreach ($target in $checksumTargets) {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $target).Hash.ToLowerInvariant()
        "$hash  $([System.IO.Path]::GetFileName($target))"
    }
    [System.IO.File]::WriteAllLines((Join-Path $outputRoot "SHA256SUMS"), $checksumLines, [System.Text.UTF8Encoding]::new($false))

    Write-Output "GITHUB_RELEASE_ASSETS=$outputRoot"
    Write-Output "ROUTEROS_BUNDLE=$bundlePath"
    Write-Output "ROUTEROS_BUNDLE_SHA256=$bundleHash"
    Write-Output "PUBLIC_SOURCE_SHA256=$sourceHash"
    $packageCompleted = $true
} finally {
    Pop-Location
}

if ($packageCompleted -and -not $SkipWorkspaceCleanup) {
    & (Join-Path $PSScriptRoot "clean-local-build-artifacts.ps1") -PublicSourceDirectory $PublicSourceDirectory
}
