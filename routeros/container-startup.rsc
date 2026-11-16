:local xrouteFileNamespace "XROUTE"
:local xrouteDelegated false
# XRoute namespace compatibility guard: begin
:local xrouteCurrentKnown [/container/find where comment~"^XROUTE (container|rollback container)( previous| candidate| rollback| failed)?\$"]
:local xrouteLegacyKnown [/container/find where comment~"^SB-GATEWAY (container|rollback container)( previous| candidate| rollback| failed)?\$"]
:if (([:len $xrouteCurrentKnown] > 0) && ([:len $xrouteLegacyKnown] > 0)) do={ :error "XRoute: managed namespaces are mixed; no changes made" }
:local xrouteKnown [/container/find where (comment="XROUTE container") or (comment="SB-GATEWAY container")]
:if ([:len $xrouteKnown] > 1) do={ :error "XRoute: managed container identity is ambiguous; no changes made" }
:if (([:len $xrouteKnown] = 0) && ([:len $xrouteLegacyKnown] > 0)) do={ :set xrouteKnown [:pick $xrouteLegacyKnown 0 1] }
:if (([:len $xrouteKnown] = 0) && ([:len $xrouteCurrentKnown] > 0)) do={ :set xrouteKnown [:pick $xrouteCurrentKnown 0 1] }
:if ([:len $xrouteKnown] = 1) do={
  :if (($xrouteFileNamespace = "SB-GATEWAY") && ([:len $xrouteCurrentKnown] > 0)) do={ :error "XRoute: legacy installer cannot manage a new installation" }
  :if (($xrouteFileNamespace = "XROUTE") && ([:len $xrouteLegacyKnown] > 0)) do={
    :local xrouteOwnedRoot [:tostr [/container/get $xrouteKnown root-dir]]
    :if ([:pick $xrouteOwnedRoot 0 1] = "/") do={ :set xrouteOwnedRoot [:pick $xrouteOwnedRoot 1 [:len $xrouteOwnedRoot]] }
    :local xrouteLastSlash -1
    :local xrouteScan [:find $xrouteOwnedRoot "/"]
    :while ([:typeof $xrouteScan] = "num") do={ :set xrouteLastSlash $xrouteScan; :set xrouteScan [:find $xrouteOwnedRoot "/" ($xrouteScan + 1)] }
    :if ($xrouteLastSlash < 1) do={ :error "XRoute: existing storage identity is invalid" }
    :local xrouteRootName [:pick $xrouteOwnedRoot ($xrouteLastSlash + 1) [:len $xrouteOwnedRoot]]
    :if (($xrouteRootName != "root") && ([:pick $xrouteRootName 0 5] != "root-")) do={ :error "XRoute: existing container root is not recognized" }
    :local xrouteCompatibilityFile ([:pick $xrouteOwnedRoot 0 $xrouteLastSlash] . "/legacy/container-startup.rsc")
    :if ([:len [/file/find where name=$xrouteCompatibilityFile]] != 1) do={ :error "XRoute: upload the matching legacy installer directory beside the existing installation" }
    /import file-name=$xrouteCompatibilityFile
    :set xrouteDelegated true
  }
}
# XRoute namespace compatibility guard: end

