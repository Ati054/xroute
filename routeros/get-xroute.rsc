# XRoute release loader. Fetch this small file over verified HTTPS and import it.
# Only public GitHub API endpoints are used; no account or token is required.
{
:local version "1.7.1"
:local arch [/system/resource/get architecture-name]
:if (($arch = "x86") || ($arch = "x86_64")) do={ :set arch "amd64" }
:if (($arch != "arm64") && ($arch != "amd64")) do={ :error "XRoute: this architecture is not supported" }
:local name ("xroute-" . $version . "-install-" . $arch . ".rsc")
:local assetId 0
:local bytes 0
:onerror lookupError in={
  :local reply [/tool/fetch url=("https://api.github.com/repos/Ati054/xroute/releases/tags/v" . $version) check-certificate=yes http-header-field="Accept: application/vnd.github+json" output=user as-value duration=1m]
  :local release [:deserialize ($reply->"data") from=json]
  :if ((($release->"tag_name") != ("v" . $version)) || (($release->"draft") = true) || (($release->"prerelease") = true)) do={ :error "The stable release is not available" }
  :foreach asset in=($release->"assets") do={
    :if (($asset->"name") = $name) do={
      :if (($assetId != 0) || (($asset->"state") != "uploaded")) do={ :error "Release installer is ambiguous or incomplete" }
      :set assetId [:tonum ($asset->"id")]
      :set bytes [:tonum ($asset->"size")]
    }
  }
  :if (($assetId < 1) || ($bytes < 2048) || ($bytes > 524288)) do={ :error "The installer for this architecture is not published" }
} do={ :error ("XRoute: release lookup failed; no router configuration was changed. Use the manual bundle if GitHub is unavailable. " . $lookupError) }
:local temporary ("xroute-setup-" . [:rndstr length=12 from="0123456789abcdefghijklmnopqrstuvwxyz"] . ".rsc")
:if ([:len [/file/find where name=$temporary]] > 0) do={ :error "XRoute: temporary filename collision; run the command again" }
:onerror downloadError in={
  /tool/fetch url=("https://api.github.com/repos/Ati054/xroute/releases/assets/" . $assetId) check-certificate=yes http-header-field="Accept: application/octet-stream" dst-path=$temporary duration=2m
  :if ([/file/get [/file/find where name=$temporary] size] != $bytes) do={ :error "Installer download is incomplete" }
  /import file-name=$temporary
} do={
  :local file [/file/find where name=$temporary]
  :if ([:len $file] = 1) do={ /file/remove $file }
  :error ("XRoute: " . $downloadError)
}
:local file [/file/find where name=$temporary]
:if ([:len $file] = 1) do={ /file/remove $file }
}
