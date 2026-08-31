# WiFi Support: Upstream Compliance Analysis & Work Plan

Date: 2026-08-31 • Branch: `wifi` (talos @ `20f699b70`, pkgs fork @ `2dd498d`)
Scope: what it takes to make the current WiFi implementation acceptable as an upstream `siderolabs/talos` PR.

Upstream context: there is an open upstream feature request asking for exactly this, framed as an
extension: [siderolabs/talos#11185 — "Add 802.11 Wireless Kernel Support as a Talos Extension"](https://github.com/siderolabs/talos/issues/11185),
plus long-running discussions [#8259](https://github.com/siderolabs/talos/discussions/8259) and
[#6911](https://github.com/siderolabs/talos/discussions/6911). Upstream today ships **no** wireless
stack at all (kernel config has `# CONFIG_WLAN is not set`), so the PR spans **three repos**:
`siderolabs/pkgs` (kernel config, wpa-supplicant pkg), `siderolabs/extensions` (firmware + modules +
supplicant binary), and `siderolabs/talos` (config doc, resources, controllers, SELinux, docs).

---

## 0. Current implementation inventory

Committed in `20f699b70` (talos) and `2dd498d` (pkgs):

| Piece | File |
|---|---|
| Config document `NetworkWifiConfig` | `pkg/machinery/config/types/network/wifi.go` (+ `wifi_test.go`, `testdata/networkwificonfig.yaml`, `network_doc.go`, `deep_copy.generated.go`) |
| Config accessor interfaces | `pkg/machinery/config/config/network.go` (`NetworkWifiConfig`, `WifiNetwork`), `config/config.go`, `config/container/container.go` |
| Resource `WifiSpec` (`WifiSpecs.net.talos.dev`, `meta.Sensitive`) | `pkg/machinery/resources/network/wifi_spec.go` |
| Controller: config → spec | `internal/app/machined/pkg/controllers/network/wifi_config.go` |
| Controller: spec + LinkStatus → per-link services | `internal/app/machined/pkg/controllers/network/wifi_service.go` (+ `wifi_service_internal_test.go`) |
| machined service (process runner) | `internal/app/machined/pkg/system/services/wpa_supplicant.go` — execs `/usr/bin/wpa_supplicant -i <link> -D nl80211 -c /run/wpa_supplicant/<link>.conf` |
| Registration | `internal/app/machined/pkg/runtime/v1alpha2/v1alpha2_controller.go:462-465`, `v1alpha2_state.go:229` |
| Constants | `pkg/machinery/constants/constants.go` (`CgroupWpaSupplicant`, `WifiSupplicantRunDir`) |
| Build wiring | `Makefile` (`PKG_WPA_SUPPLICANT`), `Dockerfile:186-187,798-800` (amd64 rootfs only: wpa_supplicant + `regulatory.db(.p7s)` + `iwlwifi-Qu*.ucode`), `hack/modules-amd64.txt` (cfg80211, mac80211, iwlwifi, iwlmvm) |
| pkgs fork | `kernel/build/config-amd64` (wireless enabled, amd64 only), `wpa-supplicant/pkg.yaml` (static wpa_supplicant/wpa_cli/wpa_passphrase → `/usr/bin`), `Makefile`, `Pkgfile` (v2.11) |

The design of the config/resource/controller layer already matches upstream patterns closely
(mirrors `EthernetConfig`/`EthernetSpec`). The main deviations are **packaging** (everything baked
into the base image), **SELinux** (service runs unconfined), **codegen parity** (proto/schema/docs
not regenerated), missing **status observability**, and **amd64-Intel-only** hardware scope.

---

## 1. System-extension packaging

### Current state

Everything is baked into the BASE image: static `wpa_supplicant` at `/usr/bin`, iwlwifi firmware +
`regulatory.db` copied into `/rootfs/usr/lib/firmware`, four wireless modules added to
`hack/modules-amd64.txt` squashfs list. This contradicts the upstream ethos (minimal base image; the
upstream ask in #11185 is explicitly "as a Talos extension") and adds ~10 MB firmware to every
image for a niche use case.

### How extensions actually work (verified in this tree)

* Manifest: `pkg/machinery/extensions/metadata.go` — `Manifest{Version: "v1alpha1", Metadata{Name,
  Version, Author, Description, Compatibility.Talos.Version, ExtraInfo}}`. Layout enforced by
  `pkg/machinery/extensions/load.go:24-50`: an extension directory may contain **only**
  `manifest.yaml` + `rootfs/`.
* Content validation: `pkg/machinery/extensions/validate.go:129-173` restricts non-directory files
  to `AllowedPaths` (`pkg/machinery/extensions/extensions.go:10-35`). Relevant entries:
  `/usr/lib/firmware`, `/usr/lib/modules`, `/usr/lib/udev/rules.d`, `/usr/local` — plus one-off
  carve-outs like `/usr/bin/nvidia-*`, `/usr/bin/nvme`. **`/usr/bin/wpa_supplicant` is NOT allowed**;
  `/usr/local/...` is the only general-purpose binary location.
* Firmware: `internal/pkg/extensions/compress.go` — `<ext-rootfs>/usr/lib/firmware` is *moved out of
  the squashfs into the initramfs* and bind-mounted read-only onto `/usr/lib/firmware` at boot
  (`internal/app/init/main.go` `bindMountFirmware()`). Firmware extensions therefore Just Work.
* Kernel modules: `internal/pkg/extensions/kernel_modules.go:64-170` — modules from extensions are
  merged into the base kernel-version dir, `depmod` is re-run (`hack/extra-modules.conf`:
  `search extras built-in`), and a synthetic `modules.dep` extension is layered on top. Extensions
  shipping *in-tree* modules built from the same pkgs kernel are established upstream practice
  (e.g. `usb-modem-drivers`, `v4l-uvc-drivers`, `gasket-driver` in siderolabs/extensions).
* Extension layers are composed into a single read-only overlay over `/` at boot
  (`internal/app/init/main.go` `mountRootFS()`), **in machined's own mount namespace** — so any
  native machined service can exec an extension binary by absolute path. Precedents:
  `/usr/local/bin/kubelet` (`services/kubelet.go:148`), `/usr/local/bin/etcd` (`services/etcd.go:202`).
* Extension *services* (`pkg/machinery/extensions/services/services.go`) are static one-per-yaml
  specs loaded once from `/usr/local/etc/containers/*.yaml`
  (`internal/app/machined/pkg/controllers/runtime/extension_service.go`); host-runner mode
  (`runnerMode: host`, fork commit `86d76b0df`) forbids config files and mounts. **They cannot model
  per-interface dynamic supplicants with generated configs** — a static extension service is the
  wrong tool for this job.

### Recommended split (cleanest, matches upstream ethos)

**Stays in talos core** (this repo — no binary/firmware payload):
* `NetworkWifiConfig` document, `WifiSpec`/`WifiStatus` resources, `WifiConfigController`,
  `WifiServiceController`, `WifiStatusController`, `WpaSupplicant` service definition, SELinux
  policy, docs. This mirrors how Talos core contains the kubelet service definition while the
  binary arrives separately.

**Moves to `siderolabs/extensions`** (new extension, e.g. `network/wifi` or split
`firmware/iwlwifi-firmware` + `drivers/wifi` + `network/wpa-supplicant`):
* `rootfs/usr/local/bin/wpa_supplicant` (static, from the pkgs `wpa-supplicant` package —
  passes `AllowedPaths`),
* `rootfs/usr/lib/firmware/iwlwifi-*.ucode` (+ other vendors), `regulatory.db`, `regulatory.db.p7s`,
* `rootfs/usr/lib/modules/<kver>/…/cfg80211.ko, mac80211.ko, iwlwifi.ko, iwlmvm.ko, …` (built from
  the pkgs kernel; the depmod-merge machinery handles the rest),
* `manifest.yaml` with `compatibility.talos.version: ">= v1.15.0"`.
* **No extension service spec** — the supplicant lifecycle stays with `WifiServiceController`.

**Talos-side changes to support this split** (concrete tasks):
1. `pkg/machinery/constants/constants.go`: add
   `WpaSupplicantExecutablePath = "/usr/local/bin/wpa_supplicant"` (drop the `/usr/bin` path).
2. `internal/app/machined/pkg/system/services/wpa_supplicant.go:76`: use the constant.
3. `internal/app/machined/pkg/controllers/network/wifi_service.go`: before loading services,
   `os.Stat(constants.WpaSupplicantExecutablePath)`; if missing while WifiSpecs exist, log a clear
   error ("wifi extension is not installed") and skip — do not crash-loop the controller. Optionally
   gate on the `ExtensionStatus` resource instead of a stat.
4. Revert base-image bloat: remove `Dockerfile:186-187,798-800` COPYs, `PKG_WPA_SUPPLICANT` from
   `Makefile`/`Dockerfile` args, and the four wireless lines from `hack/modules-amd64.txt`
   (keep them only if maintainers agree cfg80211/mac80211 belong in base — default: they do not).
5. Decide with maintainers whether `wpa_cli`/`wpa_passphrase` ship at all (debug value vs size).

Alternative (fallback if maintainers reject controller-managed supplicants): add
`/usr/bin/wpa_supplicant` to `AllowedPaths` (nvidia-style carve-out) — 1-line change in
`pkg/machinery/extensions/extensions.go` — and keep everything else identical. Mention both options
in the PR description; the `/usr/local/bin` route needs no machinery changes and is preferred.

**Effort:** talos-side 1-2 days; extensions-repo extension (pkg.yaml + manifest + CI wiring +
image-factory schematic availability) 2-4 days; sequencing with pkgs release adds calendar time.

---

## 2. SELinux policy

### Current state

`wpa_supplicant.go` sets no `runner.WithSelinuxLabel(...)`, so the process runs as
`unconfined_service_t` (fallback written by `internal/app/machined/pkg/system/runner/process/process.go:153`).
Every comparable native service is confined (`udevd.go:91`, `dashboard.go:124`, `apid.go:234`, …).
`/run/wpa_supplicant` and the binary are unlabeled beyond generic `run_t`/`bin_exec_t`. `/dev/rfkill`
has no dedicated label.

### How policy is built

* `.cil` sources: `internal/pkg/selinux/policy/selinux/{immutable,common,services}/*.cil`.
* Compiled by the `selinux` Dockerfile stage (`Dockerfile:406-408`):
  `secilc -o /policy/policy.33 -f /policy/file_contexts -c 33 /selinux/**/*.cil -O`, copied back into
  the tree by **`make generate`** (`Dockerfile:466`: `COPY --from=selinux-generate / /internal/pkg/selinux/`).
  The blob is `//go:embed`-ed by `internal/pkg/selinux/selinux.go:26`. So: edit `.cil`, run
  `make generate`, commit `policy.33` + `file_contexts` alongside.
* Device labels are applied by udev: `hack/udevd/90-selinux.rules` (SECLABEL{selinux}).
* Macros: `service_p`/`system_f`/`protected_device_f` in
  `internal/pkg/selinux/policy/selinux/common/typeattributes.cil`; note that
  `common/network.cil:64` already grants `(allow any_p self (netlink_classes (full)))` — nl80211
  (netlink_generic_socket) and rtnetlink are covered by that; the point of the policy is the
  dedicated domain + tight file/socket labels, not netlink plumbing.

### Concrete tasks

1. New file `internal/pkg/selinux/policy/selinux/services/wpa-supplicant.cil` (draft below).
2. `pkg/machinery/constants/constants.go`: add
   `SelinuxLabelWpaSupplicant = "system_u:system_r:wpa_supplicant_t:s0"`.
3. `internal/app/machined/pkg/system/services/wpa_supplicant.go`: add
   `runner.WithSelinuxLabel(constants.SelinuxLabelWpaSupplicant)` and
   `runner.WithDroppedCapabilities(constants.WpaSupplicantDroppedCapabilities)` (new constant —
   keep only `CAP_NET_ADMIN`, `CAP_NET_RAW`; model on `UdevdDroppedCapabilities`).
4. `hack/udevd/90-selinux.rules`: add `KERNEL=="rfkill",SECLABEL{selinux}="system_u:object_r:rfkill_device_t:s0"`.
5. `make generate` to rebuild `policy.33`/`file_contexts`; validate on a SELinux-enforcing node
   (`talosctl dmesg | grep avc`) with `securityState` enforcing.

### Draft `wpa-supplicant.cil`

```cil
; wpa_supplicant WiFi authentication daemon, one instance per wireless link.
; Spawned by machined (init_t) via the process runner.

(type wpa_supplicant_exec_t)
(call system_f (wpa_supplicant_exec_t))
; binary is shipped by the wifi extension under /usr/local
(filecon "/usr/local/bin/wpa_supplicant" file (system_u object_r wpa_supplicant_exec_t (systemLow systemLow)))

(type wpa_supplicant_t)
(call service_p (wpa_supplicant_t wpa_supplicant_exec_t))
; service_p gives: entrypoint/execute on exec type, init_t manage + nosuid_transition (processes.cil:102-107)
(typetransition init_t wpa_supplicant_exec_t process wpa_supplicant_t)
(allow init_t wpa_supplicant_exec_t (file (execute)))

; --- runtime dir: /run/wpa_supplicant (configs written by machined, ctrl sockets by the daemon)
(type wpa_supplicant_run_t)
(call system_f (wpa_supplicant_run_t))
(allow wpa_supplicant_run_t tmpfs_t (filesystem (associate)))
(filecon "/run/wpa_supplicant(/.*)?" any (system_u object_r wpa_supplicant_run_t (systemLow systemLow)))
; machined creates the dir and per-link .conf before starting the service
(typetransition init_t run_t dir "wpa_supplicant" wpa_supplicant_run_t)
(allow init_t wpa_supplicant_run_t (fs_classes (rw)))
; the daemon writes control sockets and reads its config there
(typetransition wpa_supplicant_t run_t dir wpa_supplicant_run_t)
(typetransition wpa_supplicant_t run_t file wpa_supplicant_run_t)
(typetransition wpa_supplicant_t run_t sock_file wpa_supplicant_run_t)
(allow wpa_supplicant_t run_t (dir (search)))
(allow wpa_supplicant_t wpa_supplicant_run_t (fs_classes (rw)))
(allow wpa_supplicant_t wpa_supplicant_run_t (sock_file (create unlink)))

; --- netlink: nl80211 (generic) for driver control, route for link state
; (any_p already has netlink_classes full via common/network.cil; kept explicit for documentation
;  and to survive future tightening of the any_p rule)
(allow wpa_supplicant_t self (netlink_generic_socket (bind create getattr getopt read setopt write)))
(allow wpa_supplicant_t self (netlink_route_socket (bind create getattr nlmsg_read read write)))
(allow wpa_supplicant_t self (udp_socket (create ioctl)))          ; SIOCGIFINDEX et al.
(allow wpa_supplicant_t self (packet_socket (bind create read write))) ; EAPOL frames (l2_packet)
(allow wpa_supplicant_t self (capability (net_admin net_raw)))

; --- rfkill: check/unblock radio kill switches
(type rfkill_device_t)
(call protected_device_f (rfkill_device_t))
(allow wpa_supplicant_t rfkill_device_t (fs_classes (rw)))
; udevd labels /dev/rfkill (90-selinux.rules); udev_t relabel rights come from udev.cil

; --- misc: entropy, config read is on wpa_supplicant_run_t already; sysfs for phy enumeration
(allow wpa_supplicant_t sysfs_t (fs_classes (ro)))

; machined health-check stats the control socket (covered by any_p getattr in processes.cil:183-184)
```

Notes for review: `protected_device_f` for rfkill means pods can no longer poke `/dev/rfkill`
(desired); if maintainers prefer minimal blast radius, use `common_device_f` instead. The explicit
netlink/packet rules are technically redundant today (`network.cil:64`) — call that out in the PR
so reviewers know the domain still works if `any_p` netlink is ever tightened.

**Effort:** 1-2 days including on-node enforcing-mode validation.

---

## 3. Codegen / docs parity

### Current state — `make check-dirty` in CI **fails today**

The CI `default` job (generated `.github/workflows/ci.yaml` from `.kres.yaml`) runs
`make generate docs` then `make check-dirty`. The following regeneratable artifacts are missing
from the branch:

| Missing artifact | Producer |
|---|---|
| `WifiSpecSpec`/`WifiNetwork` messages in `api/resource/definitions/network/network.proto` | `tools/structprotogen` (Dockerfile:381-383, part of `make generate`) |
| `pkg/machinery/api/resource/definitions/network/network.pb.go`, `network_vtproto.pb.go` | buf generate (`make generate`) |
| `network.WifiConfigV1Alpha1` in `pkg/machinery/config/schemas/config.schema.json` | docgen `//go:generate` in `pkg/machinery/config/config.go:8` (`make generate`/`make docs`) |
| `website/content/v1.15/reference/configuration/network/networkwificonfig.md` | `make docs` |
| `website/content/v1.15/schemas/config.schema.json` | `make docs` |
| `WifiSpecSpec` redaction in `pkg/machinery/resources/network/redact.generated.go` | `tools/redactgen` — **requires a source change first**, see below |

### Source-level gaps found

1. **Resource PSK is not redacted.** `pkg/machinery/resources/network/wifi_spec.go` marks the
   resource `meta.Sensitive` (hides it from unauthorized API reads) but `WifiNetwork.PSK` lacks the
   `redact:"replace"` struct tag (compare `link.go:323` `PrivateKey ... redact:"replace"`), so
   redactgen generates nothing and support bundles / `--redact` output would leak the PSK.
   Task: add `redact:"replace"` to the PSK field, rerun `make generate`, commit
   `redact.generated.go`.
2. **Proto conformance test.** `pkg/machinery/resources/network/network_test.go:80+` maps each
   resource to its `networkpb` spec; add `{res: &network.WifiSpec{}, spec: &networkpb.WifiSpecSpec{}}`
   after regeneration (the test enumerating registered resources at line 40-ish also needs
   `&network.WifiSpec{}` in its list — verify after `make generate`).
3. **Missing controller test.** No `wifi_config_test.go`. Add one following
   `internal/app/machined/pkg/controllers/network/ethernet_config_test.go`
   (`ctest.DefaultSuite`, `container.New(...)`, `ctest.AssertResource`). Also consider a
   `wifi_service_test.go` with a mock `WifiServiceManager` (the interface in `wifi_service.go:26-32`
   makes this easy).
4. **Redacted-golden test.** Secret docs conventionally have a `_redacted.yaml` golden
   (`wireguard_test.go:24-25` + `testdata/wireguardconfig_redacted.yaml`); add
   `testdata/networkwificonfig_redacted.yaml` if the current inline assertion in `wifi_test.go`
   doesn't already cover marshal-stability of redacted output.
5. **Stability testdata: no action needed.** `pkg/machinery/config/types/v1alpha1/testdata/stability/`
   only covers documents emitted by `generate.NewInput().Config()`; opt-in docs (Ethernet, Wireguard,
   Wifi) never appear there. Verified: zero ethernet references in stability goldens.
6. **`api/lock.binpb`**: additive proto messages pass `buf breaking`, but run
   `make api-descriptors` if CI complains.

### Commands for full parity

```
make generate        # structprotogen, buf, deep-copy, gotagsrewrite, redactgen, schema, selinux policy
make docs            # website reference + cli.md + schema copy
make fmt lint        # gofumpt/goimports, golangci, protobuf lint, markdown lint
make unit-tests
make check-dirty     # must be clean afterwards
make conformance     # commit policy check
```

### CI scripts that fail as-is (fork-specific)

* `hack/check-extensions-metadata.sh` compares `pkg/machinery/gendata/data/pkgs`/`tools` against the
  `siderolabs/extensions` Makefile via `gh api` — a pkgs **fork** ref makes this a guaranteed
  failure (literal string compare, `set -euo pipefail`, needs authenticated `gh`). For an upstream
  PR this resolves itself: land the pkgs change in `siderolabs/pkgs`, wait for a pkgs tag, let
  renovate bump both talos and extensions. For fork CI, skip the check.
* **PKGS pin inconsistency in this branch:** `Makefile:31` pins `PKGS ?= v1.15.0-alpha.0-13-gb6b2843`,
  which is the pkgs commit *before* the wireless/wpa-supplicant commit (`2dd498d`). Builds only work
  with a manual `make PKGS=...` override. Fix the pin (or note it) before sharing the branch.

**Effort:** 1 day (mostly running targets + the redact tag + two tests), assuming Docker/buildx available.

---

## 4. WifiStatus observability

### Current state

None. Association state is only observable indirectly via `LinkStatus.OperationalState`. Upstream
reviewers will want parity with `EthernetStatus` (introspection is a Talos design pillar).

### Design (follows `EthernetStatusController` precedent)

**Resource** — new file `pkg/machinery/resources/network/wifi_status.go`:

```go
// WifiStatusType = "WifiStatuses.net.talos.dev", DefaultNamespace: network, ID: link name.
//
//gotagsrewrite:gen
type WifiStatusSpec struct {
    SSID          string             `yaml:"ssid" protobuf:"1"`
    BSSID         string             `yaml:"bssid" protobuf:"2"`           // formatted MAC
    Status        string             `yaml:"status" protobuf:"3"`          // authenticated|associated|ibss-joined (nl80211 BSS status)
    FrequencyMHz  uint32             `yaml:"frequencyMHz" protobuf:"4"`
    SignalDBM     int32              `yaml:"signalStrength" protobuf:"5"`  // averaged station signal
    RXBitrateMbps float64            `yaml:"rxBitrate,omitempty" protobuf:"6"`
    TXBitrateMbps float64            `yaml:"txBitrate,omitempty" protobuf:"7"`
    PHYName       string             `yaml:"phy,omitempty" protobuf:"8"`
}
```
Not sensitive (SSID/BSSID are not secrets; matches upstream treatment of link info). Register in:
`pkg/machinery/resources/network/deep_copy.generated.go` go:generate list (`address_spec.go:19`),
`internal/app/machined/pkg/runtime/v1alpha2/v1alpha2_state.go` (next to `EthernetStatus{}`, line ~202),
`pkg/machinery/resources/network/network_test.go`, then `make generate` for proto.

**Controller** — new file `internal/app/machined/pkg/controllers/network/wifi_status.go`:
* Pattern copied from `ethernet_status.go`: `Inputs() nil`, `Outputs: WifiStatusType OutputExclusive`,
  `runtime.WaitForDevicesReady(...)` first.
* Data source: `github.com/mdlayher/wifi` (nl80211 via genetlink) — **new dependency in root
  `go.mod`** (`github.com/mdlayher/genetlink` is already a direct dep, so the tree is warm).
  Per reconcile: `client.Interfaces()` → for wireless interfaces `client.BSS(ifi)` (SSID, BSSID,
  frequency, status) + `client.StationInfo(ifi)` (signal, bitrates).
* Event source: add `internal/app/machined/pkg/controllers/network/watch/nl80211.go` alongside
  `watch/ethtool.go` — a genetlink conn that resolves the `nl80211` family and joins the `mlme` and
  `scan` multicast groups, feeding `trigger.NewDefaultRateLimitedTrigger(ctx, r)`. Degrade to a
  polling ticker (e.g. 30 s, for signal refresh) when the family is absent (no wifi extension /
  no hardware) — log a debug message and return nil, exactly like `ethernet_status.go` does when
  the ethtool watcher fails.
* Cleanup: remove `WifiStatus` for links that disappeared (list-and-diff like other status controllers).

**LinkRefresh interplay** (wireguard precedent, `link_spec.go:783`): LinkSpecController bumps
`LinkRefresh(id=LinkKindWireguard)` because wireguard reconfiguration is invisible to rtnetlink. WiFi
association/disassociation *does* surface via rtnetlink carrier/operstate messages, which
`LinkStatusController`'s rtnetlink watcher (`link_status.go:80`) already picks up — so no
LinkRefresh bump is required for correctness. If review surfaces missed carrier transitions, the
WifiStatusController can bump `network.NewLinkRefresh(NamespaceName, "wifi")` on mlme events; note
this in the PR rather than pre-building it.

**talosctl UX:** `talosctl get wifistatuses` (and `talosctl get wifispecs`, redacted) work generically
via COSI once the resource is registered — no CLI changes required. Optional polish: surface
SSID/signal in `talosctl dashboard`'s network section (follow how `LinkStatus` feeds
`internal/pkg/dashboard`) — defer to a follow-up PR.

**Effort:** 3-5 days including the nl80211 watch plumbing, unit tests (mock via interface over the
wifi client, as `ethernet_status.go` is structured), and proto/codegen re-runs.

---

## 5. arm64 + generic (non-Intel) hardware

### Current state

Strictly amd64 + Intel AX201:
* pkgs fork: `kernel/build/config-arm64` still has `# CONFIG_CFG80211 is not set`,
  `# CONFIG_WLAN is not set` — no wireless stack builds at all on arm64.
* talos: `hack/modules-arm64.txt` has zero wireless entries; `Dockerfile` copies wpa_supplicant and
  firmware into the **amd64 rootfs only** (lines 798-800; the arm64 `pkg-wpa-supplicant-arm64`
  stage at line 187 is defined but unused).
* Firmware selection is a hardcoded `iwlwifi-Qu*.ucode` glob (AX201-only — not even other iwlwifi
  generations).

### Tasks

If the extension route (§1) is taken, most of this moves out of talos entirely:

1. **pkgs (`siderolabs/pkgs` PR):**
   * `kernel/build/config-amd64` *and* `config-arm64`: `CONFIG_WLAN=y`, `CONFIG_CFG80211=m`,
     `CONFIG_MAC80211=m`, `CONFIG_RFKILL=y`, plus vendor drivers as modules. For "generic" coverage
     enable at minimum: `iwlwifi/iwlmvm` (Intel), `ath10k/ath11k/ath12k` (Qualcomm),
     `mt76`-family (MediaTek), `brcmfmac` (Broadcom/RPi — the main arm64 SBC target), `rtw88/rtw89`
     (Realtek). Keep the current fork's amd64 diff but trim it to `=m` everywhere and mirror to arm64.
   * `wpa-supplicant/pkg.yaml`: change install prefix to `/rootfs/usr/local/bin/` (extension
     `AllowedPaths`), keep static linking. Already builds for both arches (bldr handles that).
2. **extensions:** ship firmware per vendor. Upstream already carves firmware into per-vendor
   extensions; either fold wifi firmware into the single wifi extension (simple) or per-vendor
   firmware extensions + one `wpa-supplicant`+modules extension (composable, image-factory friendly).
   `linux-firmware` upstream contains all needed blobs; the regulatory db (`regulatory.db(.p7s)`)
   must be included once.
3. **talos:** if any modules stay in base (maintainer call), mirror additions into
   `hack/modules-arm64.txt`; otherwise delete from `hack/modules-amd64.txt` too. Remove the
   `iwlwifi-Qu*` glob either way — hardware-specific firmware selection belongs in
   extensions/image-factory schematics, not the Dockerfile.

**Effort:** pkgs kernel config both arches 1-2 days (+ build time); extension multi-vendor firmware
1-2 days. No Go code impact (controllers are arch/vendor agnostic — nl80211 throughout).

---

## 6. Upstream review friction (process requirements)

From `CONTRIBUTING.md`, `.github/PULL_REQUEST_TEMPLATE.md`, `.conform.yaml`, `lefthook.yml`, `.kres.yaml`:

* **DCO**: every commit `--signoff`. Current commits (`20f699b70`, pkgs `2dd498d`) have **no
  `Signed-off-by`** → must be reworded/rebased. They also carry `Co-Authored-By: Claude` +
  `Claude-Session:` trailers; harmless to conform, but decide deliberately whether to keep them.
* **Conform policy** (`make conformance`, also a lefthook post-commit hook): header ≤ 89 chars,
  imperative, lowercase, no trailing period; **non-empty body required**; conventional type from
  `{chore, docs, perf, refactor, style, test, release}` + implicit `feat`/`fix`; scopes limited to
  `apid, machined, networkd, talosctl, trustd, kernel, security, ci`. Current subject
  `feat: add WiFi support via NetworkWifiConfig and wpa_supplicant` passes shape checks; US-locale
  spellcheck applies.
* **GPG**: `.conform.yaml` sets `gpg.required: true` with identity in the `siderolabs` GitHub org —
  external PRs are exempted in practice, but sign commits if possible.
* **PR checklist**: linked issue (use #11185), tests, `make conformance fmt lint docs unit-tests`.
* **Split the PR train**: upstream will want (1) pkgs PR, (2) extensions PR, (3) talos PR that only
  bumps refs + adds Go/policy/docs, each landing in order. A single mega-PR baking binaries into the
  base image will be rejected on ethos grounds; reference the extension in the talos PR docs.
* **Integration tests**: no wifi suite exists; QEMU provides no wireless NIC. Precedent for what CI
  can actually run: `internal/integration/api/ethernet.go` (skips off-QEMU) and
  `internal/integration/api/network-config.go` (the wireguard analogue — apply doc via
  `suite.PatchMachineConfig`, assert resources via `rtestutils`). A realistic
  `internal/integration/api/wifi.go`: apply `NetworkWifiConfig` for a nonexistent `wlan0`, assert
  the `WifiSpec` resource appears (config plumbing) and that no service crash-loops; assert removal
  on `"$patch": delete`. Register with `func init() { allSuites = append(allSuites, new(WifiSuite)) }`,
  tag `//go:build integration_api`. Full radio testing stays out of CI (mention `mac80211_hwsim`
  as a possible future QEMU-side option — the module would need enabling in pkgs).
* **New-feature docs**: upstream ships a "what's new" page and networking guides; expect to add a
  WiFi guide under `website/content/v1.15/talos-guides/network/` in the upstream tree (this fork's
  website dir is stripped to `reference/`, so that file can only be authored against upstream).

**Effort:** 0.5-1 day (rebase/sign-off, PR splitting, checklist), plus 1 day for the integration test.

---

## 7. Prioritized roadmap

| # | Priority | Task | Repo | Key files | Effort |
|---|---|---|---|---|---|
| 1 | P0 | Add `redact:"replace"` to `WifiNetwork.PSK` (resource) — secret leak in support bundles | talos | `pkg/machinery/resources/network/wifi_spec.go`, regen `redact.generated.go` | 0.5 h |
| 2 | P0 | Run full codegen + docs; commit proto/schema/website/redact artifacts; fix `network_test.go` proto mapping | talos | `api/resource/definitions/network/network.proto`, `pkg/machinery/api/.../network*.go`, `pkg/machinery/config/schemas/config.schema.json`, `website/content/v1.15/...` | 1 d |
| 3 | P0 | Rebase commits: DCO sign-off, conform-clean messages (talos + pkgs) | both | git history | 0.5 d |
| 4 | P1 | SELinux confinement: `wpa-supplicant.cil`, rfkill udev label, `WithSelinuxLabel` + dropped caps, regen `policy.33` | talos | `internal/pkg/selinux/policy/selinux/services/wpa-supplicant.cil`, `hack/udevd/90-selinux.rules`, `services/wpa_supplicant.go`, `constants.go` | 1-2 d |
| 5 | P1 | Extension split: move binary to `/usr/local/bin`, stat-gate in `WifiServiceController`, strip base-image COPYs/modules | talos | `services/wpa_supplicant.go`, `wifi_service.go`, `Dockerfile`, `Makefile`, `hack/modules-amd64.txt`, `constants.go` | 1-2 d |
| 6 | P1 | pkgs upstreamable: wireless `=m` in **both** arch kernel configs (multi-vendor), wpa-supplicant → `/usr/local/bin`, fix PKGS pin story | pkgs | `kernel/build/config-{amd64,arm64}`, `wpa-supplicant/pkg.yaml` | 1-2 d |
| 7 | P1 | New `wifi` extension: manifest + firmware (multi-vendor + regdb) + wireless modules + supplicant | extensions | new `network/wifi/` (or split per-vendor) | 2-4 d |
| 8 | P2 | Unit tests: `wifi_config_test.go` (ctest), `wifi_service_test.go` (mock manager), redacted golden | talos | `internal/app/machined/pkg/controllers/network/`, `pkg/machinery/config/types/network/testdata/` | 1 d |
| 9 | P2 | `WifiStatus` resource + `WifiStatusController` (mdlayher/wifi, nl80211 mlme watch) + codegen | talos | `pkg/machinery/resources/network/wifi_status.go`, `controllers/network/wifi_status.go`, `watch/nl80211.go`, `v1alpha2_state.go`, `v1alpha2_controller.go` | 3-5 d |
| 10 | P2 | Integration test `api.WifiSuite` (config-plumbing level, QEMU-skip aware) | talos | `internal/integration/api/wifi.go` | 1 d |
| 11 | P3 | Docs: WiFi networking guide, extension README; dashboard SSID/signal display | talos/extensions | upstream `website/content/.../talos-guides/network/wifi.md`, `internal/pkg/dashboard` | 1-2 d |
| 12 | P3 | Nice-to-haves for review: cgroup limits for supplicant (`internal/pkg/cgroup/cgroup.go` switch), `mac80211_hwsim` CI exploration, WPA3-Enterprise scoping statement | talos/pkgs | — | opportunistic |

**Sequencing:** 1-3 immediately (branch is CI-red without them) → 4-5 in the talos tree while 6
lands in pkgs → 7 after a pkgs tag exists → 8-10 alongside → upstream PR train: pkgs → extensions →
talos, PR description linking issue #11185. Total focused effort ≈ 2-3 weeks.

---

## Sources

* [siderolabs/talos#11185 — 802.11 wireless support as a Talos extension (feature request)](https://github.com/siderolabs/talos/issues/11185)
* [siderolabs/talos discussion #8259 — Initial network connectivity with WiFi?](https://github.com/siderolabs/talos/discussions/8259)
* [siderolabs/talos discussion #6911 — WiFi configuration for bare metal](https://github.com/siderolabs/talos/discussions/6911)
* All other findings verified directly in `/Users/tom/Documents/Projects/talos-wifi` and `/Users/tom/Documents/Projects/talos-pkgs` at the commits noted above.
