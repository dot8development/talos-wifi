<!-- markdownlint-disable MD041 -->

# Talos Linux with WiFi

This is a fork of [Talos Linux](https://github.com/siderolabs/talos) **v1.14.7** that adds
**built-in WiFi**. Upstream Talos has no wireless support at all, so a machine with only a
WiFi card (like a laptop) cannot run stock Talos.

What the fork adds on top of v1.14.7:

- a kernel with WiFi enabled (`cfg80211`, `mac80211`, Intel `iwlwifi`/`iwlmvm`) — built from
  [dot8development/pkgs](https://github.com/dot8development/pkgs) `main`
- `wpa_supplicant`, the wireless regulatory database and Intel WiFi firmware in the image
- a `NetworkWifiConfig` machine-config document: put your SSID and password in it and
  Talos connects on boot
- `talosctl get wifistatus` to see the connection (SSID, signal)

**Supported WiFi cards:** Intel Wi-Fi 6 **AX201** and Intel **9560 / 9461 / 9462** on an
x86-64 (amd64) computer. Other cards need their firmware added to the image.

**Prebuilt installer image (public):** `ghcr.io/dot8development/installer:v1.14.7-20-g42b923c85`

---

## Install on a computer — step by step

You install from a USB stick. The very first boot needs a **temporary wired connection**
(an Ethernet cable, a USB-Ethernet adapter, or phone USB tethering), because that is how you
hand the machine its configuration — including your WiFi password. After it installs and
reboots it connects to WiFi by itself and you can unplug the cable.

### What you need

- the computer to install on (everything on its internal NVMe drive will be replaced by Talos)
- a USB stick (1 GB or larger)
- a temporary wired network connection for that computer
- your own Mac or Linux machine with [Docker](https://www.docker.com/) and `git`
- your router's admin page (to give the computer a fixed IP address)

### 1. Get the tools

```sh
git clone https://github.com/dot8development/talos-wifi.git
cd talos-wifi

# macOS only: the build needs GNU make and sed
brew install make gnu-sed
export PATH="$(brew --prefix make)/libexec/gnubin:$(brew --prefix gnu-sed)/libexec/gnubin:$PATH"

# log in to GitHub's registry (the image builder is private to dot8development)
gh auth token | docker login ghcr.io -u <your-github-user> --password-stdin

# build talosctl, the command-line tool you manage Talos with
make talosctl-darwin-amd64          # on Linux: make talosctl-linux-amd64
sudo cp _out/talosctl-darwin-amd64 /usr/local/bin/talosctl
talosctl version --client           # should print v1.14.7-...
```

Use this `talosctl`, not the upstream one: only the fork's version understands `NetworkWifiConfig`.

### 2. Make the USB stick

Build the boot image (one command, takes a minute or two):

```sh
mkdir -p _out
docker run --rm -t -v "$PWD/_out:/out" \
  ghcr.io/dot8development/imager:v1.14.7-20-g42b923c85 iso --arch amd64
# -> _out/metal-amd64.iso
```

Write it to the USB stick. **Double-check the disk number — this erases that disk.**

macOS:

```sh
diskutil list                              # find the stick, e.g. /dev/disk4
diskutil unmountDisk /dev/disk4
sudo dd if=_out/metal-amd64.iso of=/dev/rdisk4 bs=4m
diskutil eject /dev/disk4
```

Linux:

```sh
lsblk                                      # find the stick, e.g. /dev/sdb
sudo dd if=_out/metal-amd64.iso of=/dev/sdb bs=4M oflag=sync status=progress
```

### 3. Boot the computer from the stick

1. Plug in the USB stick **and** the wired connection.
2. In the BIOS/UEFI setup, **turn off Secure Boot** (this image is not signed), then boot
   from the USB stick (on HP laptops press `F9` at power-on for the boot menu).
3. Talos starts in *maintenance mode*. The screen shows the computer's IP address on the
   wired network — write it down (below: `WIRED_IP`).

### 4. Find the WiFi card and reserve an IP for it

From your own machine:

```sh
talosctl get links --insecure -n WIRED_IP
```

Look for the link whose name starts with `wl` (for example `wlp0s20f3` or `wlan0`) and note
its `HW ADDR` (MAC address).

In your router's admin page, create a **DHCP reservation** so that MAC address always gets the
same IP, e.g. `192.168.1.50`. Below this is `NODE_IP`.

### 5. Write the configuration

Create `wifi.yaml` — put in your WiFi name and password, and the `wl…` link name from step 4.
Listing a second common name (`wlan0`) is harmless; Talos uses whichever exists.

```yaml
apiVersion: v1alpha1
kind: NetworkWifiConfig
name: wlp0s20f3            # <- your wl… link name from step 4
networks:
  - ssid: "MyNetwork"      # <- your WiFi name
    psk: "my-wifi-password" # <- your WiFi password
---
apiVersion: v1alpha1
kind: NetworkWifiConfig
name: wlan0
networks:
  - ssid: "MyNetwork"
    psk: "my-wifi-password"
---
# Install Talos on the internal NVMe drive
apiVersion: v1alpha1
kind: UnattendedInstallConfig
provisioning:
  diskSelector:
    match: disk.transport == "nvme"
  wipe: false
```

A single computer is both the Kubernetes control plane and the place your apps run, so remove
the "control plane only" taint with these two small files:

`single-node-1.yaml`

```yaml
apiVersion: v1alpha1
kind: KubeNodeConfig
$patch: delete
```

`single-node-2.yaml`

```yaml
apiVersion: v1alpha1
kind: KubeNodeConfig
labels:
  node-role.kubernetes.io/control-plane: ""
```

Generate the machine configuration (replace `NODE_IP`; `mycluster` is any name you like):

```sh
talosctl gen config mycluster https://NODE_IP:6443 \
  --install-image ghcr.io/dot8development/installer:v1.14.7-20-g42b923c85 \
  --config-patch @wifi.yaml \
  --config-patch-control-plane @single-node-1.yaml \
  --config-patch-control-plane @single-node-2.yaml \
  --output-dir cluster

talosctl validate --config cluster/controlplane.yaml --mode metal   # must say "valid"
```

> **Keep `--install-image` exactly as above.** Without it Talos installs the upstream image,
> which has no WiFi — the machine would be unreachable after its first reboot.
>
> The `cluster/` folder now holds your cluster's keys and your WiFi password: keep it private
> and backed up, never commit it.

### 6. Install

```sh
talosctl apply-config --insecure -n WIRED_IP --file cluster/controlplane.yaml
```

Talos installs itself on the NVMe drive and reboots (a few minutes). When the screen shows it
rebooting, **remove the USB stick and unplug the wired connection.** It comes back up on WiFi
at `NODE_IP`.

### 7. Start Kubernetes

```sh
export TALOSCONFIG="$PWD/cluster/talosconfig"
talosctl config endpoint NODE_IP
talosctl config node NODE_IP

talosctl get wifistatus     # should show your SSID and "associated"
talosctl bootstrap          # run this ONCE, only on a brand-new cluster
talosctl health             # wait until everything is OK (a few minutes)

talosctl kubeconfig .       # writes ./kubeconfig
kubectl --kubeconfig ./kubeconfig get nodes
```

Done — the node shows `Ready` and you can deploy to it.

---

## Updating an installed machine

When a new installer image is published (`ghcr.io/dot8development/installer:<tag>`):

```sh
talosctl upgrade --image ghcr.io/dot8development/installer:<tag> --drain=false
```

`--drain=false` is right for a single machine (there is nowhere to move the pods). If the
command finishes installing but the machine does not restart, run
`talosctl reboot --mode powercycle --wait`. The previous version stays on disk: if anything goes
wrong, choose it in the boot menu at power-on, or run `talosctl rollback`.

Always use a **dot8development** installer image for upgrades — an upstream image has no WiFi.

## Troubleshooting WiFi

```sh
talosctl get wifistatus                     # connected? which SSID, signal strength
talosctl services | grep wpa                # the wpa-supplicant-<link> service should be Running
talosctl logs wpa-supplicant-wlp0s20f3      # supplicant log (use your link name)
talosctl dmesg | grep -i iwlwifi            # did the card and its firmware load?
```

To change the WiFi network or password later, edit the `NetworkWifiConfig` and apply it with
`talosctl apply-config --file cluster/controlplane.yaml` (while the machine is still reachable).

## Building the images yourself (maintainers)

The WiFi kernel, `wpa-supplicant` and `wireless-regdb` packages come from
[dot8development/pkgs](https://github.com/dot8development/pkgs) `main` (pinned by
`WIFI_PKGS` in the `Makefile`). To build and publish this repo's images:

```sh
export GITHUB_TOKEN=$(gh auth token)      # the imager pulls the private installer-base
make installer-base imager installer USERNAME=dot8development PLATFORM=linux/amd64 \
  INSTALLER_ARCH=targetarch PUSH=true
```

`INSTALLER_ARCH=targetarch` keeps the build amd64-only (the WiFi kernel is only built for amd64).

---

# Upstream Talos README

<p align="center">
  <h1 align="center">Talos Linux</h1>
  <p align="center">A modern OS for Kubernetes.</p>
  <p align="center">
    <a href="https://github.com/talos-systems/talos/releases/latest"><img alt="Release" src="https://img.shields.io/github/release/talos-systems/talos.svg?logo=github&logoColor=white"></a>
    <a href="https://github.com/talos-systems/talos/releases/latest"><img alt="Pre-release" src="https://img.shields.io/github/release-pre/talos-systems/talos.svg?label=pre-release&logo=GitHub&logoColor=white"></a>
    <a href="https://www.bestpractices.dev/projects/7340"><img src="https://www.bestpractices.dev/projects/7340/badge" alt="OpenSSF badge"></a>
  </p>
</p>

---

**Talos** is a modern OS for running Kubernetes: secure, immutable, and minimal.
Talos is fully open source, production-ready, and supported by the people at [Sidero Labs](https://www.SideroLabs.com/).
All system management is done via an API - there is no shell or interactive console.
Benefits include:

- **Security**: Talos reduces your attack surface: It's minimal, hardened, and immutable.
  All API access is secured with mutual TLS (mTLS) authentication.
- **Predictability**: Talos eliminates configuration drift, reduces unknown factors by employing immutable infrastructure ideology, and delivers atomic updates.
- **Evolvability**: Talos simplifies your architecture, increases your agility, and always delivers current stable Kubernetes and Linux versions.

## Documentation

For instructions on deploying and managing Talos, see the [Documentation](https://docs.siderolabs.com/talos).

## Community

- Support: Questions, bugs, feature requests [GitHub Discussions](https://github.com/siderolabs/talos/discussions)
- Slack: Join our [slack channel](https://taloscommunity.slack.com/). Request access via [inviter.co](https://inviter.co/sidero-labs-community).
- Twitter: [@SideroLabs](https://twitter.com/SideroLabs)

If you're interested in this project and would like to help in engineering efforts or have general usage questions, we are happy to have you!
We hold a monthly meeting that all audiences are welcome to attend.

We would appreciate your feedback so that we can make Talos even better!
To do so, you can take our [survey](https://docs.google.com/forms/d/1TUna5YTYGCKot68Y9YN_CLobY6z9JzLVCq1G7DoyNjA/edit).

### Office Hours

- When: Second Monday of every month at 16:30 UTC.
- Where: [Google Meet](https://meet.google.com/ivb-kjfm-jfc).

> Note: You can convert the meeting hours to your [local time](https://everytimezone.com/s/599e61d6).

## Contributing

Contributions are welcomed and appreciated!
See [Contributing](CONTRIBUTING.md) for our guidelines.

## License

<a href="https://github.com/siderolabs/talos/blob/master/LICENSE">
  <img alt="GitHub" src="https://img.shields.io/github/license/siderolabs/talos">
</a>

Some software we distribute is under the General Public License family of licenses or other licenses that require we provide you with the source code.
If you would like a copy of the source code for this software, please contact us via email: info at SideroLabs.com.
