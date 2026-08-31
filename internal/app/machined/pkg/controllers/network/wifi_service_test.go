// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talos/internal/app/machined/pkg/controllers/ctest"
	netctrl "github.com/siderolabs/talos/internal/app/machined/pkg/controllers/network"
	"github.com/siderolabs/talos/internal/app/machined/pkg/system"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// fakeServiceManager is a fake implementation of netctrl.WifiServiceManager recording service operations.
type fakeServiceManager struct {
	mu sync.Mutex

	loaded  map[string]system.Service
	started map[string]bool
	events  []string
}

func newFakeServiceManager() *fakeServiceManager {
	return &fakeServiceManager{
		loaded:  map[string]system.Service{},
		started: map[string]bool{},
	}
}

func (f *fakeServiceManager) IsRunning(id string) (system.Service, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	svc, ok := f.loaded[id]
	if !ok {
		return nil, false, fmt.Errorf("service %q not loaded", id)
	}

	return svc, f.started[id], nil
}

func (f *fakeServiceManager) Load(services ...system.Service) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var ids []string

	for _, svc := range services {
		id := svc.ID(nil)

		f.loaded[id] = svc
		ids = append(ids, id)
	}

	return ids
}

func (f *fakeServiceManager) Start(serviceIDs ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, id := range serviceIDs {
		f.started[id] = true
		f.events = append(f.events, "start:"+id)
	}

	return nil
}

func (f *fakeServiceManager) Stop(_ context.Context, serviceIDs ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, id := range serviceIDs {
		f.started[id] = false
		f.events = append(f.events, "stop:"+id)
	}

	return nil
}

func (f *fakeServiceManager) Unload(_ context.Context, serviceIDs ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, id := range serviceIDs {
		delete(f.loaded, id)
		f.events = append(f.events, "unload:"+id)
	}

	return nil
}

func (f *fakeServiceManager) Events() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.events)
}

func TestWifiServiceController(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			wlan0Service = "wpa-supplicant-wlan0"
			wlan1Service = "wpa-supplicant-wlan1"
		)

		manager := newFakeServiceManager()
		runDir := t.TempDir()

		suite := &ctest.DefaultSuite{
			AfterSetup: func(s *ctest.DefaultSuite) {
				s.Require().NoError(s.Runtime().RegisterController(&netctrl.WifiServiceController{
					V1Alpha1Services: manager,
					RunDir:           runDir,
					SupplicantPathFunc: func() (string, error) {
						return "/usr/bin/wpa_supplicant", nil
					},
				}))
			},
		}
		suite.SetT(t)
		suite.SetupTest()

		defer suite.TearDownTest()

		// WifiSpec for an absent link: should be skipped without starting anything
		specAbsent := network.NewWifiSpec(network.NamespaceName, "wlan1")
		specAbsent.TypedSpec().Networks = []network.WifiNetwork{{SSID: "AbsentNetwork", PSK: "passphrase1"}}
		suite.Create(specAbsent)

		// WifiSpec for wlan0, link not present yet
		spec := network.NewWifiSpec(network.NamespaceName, "wlan0")
		spec.TypedSpec().CountryCode = "NL"
		spec.TypedSpec().Networks = []network.WifiNetwork{{SSID: "HomeNetwork", PSK: "topsecretphrase"}}
		suite.Create(spec)

		synctest.Wait()

		require.Empty(t, manager.Events())

		// once the link appears, the supplicant should be started
		linkStatus := network.NewLinkStatus(network.NamespaceName, "wlan0")
		linkStatus.TypedSpec().Type = nethelpers.LinkEther
		suite.Create(linkStatus)

		synctest.Wait()

		require.Equal(t, []string{"start:" + wlan0Service}, manager.Events())

		confPath := filepath.Join(runDir, "wlan0.conf")

		conf, err := os.ReadFile(confPath)
		require.NoError(t, err)
		assert.Contains(t, string(conf), "country=NL")
		// SSIDs are rendered in hex-encoded form
		assert.Contains(t, string(conf), "ssid="+hex.EncodeToString([]byte("HomeNetwork")))

		// changing the spec should restart the supplicant with the new configuration
		ctest.UpdateWithConflicts(suite, spec, func(spec *network.WifiSpec) error {
			spec.TypedSpec().CountryCode = ""
			spec.TypedSpec().Networks = []network.WifiNetwork{{SSID: "OtherNetwork", PSK: "otherpassphrase"}}

			return nil
		})

		synctest.Wait()

		require.Equal(t, []string{
			"start:" + wlan0Service,
			"stop:" + wlan0Service, "unload:" + wlan0Service,
			"start:" + wlan0Service,
		}, manager.Events())

		conf, err = os.ReadFile(confPath)
		require.NoError(t, err)
		assert.NotContains(t, string(conf), "country=")
		assert.Contains(t, string(conf), "ssid="+hex.EncodeToString([]byte("OtherNetwork")))

		// removing the spec should stop the supplicant and remove the configuration
		suite.Destroy(spec)

		synctest.Wait()

		require.Equal(t, []string{
			"start:" + wlan0Service,
			"stop:" + wlan0Service, "unload:" + wlan0Service,
			"start:" + wlan0Service,
			"stop:" + wlan0Service, "unload:" + wlan0Service,
		}, manager.Events())

		_, err = os.Stat(confPath)
		assert.True(t, os.IsNotExist(err))

		// the absent-link spec should never have been acted upon
		for _, event := range manager.Events() {
			assert.NotContains(t, event, wlan1Service)
		}
	})
}
