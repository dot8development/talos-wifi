// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"

	"github.com/siderolabs/talos/pkg/machinery/proto"
)

// WifiStatusType is type of WifiStatus resource.
const WifiStatusType = resource.Type("WifiStatuses.net.talos.dev")

// WifiStatus resource holds WiFi association status of a wireless link.
type WifiStatus = typed.Resource[WifiStatusSpec, WifiStatusExtension]

// WifiStatusSpec describes WiFi association status of a wireless link.
//
//gotagsrewrite:gen
type WifiStatusSpec struct {
	SSID          string  `yaml:"ssid" protobuf:"1"`
	BSSID         string  `yaml:"bssid" protobuf:"2"`  // formatted MAC
	Status        string  `yaml:"status" protobuf:"3"` // authenticated|associated|IBSS-joined (nl80211 BSS status)
	FrequencyMHz  uint32  `yaml:"frequencyMHz" protobuf:"4"`
	SignalDBM     int32   `yaml:"signalStrength" protobuf:"5"` // averaged station signal
	RXBitrateMbps float64 `yaml:"rxBitrate,omitempty" protobuf:"6"`
	TXBitrateMbps float64 `yaml:"txBitrate,omitempty" protobuf:"7"`
	PHYName       string  `yaml:"phy,omitempty" protobuf:"8"`
}

// NewWifiStatus initializes a WifiStatus resource.
func NewWifiStatus(namespace resource.Namespace, id resource.ID) *WifiStatus {
	return typed.NewResource[WifiStatusSpec, WifiStatusExtension](
		resource.NewMetadata(namespace, WifiStatusType, id, resource.VersionUndefined),
		WifiStatusSpec{},
	)
}

// WifiStatusExtension provides auxiliary methods for WifiStatus.
type WifiStatusExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (WifiStatusExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             WifiStatusType,
		DefaultNamespace: NamespaceName,
		PrintColumns: []meta.PrintColumn{
			{
				Name:     "SSID",
				JSONPath: `{.ssid}`,
			},
			{
				Name:     "Status",
				JSONPath: `{.status}`,
			},
			{
				Name:     "Signal",
				JSONPath: `{.signalStrength}`,
			},
		},
		Sensitivity: meta.NonSensitive,
	}
}

func init() {
	proto.RegisterDefaultTypes()

	err := protobuf.RegisterDynamic[WifiStatusSpec](WifiStatusType, &WifiStatus{})
	if err != nil {
		panic(err)
	}
}
