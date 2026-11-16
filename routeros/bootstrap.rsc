# XROUTE single-entry local installer.
# Upload this release bundle and one private variables.rsc into the same
# XRoute directory, then import only this file. Every required artifact is
# checked before the first mutating step.

:local variablesSuffix "/variables.rsc"
:local variablesPath ""
:foreach candidate in=[/file/find where name~"variables.rsc"] do={
  :local candidatePath [/file/get $candidate name]
  :if ([:len $candidatePath] >= [:len $variablesSuffix]) do={
    :local suffixStart ([:len $candidatePath] - [:len $variablesSuffix])
    :if ([:pick $candidatePath $suffixStart [:len $candidatePath]] = $variablesSuffix) do={
      :if ([:len $variablesPath] > 0) do={ :error "XROUTE: multiple variables.rsc files found; keep exactly one release bundle" }
      :set variablesPath $candidatePath
    }
  }
}
:if ([:len $variablesPath] = 0) do={ :error "XROUTE: place a private variables.rsc in the uploaded XRoute directory" }

:local bundleRoot [:pick $variablesPath 0 ([:len $variablesPath] - [:len $variablesSuffix])]
:local scriptRoot $bundleRoot
# XRoute namespace compatibility guard: begin
:local xrouteCurrentKnown [/container/find where comment~"^XROUTE (container|rollback container)( previous| candidate| rollback| failed)?\$"]
:local xrouteLegacyKnown [/container/find where comment~"^SB-GATEWAY (container|rollback container)( previous| candidate| rollback| failed)?\$"]
:if (([:len $xrouteCurrentKnown] > 0) && ([:len $xrouteLegacyKnown] > 0)) do={ :error "XRoute: managed namespaces are mixed; no changes made" }
:local xrouteKnown [/container/find where (comment="XROUTE container") or (comment="SB-GATEWAY container")]
:if ([:len $xrouteKnown] > 1) do={ :error "XRoute: managed container identity is ambiguous; no changes made" }
:if ([:len $xrouteLegacyKnown] > 0) do={ :set scriptRoot ($bundleRoot . "/legacy") }
# XRoute namespace compatibility guard: end

:foreach requiredFile in={"preflight.rsc";"webfig-bootstrap.rsc";"fasttrack-patch.rsc";"container-startup.rsc";"install.rsc";"watchdog.rsc"} do={
  :local requiredPath ($scriptRoot . "/" . $requiredFile)
  :if ([:len [/file/find where name=$requiredPath]] != 1) do={ :error ("XROUTE: release bundle is incomplete: " . $requiredPath) }
}

:log info ("XROUTE: bootstrap loading private variables from " . $variablesPath)
/import file-name=$variablesPath
:global "SB_STORAGE_ROOT"
:if ($"SB_STORAGE_ROOT" != $bundleRoot) do={ :error "XROUTE: variables.rsc SB_STORAGE_ROOT must match its release-bundle directory" }

:log info "XROUTE: bootstrap 1/5 preflight"
/import file-name=($scriptRoot . "/preflight.rsc")
:log info "XROUTE: bootstrap 2/5 dedicated RouterOS REST account"
/import file-name=($scriptRoot . "/webfig-bootstrap.rsc")
:log info "XROUTE: bootstrap 3/5 FastTrack compatibility"
/import file-name=($scriptRoot . "/fasttrack-patch.rsc")
:log info "XROUTE: bootstrap 4/5 container and owned network objects"
/import file-name=($scriptRoot . "/container-startup.rsc")
/import file-name=($scriptRoot . "/install.rsc")
:log info "XROUTE: bootstrap 5/5 fail-open watchdog"
/import file-name=($scriptRoot . "/watchdog.rsc")
:if ($"SB_IMAGE_SOURCE" = "file") do={
  :local imageFile [/file/find where name=$"SB_IMAGE_FILE"]
  :if ([:len $imageFile] = 1) do={ :do { /file/remove $imageFile } on-error={ :log warning "XROUTE: extracted image archive cleanup is pending" } }
}
:foreach transientFile in={"bootstrap.rsc";"fasttrack-patch.rsc";"container-startup.rsc";"install.rsc";"preflight.rsc";"variables.rsc";"watchdog.rsc";"webfig-bootstrap.rsc"} do={
  :local transientPath ($bundleRoot . "/" . $transientFile)
  :local transientId [/file/find where name=$transientPath]
  :if ([:len $transientId] = 1) do={ :do { /file/remove $transientId } on-error={ :log warning ("XROUTE: one-shot installer cleanup is pending: " . $transientPath) } }
}
:log info "XROUTE: bootstrap complete; open the container panel and finish the first-launch wizard"
