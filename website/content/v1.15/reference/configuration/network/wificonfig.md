---
description: |
    WifiConfig is a config document to configure a Wi-Fi (wireless) network interface.
    When at least one Wi-Fi configuration document is present, Talos loads the generic
    wireless kernel modules (`cfg80211`, `mac80211`) and runs a `wpa_supplicant` instance
    for each configured interface. Vendor driver modules which the kernel can't autoload
    (e.g. Intel's `iwlmvm` op-mode module) should be loaded with a `KernelModuleConfig`
    document, or are handled by the system extension shipping the driver.
    Personal authentication is supported (WPA2-PSK, WPA3-SAE and mixed-mode access points);
    WPA-Enterprise (802.1X) is not supported.
    Use `talosctl get wifistatuses` to inspect the association status.
title: WifiConfig
---

<!-- markdownlint-disable -->









{{< highlight yaml >}}
apiVersion: v1alpha1
kind: WifiConfig
name: wlan0 # Name of the wireless link (interface).
countryCode: NL # ISO/IEC 3166-1 alpha2 country code to set the wireless regulatory domain.
# List of wireless networks to connect to (in order of preference).
networks:
    - ssid: HomeNetwork # SSID (network name) of the wireless network.
      psk: topsecretpassphrase # Pre-shared key (passphrase) of the wireless network, 8 to 63 characters.
{{< /highlight >}}


| Field | Type | Description | Value(s) |
|-------|------|-------------|----------|
|`name` |string |Name of the wireless link (interface). <details><summary>Show example(s)</summary>{{< highlight yaml >}}
name: wlan0
{{< /highlight >}}</details> | |
|`countryCode` |string |ISO/IEC 3166-1 alpha2 country code to set the wireless regulatory domain.<br><br>The regulatory domain is global to the node, so all Wi-Fi documents must use<br>the same country code.<br><br>If not set, the regulatory domain is left to the kernel default (world domain),<br>which might restrict available channels and transmit power. <details><summary>Show example(s)</summary>{{< highlight yaml >}}
countryCode: NL
{{< /highlight >}}</details> | |
|`networks` |<a href="#WifiConfig.networks.">[]WifiNetworkConfig</a> |List of wireless networks to connect to (in order of preference).  | |




## networks[] {#WifiConfig.networks.}

WifiNetworkConfig describes a single Wi-Fi network.




| Field | Type | Description | Value(s) |
|-------|------|-------------|----------|
|`ssid` |string |SSID (network name) of the wireless network. <details><summary>Show example(s)</summary>{{< highlight yaml >}}
ssid: HomeNetwork
{{< /highlight >}}</details> | |
|`psk` |string |Pre-shared key (passphrase) of the wireless network, 8 to 63 characters.<br><br>The same passphrase is used for both WPA2-PSK and WPA3-SAE, so<br>mixed-mode access points are supported transparently.<br>Raw 64-character hexadecimal PSKs are not supported.<br><br>If not set, the network is assumed to be open (no authentication).  | |
|`hidden` |bool |Set if the network SSID is hidden (not broadcasted).  | |








