// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

import (
	"net"
	"testing"

	"github.com/mdlayher/wifi"
	"github.com/stretchr/testify/assert"

	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

func TestWifiStatusSpec(t *testing.T) {
	t.Parallel()

	iface := &wifi.Interface{
		Index:     4,
		Name:      "wlan0",
		PHY:       0,
		Type:      wifi.InterfaceTypeStation,
		Frequency: 2412,
	}

	t.Run("not associated", func(t *testing.T) {
		t.Parallel()

		spec := wifiStatusSpec(iface, nil, nil)

		assert.Equal(t, network.WifiStatusSpec{
			FrequencyMHz: 2412,
			PHYName:      "phy0",
		}, spec)
	})

	t.Run("associated", func(t *testing.T) {
		t.Parallel()

		bss := &wifi.BSS{
			SSID:      "talos-net",
			BSSID:     net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01},
			Frequency: 5180,
			Status:    wifi.BSSStatusAssociated,
		}

		station := &wifi.StationInfo{
			Signal:          -60,
			SignalAverage:   -55,
			ReceiveBitrate:  866_700_000,
			TransmitBitrate: 400_000_000,
		}

		spec := wifiStatusSpec(iface, bss, station)

		assert.Equal(t, network.WifiStatusSpec{
			SSID:          "talos-net",
			BSSID:         "de:ad:be:ef:00:01",
			Status:        "associated",
			FrequencyMHz:  5180,
			SignalDBM:     -55,
			RXBitrateMbps: 866.7,
			TXBitrateMbps: 400,
			PHYName:       "phy0",
		}, spec)
	})

	t.Run("station signal fallback", func(t *testing.T) {
		t.Parallel()

		station := &wifi.StationInfo{
			Signal: -70,
		}

		spec := wifiStatusSpec(iface, nil, station)

		assert.Equal(t, int32(-70), spec.SignalDBM)
	})
}
