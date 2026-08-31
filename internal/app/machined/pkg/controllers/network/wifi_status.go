// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/mdlayher/wifi"
	"go.uber.org/zap"

	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/internal/trigger"
	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/network/watch"
	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// wifiStatusPollInterval is the fallback polling interval to refresh signal strength
// between nl80211 mlme/scan events.
const wifiStatusPollInterval = 30 * time.Second

// WifiStatusController reports Wi-Fi link association statuses.
type WifiStatusController struct{}

// Name implements controller.Controller interface.
func (ctrl *WifiStatusController) Name() string {
	return "network.WifiStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *WifiStatusController) Inputs() []controller.Input {
	return nil
}

// Outputs implements controller.Controller interface.
func (ctrl *WifiStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: network.WifiStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocyclo
func (ctrl *WifiStatusController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	// wait for udevd to be healthy, which implies that all link renames are done
	if err := runtime.WaitForDevicesReady(
		ctx, r,
		[]controller.Input{
			{
				Namespace: network.NamespaceName,
				Type:      network.LinkStatusType,
				Kind:      controller.InputWeak,
			},
		},
	); err != nil {
		return err
	}

	// create a watch connection to nl80211 via genetlink to be notified on association changes;
	// if the nl80211 family is not available (the wireless stack is not loaded yet - no wifi
	// hardware, or cfg80211 loads later e.g. via the wifi extension or a kernel module spec),
	// wait for link changes and retry instead of disabling the controller permanently
	var nl80211Watcher watch.Watcher

	for {
		var err error

		nl80211Watcher, err = watch.NewNL80211(trigger.NewDefaultRateLimitedTrigger(ctx, r))
		if err == nil {
			break
		}

		logger.Debug("nl80211 watch is not available, waiting for wireless links to appear", zap.Error(err))

		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}
	}

	defer nl80211Watcher.Done()

	wifiClient, err := wifi.New()
	if err != nil {
		return fmt.Errorf("error dialing nl80211 socket: %w", err)
	}

	defer wifiClient.Close() //nolint:errcheck

	// polling ticker to refresh signal strength/bitrates between nl80211 events
	ticker := time.NewTicker(wifiStatusPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case <-ticker.C:
		}

		r.StartTrackingOutputs()

		if err = ctrl.reconcile(ctx, r, logger, wifiClient); err != nil {
			return err
		}

		if err = safe.CleanupOutputs[*network.WifiStatus](ctx, r); err != nil {
			return err
		}
	}
}

// reconcile function runs for every reconciliation loop querying nl80211 and updating resources.
func (ctrl *WifiStatusController) reconcile(
	ctx context.Context,
	r controller.Runtime,
	logger *zap.Logger,
	wifiClient *wifi.Client,
) error {
	interfaces, err := wifiClient.Interfaces()
	if err != nil {
		return fmt.Errorf("error listing wireless interfaces: %w", err)
	}

	for _, iface := range interfaces {
		if iface.Name == "" {
			// e.g. P2P devices don't have a netdev
			continue
		}

		lgger := logger.With(zap.String("interface", iface.Name))

		bss, err := wifiClient.BSS(iface)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			lgger.Warn("error getting BSS information", zap.Error(err))
		}

		var station *wifi.StationInfo

		stations, err := wifiClient.StationInfo(iface)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			lgger.Warn("error getting station information", zap.Error(err))
		}

		// in station (client) mode, there is a single station entry: the access point
		if len(stations) > 0 {
			station = stations[0]
		}

		if err = safe.WriterModify(ctx, r, network.NewWifiStatus(network.NamespaceName, iface.Name), func(res *network.WifiStatus) error {
			*res.TypedSpec() = wifiStatusSpec(iface, bss, station)

			return nil
		}); err != nil {
			return fmt.Errorf("error updating WifiStatus resource: %w", err)
		}
	}

	return nil
}

// wifiStatusSpec converts nl80211 interface/BSS/station information into a WifiStatusSpec.
//
// bss and station might be nil when the interface is not associated.
func wifiStatusSpec(iface *wifi.Interface, bss *wifi.BSS, station *wifi.StationInfo) network.WifiStatusSpec {
	spec := network.WifiStatusSpec{
		FrequencyMegahertz: uint32(iface.Frequency),
		PHYName:            fmt.Sprintf("phy%d", iface.PHY),
	}

	if bss != nil {
		spec.SSID = bss.SSID
		spec.BSSID = bss.BSSID.String()
		spec.Status = bss.Status.String()

		if bss.Frequency != 0 {
			spec.FrequencyMegahertz = uint32(bss.Frequency)
		}
	}

	if station != nil {
		spec.SignalDBM = int32(station.SignalAverage)

		if spec.SignalDBM == 0 {
			spec.SignalDBM = int32(station.Signal)
		}

		spec.RXBitrateMbps = float64(station.ReceiveBitrate) / 1e6
		spec.TXBitrateMbps = float64(station.TransmitBitrate) / 1e6
	}

	return spec
}