:if ($xrouteDelegated != true) do={

# XROUTE storage-aware boot. Import before install.rsc.
# This installer never starts or changes a container.
:local initiallyDisabled ($1 = true)
:local readyId [/system/script/find where name="XROUTE-storage-ready"]
:local bootId [/system/script/find where name="XROUTE-container-startup"]
:local scheduleId [/system/scheduler/find where name="XROUTE-container-startup"]
:if (([:len $readyId] > 1) || ([:len $bootId] > 1) || ([:len $scheduleId] > 1)) do={ :error "XROUTE startup objects are ambiguous" }
:if ([:len $readyId] = 1) do={ :if ([/system/script/get $readyId comment] != "XROUTE storage readiness") do={ :error "XROUTE storage probe is not owned" } }
:if ([:len $bootId] = 1) do={ :if ([/system/script/get $bootId comment] != "XROUTE storage-aware startup") do={ :error "XROUTE startup script is not owned" } }
:if ([:len $scheduleId] = 1) do={ :if ([/system/scheduler/get $scheduleId comment] != "XROUTE storage-aware startup scheduler") do={ :error "XROUTE startup scheduler is not owned" } }

:if ([:len $readyId] = 0) do={
  /system/script/add name="XROUTE-storage-ready" policy=read source="" comment="XROUTE storage readiness"
  :set readyId [/system/script/find where name="XROUTE-storage-ready"]
}
/system/script/set $readyId source={
  :local target $1
  :local disksOnly ($2 = true)
  :local hostArchitecture [/system/resource/get architecture-name]
  :local systemDiskAllowed (([:pick [/system/resource/get board-name] 0 3] = "CHR") && (($hostArchitecture = "x86") || ($hostArchitecture = "x86_64")))
  :local pathReady do={
    :local path [:tostr $1]
    :if ([:pick $path 0 1] = "/") do={ :set path [:pick $path 1 [:len $path]] }
    :local separator [:find $path "/"]
    :if (([:typeof $separator] != "num") || ($separator = 0)) do={ :return false }
    :local diskName [:pick $path 0 $separator]
    :local kind ""
    :do {
      :if ([/file/get $diskName type] != "disk") do={
        :if (($2 != true) || ([/file/get $diskName type] != "directory")) do={ :return false }
      }
      :set kind [/file/get $path type]
    } on-error={ :return false }
    :return (($kind = "directory") || ($kind = "container store"))
  }
  :local root [/container/get $target root-dir]
  :if ([$pathReady $root $systemDiskAllowed] != true) do={ :return false }
  :if ([:pick $root 0 1] = "/") do={ :set root [:pick $root 1 [:len $root]] }
  :local rootDisk [:pick $root 0 [:find $root "/"]]
  :local checkedDisks [:toarray ""]
  :set ($checkedDisks->$rootDisk) true
  :local lists [/container/get $target mountlists]
  :if ([:len $lists] = 0) do={ :return true }
  :if ([:typeof $lists] != "array") do={ :return false }
  :foreach mountListName in=$lists do={
    :local mounts [/container/mounts/find where list=$mountListName]
    :if ([:len $mounts] = 0) do={ :return false }
    :foreach mountId in=$mounts do={
      :if ([/container/mounts/get $mountId disabled] = false) do={
        :local source [/container/mounts/get $mountId src]
        :if ($disksOnly = true) do={
          :if ([:pick $source 0 1] = "/") do={ :set source [:pick $source 1 [:len $source]] }
          :local separator [:find $source "/"]
          :if (([:typeof $separator] != "num") || ($separator = 0)) do={ :return false }
          :local diskName [:pick $source 0 $separator]
          :if (($checkedDisks->$diskName) != true) do={
            :do {
              :if ([/file/get $diskName type] != "disk") do={
                :if (($systemDiskAllowed != true) || ([/file/get $diskName type] != "directory")) do={ :return false }
              }
            } on-error={ :return false }
            :set ($checkedDisks->$diskName) true
          }
        } else={ :if ([$pathReady $source $systemDiskAllowed] != true) do={ :return false } }
      }
    }
  }
  :return true
}
:if ([:len $bootId] = 0) do={
  /system/script/add name="XROUTE-container-startup" policy=read,write,test source="" comment="XROUTE storage-aware startup"
  :set bootId [/system/script/find where name="XROUTE-container-startup"]
}
/system/script/set $bootId source={
  :local finished ([:len [/system/script/job/find where script="XROUTE-container-startup"]] > 1)
  :local probe [/system/script/find where name="XROUTE-storage-ready" and comment="XROUTE storage readiness"]
  :if ([:len $probe] != 1) do={ :error "XROUTE storage probe is missing or ambiguous" }
  :local ready [:parse [/system/script/get $probe source]]
  :local stable 0
  :local attempt 0
  :while (($finished = false) && ($attempt < 150)) do={
    :local busy (([:len [/system/scheduler/find where name="XROUTE-image-update"]] > 0) || ([:len [/system/script/find where name="XROUTE-image-update-transfer"]] > 0))
    :if ($busy = true) do={ :set finished true }
    :local targets [/container/find where comment="XROUTE container"]
    :if ([:len $targets] > 1) do={ :error "XROUTE startup container is ambiguous" }
    :if (([:len $targets] = 1) && ($busy = false)) do={
      :local target [:pick $targets 0]
      :if (([/container/get $target running] = true) || ([/container/get $target healthy] = true)) do={ :set finished true } else={
        :if ([/container/get $target stopped] = true) do={
          :local available false
          :do { :set available [$ready $target true] } on-error={}
          :if ($available = true) do={ :set stable ($stable + 1) } else={ :set stable 0 }
          :if ($stable >= 2) do={
            :local foldersReady false
            :do { :set foldersReady [$ready $target] } on-error={}
            :if (([:len [/system/scheduler/find where name="XROUTE-image-update"]] > 0) || ([:len [/system/script/find where name="XROUTE-image-update-transfer"]] > 0) || ([/container/get $target stopped] != true) || ([/container/get $target comment] != "XROUTE container")) do={ :set finished true; :set foldersReady false }
            :if ($foldersReady = true) do={
              /container/start $target
              :log info "XROUTE: storage ready; boot container start requested"
            } else={ :if ($finished = false) do={ :log error "XROUTE: mount source folders are missing; boot start refused" } }
            :set finished true
          }
        } else={ :set stable 0 }
      }
    } else={ :set stable 0 }
    :set attempt ($attempt + 1)
    :if ($finished = false) do={ :delay 2s }
  }
  :if ($finished = false) do={ :log error "XROUTE: boot storage wait expired; container not started" }
}
:if ([:len $scheduleId] = 0) do={
  /system/scheduler/add name="XROUTE-container-startup" interval=0s start-time=startup on-event="/system/script/run XROUTE-container-startup" policy=read,write,test disabled=$initiallyDisabled comment="XROUTE storage-aware startup scheduler"
} else={
  /system/scheduler/set $scheduleId interval=0s start-time=startup on-event="/system/script/run XROUTE-container-startup" policy=read,write,test
  :if ($initiallyDisabled = true) do={ /system/scheduler/disable $scheduleId }
}
}
