# MikrotikVPN installer for RouterOS 7.21+ / ARMv7 or ARM64.
# Download this file with the one-line command from README. Do not import it manually.

:local repo "https://github.com/oproskyrin/MikrotikVPN/releases/latest/download"
:local architecture [/system resource get architecture-name]
:local assetArch ""
:if ($architecture = "arm") do={ :set assetArch "armv7" }
:if ($architecture = "arm64") do={ :set assetArch "arm64" }
:if ($assetArch = "") do={ :error ("Unsupported architecture: " . $architecture . ". Supported: arm, arm64.") }
:local lanBridge "bridge"
:if ([:len [/interface bridge find where name=$lanBridge]] = 0) do={
  :if ([:len [/interface bridge find where name="bridgeLocal"]] > 0) do={ :set lanBridge "bridgeLocal" }
}
:local webPort 8080
:local net "172.31.255"
:local apiUser "unblock-api"
:local apiPass [:rndstr length=32]
:local panelPass [:rndstr length=20]

:if ([:len [/interface bridge find where name=$lanBridge]] = 0) do={
  :error ("Bridge '" . $lanBridge . "' not found. Edit lanBridge in install.rsc before importing.")
}
:if ([:len [/container find where name="unblock-panel"]] > 0 || [:len [/container find where name="xray-vless"]] > 0) do={
  :error "MikrotikVPN or Xray containers already exist. Installer stopped without changing them."
}
:if ([:tonum [/system resource get free-hdd-space]] < 25000000) do={
  :error "At least 25 MB of free storage is required."
}

:put ("[MikrotikVPN] Downloading " . $assetArch . " images from GitHub Releases...")
/tool fetch url=($repo . ("/xray-vless-" . $assetArch . ".tar.gz")) dst-path=("xray-vless-" . $assetArch . ".tar.gz") mode=https check-certificate=yes
/tool fetch url=($repo . ("/unblock-panel-" . $assetArch . ".tar.gz")) dst-path=("unblock-panel-" . $assetArch . ".tar.gz") mode=https check-certificate=yes

/interface bridge add name=bridge-unblock comment="MikrotikVPN container network"
/interface veth add name=xray-veth address=($net . ".2/24") gateway=($net . ".1")
/interface veth add name=panel-veth address=($net . ".3/24") gateway=($net . ".1")
/interface bridge port add bridge=bridge-unblock interface=xray-veth
/interface bridge port add bridge=bridge-unblock interface=panel-veth
/ip address add address=($net . ".1/24") interface=bridge-unblock comment="MikrotikVPN gateway"

/file add name=xray-config type=directory
/file add name=panel-data type=directory
/file add name=xray-config/config.json contents="{\"log\":{\"loglevel\":\"warning\"},\"inbounds\":[{\"listen\":\"0.0.0.0\",\"port\":1080,\"protocol\":\"socks\",\"settings\":{\"udp\":true}}],\"outbounds\":[{\"tag\":\"direct\",\"protocol\":\"freedom\"}],\"routing\":{\"rules\":[]}}"

/container mounts add list=XRAY_MOUNT src=/xray-config dst=/etc/xray mode=ro
/container mounts add list=PANEL_RW src=/xray-config dst=/xray-config mode=rw
/container mounts add list=PANEL_DATA src=/panel-data dst=/data mode=rw

/user group add name=unblock-api policy=api,read,write comment="MikrotikVPN"
/user add name=$apiUser group=unblock-api password=$apiPass comment="MikrotikVPN"

/container envs add list=PANEL_ENV key=ROUTER_USER value=$apiUser
/container envs add list=PANEL_ENV key=ROUTER_PASS value=$apiPass
/container envs add list=PANEL_ENV key=ROUTER_API value=($net . ".1:8728")
/container envs add list=PANEL_ENV key=XRAY_HOST value=($net . ".2")
/container envs add list=PANEL_ENV key=XRAY_SOCKS value=($net . ".2:1080")
/container envs add list=PANEL_ENV key=LAN_ADDR value=[/ip address get [find where interface=$lanBridge] address]
/container envs add list=PANEL_ENV key=PANEL_PASSWORD value=$panelPass

/container add file=("xray-vless-" . $assetArch . ".tar.gz") name=xray-vless interface=xray-veth mountlists=XRAY_MOUNT start-on-boot=yes logging=yes
/container add file=("unblock-panel-" . $assetArch . ".tar.gz") name=unblock-panel interface=panel-veth mountlists=PANEL_RW,PANEL_DATA envlist=PANEL_ENV start-on-boot=yes logging=yes
/ip service set api disabled=no address=($net . ".0/24")
/ip firewall nat add chain=dstnat protocol=tcp in-interface=$lanBridge dst-port=$webPort action=dst-nat to-addresses=($net . ".3") to-ports=8080 comment="MikrotikVPN panel"

:delay 5s
/file remove ("xray-vless-" . $assetArch . ".tar.gz")
/file remove ("unblock-panel-" . $assetArch . ".tar.gz")
:put ""
:put "MikrotikVPN installed."
:put ("Open: http://" . [:pick [/ip address get [find where interface=$lanBridge] address] 0 [:find [/ip address get [find where interface=$lanBridge] address] "/"]] . ":" . $webPort)
:put ("Panel password: " . $panelPass)
:put "Paste your VLESS URL in the panel, select services, then press Apply."
