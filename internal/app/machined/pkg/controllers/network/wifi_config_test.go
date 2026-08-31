// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network_test

import (
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/ctest"
	netctrl "github.com/siderolabs/talos/internal/app/machined/pkg/controllers/network"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	networkcfg "github.com/siderolabs/talos/pkg/machinery/config/types/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

type WifiConfigSuite struct {
	ctest.DefaultSuite
}

func (suite *WifiConfigSuite) TestReconcile() {
	cfg1 := networkcfg.NewWifiConfigV1Alpha1("wlan0")
	cfg1.WifiCountryCode = "NL"
	cfg1.WifiNetworks = []networkcfg.WifiNetworkConfig{
		{
			WifiSSID: "HomeNetwork",
			WifiPSK:  "topsecretpassphrase",
		},
		{
			WifiSSID:   "HiddenNetwork",
			WifiHidden: true,
		},
	}

	ctr, err := container.New(cfg1)
	suite.Require().NoError(err)

	cfg := config.NewMachineConfig(ctr)
	suite.Create(cfg)

	ctest.AssertResource(suite, "wlan0", func(spec *network.WifiSpec, asrt *assert.Assertions) {
		asrt.Equal("NL", spec.TypedSpec().CountryCode)
		asrt.Equal(
			[]network.WifiNetwork{
				{
					SSID: "HomeNetwork",
					PSK:  "topsecretpassphrase",
				},
				{
					SSID:   "HiddenNetwork",
					Hidden: true,
				},
			},
			spec.TypedSpec().Networks,
		)
	})

	// presence of any wifi configuration should request the generic wireless kernel modules
	wifiModuleIDs := []resource.ID{"wifi/cfg80211", "wifi/mac80211"}

	ctest.AssertResources(suite, wifiModuleIDs, func(spec *runtimeres.KernelModuleSpec, asrt *assert.Assertions) {
		asrt.Equal("wifi/"+spec.TypedSpec().Name, spec.Metadata().ID())
		asrt.Empty(spec.TypedSpec().Parameters)
	})

	suite.Destroy(cfg)

	ctest.AssertNoResource[*network.WifiSpec](suite, "wlan0")
	ctest.AssertNoResources[*runtimeres.KernelModuleSpec](suite, wifiModuleIDs)
}

func TestWifiConfigSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, &WifiConfigSuite{
		DefaultSuite: ctest.DefaultSuite{
			Timeout: 10 * time.Second,
			AfterSetup: func(suite *ctest.DefaultSuite) {
				suite.Require().NoError(suite.Runtime().RegisterController(&netctrl.WifiConfigController{}))
			},
		},
	})
}
