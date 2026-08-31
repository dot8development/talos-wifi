---
description: |
    NetworkWifiConfig is a config document to configure a WiFi (wireless) network interface.
    When at least one WiFi configuration document is present, Talos automatically loads
    the wireless kernel module stack and runs a `wpa_supplicant` instance for each
    configured interface, so no extra `KernelModuleConfig` document is required.
    Personal authentication is supported (WPA2-PSK, WPA3-SAE and mixed-mode access points);
    WPA-Enterprise (802.1X) is not supported.
    Use `talosctl get wifistatuses` to inspect the association status.
title: NetworkWifiConfig
---

<!-- markdownlint-disable -->









{{< highlight yaml >}}
apiVersion: v1alpha1
kind: NetworkWifiConfig
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
|`countryCode` |string |ISO/IEC 3166-1 alpha2 country code to set the wireless regulatory domain.<br><br>If not set, the regulatory domain is left to the kernel default (world domain),<br>which might restrict available channels and transmit power. <details><summary>Show example(s)</summary>{{< highlight yaml >}}
countryCode: NL
{{< /highlight >}}</details> | |
|`networks` |<a href="#NetworkWifiConfig.networks.">[]WifiNetworkConfig</a> |List of wireless networks to connect to (in order of preference).  | |




## networks[] {#NetworkWifiConfig.networks.}

WifiNetworkConfig describes a single WiFi network.




| Field | Type | Description | Value(s) |
|-------|------|-------------|----------|
|`ssid` |string |SSID (network name) of the wireless network. <details><summary>Show example(s)</summary>{{< highlight yaml >}}
ssid: HomeNetwork
{{< /highlight >}}</details> | |
|`psk` |string |Pre-shared key (passphrase) of the wireless network, 8 to 63 characters.<br><br>The same passphrase is used for both WPA2-PSK and WPA3-SAE, so<br>mixed-mode access points are supported transparently.<br><br>If not set, the network is assumed to be open (no authentication).  | |
|`hidden` |bool |Set if the network SSID is hidden (not broadcasted).  | |








