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

No network cable needed. Your WiFi name and password go **inside the USB stick**, together with
the rest of the machine's configuration. The computer boots from the stick, joins your WiFi,
installs Talos onto its internal drive by itself, and from then on runs from that drive.

> Tested end to end on 2026-11-26 in a virtual machine (boot from the stick → unattended install
> onto NVMe → reboot without the stick → Talos runs from disk with its configuration). WiFi itself
> was proven on the real EliteBook the same day.

### What you need

- the computer to install on — x86-64 with one of the supported WiFi cards above (everything on its
  internal NVMe drive is replaced by Talos)
- a USB stick (1 GB or larger)
- your own Mac or Linux machine with [Docker](https://www.docker.com/), `git` and the
  [GitHub CLI](https://cli.github.com/) (`gh`), logged in to GitHub
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

### 2. Pick the computer's IP address

The configuration needs to know the address the computer will have on your WiFi. In your router's
admin page, create a **DHCP reservation** for the computer's **WiFi MAC address** — e.g.
`192.168.1.50`. Below this is `NODE_IP`.

The WiFi MAC is printed on a sticker on many laptops, shown in the BIOS/UEFI setup, or listed in the
router's client list if the computer has been on this WiFi before (under its old operating system).

### 3. Write the configuration

Create `wifi.yaml` — put in your WiFi name and password. You don't need to know what Linux will call
the WiFi card: the file lists the usual names, and Talos uses whichever one exists.

```yaml
# Your WiFi network, once per possible interface name
apiVersion: v1alpha1
kind: NetworkWifiConfig
name: wlp0s20f3
networks:
  - ssid: "MyNetwork"        # <- your WiFi name
    psk: "my-wifi-password"  # <- your WiFi password
---
apiVersion: v1alpha1
kind: NetworkWifiConfig
name: wlan0
networks:
  - ssid: "MyNetwork"
    psk: "my-wifi-password"
---
apiVersion: v1alpha1
kind: NetworkWifiConfig
name: wlo1
networks:
  - ssid: "MyNetwork"
    psk: "my-wifi-password"
---
# Install Talos on the internal NVMe drive by itself
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

### 4. Make the USB stick (with your configuration inside)

```sh
mkdir -p _out
docker run --rm -t -v "$PWD/_out:/out" -v "$PWD/cluster:/cfg:ro" \
  ghcr.io/dot8development/imager:v1.14.7-20-g42b923c85 \
  iso --arch amd64 --embedded-config-path /cfg/controlplane.yaml
# -> _out/metal-amd64.iso   (takes a minute or two)
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

> The stick now contains your cluster keys and WiFi password. Wipe it (or write something else over
> it) once the installation is finished.

### 5. Boot from the stick — it installs itself

1. Plug in the USB stick.
2. In the BIOS/UEFI setup, **turn off Secure Boot** (this image is not signed), then boot
   from the USB stick (on HP laptops press `F9` at power-on for the boot menu).
3. Talos starts, joins your WiFi and installs itself onto the NVMe drive in the background. The
   screen shows its progress and IP address. Give it 5–10 minutes.

From your own machine, check when the installation is done:

```sh
export TALOSCONFIG="$PWD/cluster/talosconfig"
talosctl config endpoint NODE_IP
talosctl config node NODE_IP

talosctl get unattendedinstallstatus     # wait until PHASE says "installed"
talosctl get wifistatus                  # your SSID and "associated"
```

When it says `installed`: **pull out the USB stick**, then restart the computer so it boots from
its own drive:

```sh
talosctl reboot
```

(It does not restart on its own after installing — it keeps running from the stick until you
reboot it.)

### 6. Start Kubernetes

After the reboot (a minute or two) the computer is back on your WiFi at `NODE_IP`:

```sh
talosctl version            # Server: v1.14.7-... — now running from the drive
talosctl bootstrap          # run this ONCE, only on a brand-new cluster
talosctl health             # wait until everything is OK (a few minutes)

talosctl kubeconfig .       # writes ./kubeconfig
kubectl --kubeconfig ./kubeconfig get nodes
```

Done — the node shows `Ready` and you can deploy to it.

### Alternative: install with a temporary network cable

If you would rather not put the configuration on the stick (or the computer does have an Ethernet
port), build the plain image instead —
`docker run --rm -t -v "$PWD/_out:/out" ghcr.io/dot8development/imager:v1.14.7-20-g42b923c85 iso --arch amd64`
— boot it with an Ethernet cable, USB-Ethernet adapter or phone USB tethering plugged in, note the
wired IP shown on screen (`WIRED_IP`) and send it the configuration:

```sh
talosctl get links --insecure -n WIRED_IP     # optional: the real wl… name of the WiFi card
talosctl apply-config --insecure -n WIRED_IP --file cluster/controlplane.yaml
```

It installs onto the drive and reboots; remove the stick and the cable while it restarts. It comes
back on WiFi at `NODE_IP` — continue with step 6.

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
