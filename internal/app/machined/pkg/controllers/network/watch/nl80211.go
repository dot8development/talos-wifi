// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package watch

import (
	"errors"
	"fmt"
	"sync"

	"github.com/mdlayher/genetlink"
)

const (
	// nl80211 generic netlink family and multicast group names,
	// see include/uapi/linux/nl80211.h (not exported by x/sys/unix).
	nl80211FamilyName = "nl80211"
	nl80211GroupMLME  = "mlme"
	nl80211GroupScan  = "scan"
)

type nl80211Watcher struct {
	wg   sync.WaitGroup
	conn *genetlink.Conn
}

// NewNL80211 starts an nl80211 watch on the "mlme" and "scan" multicast groups.
//
// It fails if the nl80211 generic netlink family is not registered, i.e. when
// the cfg80211 module is not loaded (no wireless stack or no wifi extension).
func NewNL80211(trigger Trigger) (Watcher, error) {
	watcher := &nl80211Watcher{}

	var err error

	watcher.conn, err = genetlink.Dial(nil)
	if err != nil {
		return nil, fmt.Errorf("error dialing nl80211 watch socket: %w", err)
	}

	family, err := watcher.conn.GetFamily(nl80211FamilyName)
	if err != nil {
		watcher.conn.Close() //nolint:errcheck

		return nil, fmt.Errorf("error getting family information for nl80211: %w", err)
	}

	var joined bool

	for _, g := range family.Groups {
		if g.Name == nl80211GroupMLME || g.Name == nl80211GroupScan {
			if err = watcher.conn.JoinGroup(g.ID); err != nil {
				watcher.conn.Close() //nolint:errcheck

				return nil, fmt.Errorf("error joining multicast group %q for nl80211: %w", g.Name, err)
			}

			joined = true
		}
	}

	if !joined {
		watcher.conn.Close() //nolint:errcheck

		return nil, errors.New("could not find mlme/scan multicast group IDs for nl80211")
	}

	watcher.wg.Go(func() {
		for {
			_, _, watchErr := watcher.conn.Receive()
			if watchErr != nil {
				return
			}

			trigger.QueueReconcile()
		}
	})

	return watcher, nil
}

func (watcher *nl80211Watcher) Done() {
	watcher.conn.Close() //nolint:errcheck

	watcher.wg.Wait()
}
