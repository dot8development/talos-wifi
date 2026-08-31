// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/gen/xslices"
	"go.uber.org/zap"

	configtypes "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// wifiKernelModules is the set of kernel modules loaded whenever at least one
// NetworkWifiConfig document is present in the machine configuration.
//
// Talos kernels are built with CONFIG_MODPROBE_PATH="" (no modprobe usermode helper),
// so in-kernel request_module() calls are silently dropped: udevd loads the device
// driver (e.g. iwlwifi) from the PCI modalias, but the driver's own request for its
// op-mode module (iwlmvm) never succeeds, and the wlan interface never appears.
// Loading the full wireless module set explicitly makes WiFi work from a
// NetworkWifiConfig document alone, without requiring a KernelModuleConfig document.
//
// Module dependencies (modules.dep) are resolved by the kmod loader, and loading an
// already-loaded or builtin module is a no-op, so listing the full set is safe on any
// hardware.
var wifiKernelModules = []string{
	"cfg80211", // wireless configuration API (provides nl80211)
	"mac80211", // software MAC layer (softmac drivers)
	"iwlwifi",  // Intel wireless core driver
	"iwlmvm",   // Intel op-mode module, requested by iwlwifi via request_module (disabled by CONFIG_MODPROBE_PATH="")
}

// WifiConfigController manages network.WifiSpec based on machine configuration.
type WifiConfigController struct{}

// Name implements controller.Controller interface.
func (ctrl *WifiConfigController) Name() string {
	return "network.WifiConfigController"
}

// Inputs implements controller.Controller interface.
func (ctrl *WifiConfigController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.ActiveID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *WifiConfigController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: network.WifiSpecType,
			Kind: controller.OutputShared,
		},
		{
			Type: runtimeres.KernelModuleSpecType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *WifiConfigController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		r.StartTrackingOutputs()

		cfg, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.ActiveID)
		if err != nil && !state.IsNotFoundError(err) {
			return fmt.Errorf("error reading machine configuration: %w", err)
		}

		if cfg != nil {
			if err = ctrl.apply(ctx, r, cfg.Config().NetworkWifiConfigs()); err != nil {
				return fmt.Errorf("error applying WifiSpec: %w", err)
			}
		}

		if err = r.CleanupOutputs(ctx,
			resource.NewMetadata(network.NamespaceName, network.WifiSpecType, "", resource.VersionUndefined),
			resource.NewMetadata(runtimeres.NamespaceName, runtimeres.KernelModuleSpecType, "", resource.VersionUndefined),
		); err != nil {
			return fmt.Errorf("error cleaning up outputs: %w", err)
		}
	}
}

func (ctrl *WifiConfigController) apply(ctx context.Context, r controller.Runtime, configs []configtypes.NetworkWifiConfig) error {
	for _, cfg := range configs {
		if err := safe.WriterModify(ctx, r, network.NewWifiSpec(network.NamespaceName, cfg.Name()), func(spec *network.WifiSpec) error {
			spec.TypedSpec().CountryCode = cfg.CountryCode()
			spec.TypedSpec().Networks = xslices.Map(cfg.Networks(), func(n configtypes.WifiNetwork) network.WifiNetwork {
				return network.WifiNetwork{
					SSID:   n.SSID(),
					PSK:    n.PSK(),
					Hidden: n.Hidden(),
				}
			})

			return nil
		}); err != nil {
			return fmt.Errorf("error writing WifiSpec: %w", err)
		}
	}

	if len(configs) > 0 {
		// Talos kernels can't autoload the wireless module stack (see wifiKernelModules),
		// so request it explicitly whenever any wifi configuration is present.
		//
		// The IDs are prefixed with the "wifi" layer to avoid ownership conflicts with
		// runtime.KernelModuleConfigController, which owns the IDs matching module names
		// from KernelModuleConfig documents; runtime.KernelModuleSpecController loads
		// modules by spec.Name, and loading the same module twice is a no-op.
		for _, module := range wifiKernelModules {
			if err := safe.WriterModify(ctx, r,
				runtimeres.NewKernelModuleSpec(runtimeres.NamespaceName, "wifi/"+module),
				func(spec *runtimeres.KernelModuleSpec) error {
					spec.TypedSpec().Name = module

					return nil
				},
			); err != nil {
				return fmt.Errorf("error writing KernelModuleSpec: %w", err)
			}
		}
	}

	return nil
}
