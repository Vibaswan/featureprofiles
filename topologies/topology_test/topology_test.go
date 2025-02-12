// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package topology_test configures just the ports on DUT and ATE,
// assuming that DUT port i is connected to ATE i.  It detects the
// number of ports in the testbed and can be used with the 2, 4, 12
// port variants of the atedut testbed.
package topology_test

import (
	"fmt"
	"testing"

	"github.com/openconfig/featureprofiles/internal/deviations"
	"github.com/openconfig/featureprofiles/internal/fptest"
	"github.com/openconfig/ondatra"
	"github.com/openconfig/ondatra/telemetry"
	"github.com/openconfig/ygot/ygot"
)

func TestMain(m *testing.M) {
	fptest.RunTests(m)
}

// plen is the IPv4 prefix length used for IPv4 assignments in this
// topology.
const plen = 30

// dutPortIP assigns IP addresses for DUT port i, where i is the index
// of the port slices returned by dut.Ports().
func dutPortIP(i int) string {
	return fmt.Sprintf("192.0.2.%d", i*4+1)
}

// atePortCIDR assigns IP addresses with prefixlen for ATE port i, where
// i is the index of the port slices returned by ate.Ports().
func atePortCIDR(i int) string {
	return fmt.Sprintf("192.0.2.%d/30", i*4+2)
}

func configInterface(name, desc, ipv4 string, prefixlen uint8) *telemetry.Interface {
	i := &telemetry.Interface{}
	i.Name = ygot.String(name)
	i.Description = ygot.String(desc)
	i.Type = telemetry.IETFInterfaces_InterfaceType_ethernetCsmacd

	if *deviations.InterfaceEnabled {
		i.Enabled = ygot.Bool(true)
	}

	s := i.GetOrCreateSubinterface(0)
	s4 := s.GetOrCreateIpv4()

	if *deviations.InterfaceEnabled {
		s4.Enabled = ygot.Bool(true)
	}

	a := s4.GetOrCreateAddress(ipv4)
	a.PrefixLength = ygot.Uint8(prefixlen)
	return i
}

func configureDUT(t *testing.T, dut *ondatra.DUTDevice, dutPorts []*ondatra.Port) {
	var (
		badReplace []string
		badConfig  []string
		badTelem   []string
	)

	d := dut.Config()

	// TODO(liulk): configure breakout ports when Ondatra is able to
	// specify them in the testbed for reservation.

	for i, dp := range dutPorts {
		di := d.Interface(dp.Name())
		in := configInterface(dp.Name(), dp.String(), dutPortIP(i), plen)
		fptest.LogYgot(t, fmt.Sprintf("%s to Replace()", dp), di, in)
		if ok := fptest.NonFatal(t, func(t testing.TB) { di.Replace(t, in) }); !ok {
			badReplace = append(badReplace, dp.Name())
		}
	}

	for _, dp := range dutPorts {
		di := d.Interface(dp.Name())
		if q := di.Lookup(t); q.IsPresent() {
			fptest.LogYgot(t, fmt.Sprintf("%s from Get()", dp), di, q.Val(t))
		} else {
			badConfig = append(badConfig, dp.Name())
			t.Errorf("Config %v Get() failed", di)
		}
	}

	dt := dut.Telemetry()
	for _, dp := range dutPorts {
		di := dt.Interface(dp.Name())
		if q := di.Lookup(t); q.IsPresent() {
			fptest.LogYgot(t, fmt.Sprintf("%s from Get()", dp), di, q.Val(t))
		} else {
			badTelem = append(badTelem, dp.Name())
			t.Errorf("Telemetry %v Get() failed", di)
		}
	}

	t.Logf("Replace error on interfaces: %v", badReplace)
	t.Logf("Config Get error on interfaces: %v", badConfig)
	t.Logf("Telemetry Get error on interfaces: %v", badTelem)
}

func configureATE(t *testing.T, ate *ondatra.ATEDevice, atePorts []*ondatra.Port) {
	top := ate.Topology().New()

	for i, ap := range atePorts {
		t.Logf("ATE AddInterface: ports[%d] = %v", i, ap)
		in := top.AddInterface(ap.Name()).WithPort(ap)
		in.IPv4().WithAddress(atePortCIDR(i)).WithDefaultGateway(dutPortIP(i))
		// TODO(liulk): disable FEC for 100G-FR ports when Ondatra is able
		// to specify the optics type.  Ixia Novus 100G does not support
		// RS-FEC(544,514) used by 100G-FR/100G-DR; it only supports
		// RS-FEC(528,514).
		if false {
			t.Logf("Disabling FEC on port %v", ap)
			in.Ethernet().FEC().WithEnabled(false)
		}
	}

	top.Push(t).StartProtocols(t)
}

func TestTopology(t *testing.T) {
	// Configure the DUT
	dut := ondatra.DUT(t, "dut")
	dutPorts := fptest.SortPorts(dut.Ports())
	configureDUT(t, dut, dutPorts)

	// Configure the ATE
	ate := ondatra.ATE(t, "ate")
	atePorts := fptest.SortPorts(ate.Ports())
	configureATE(t, ate, atePorts)

	// Query Telemetry
	t.Run("Telemetry", func(t *testing.T) {
		const want = telemetry.Interface_OperStatus_UP

		dt := dut.Telemetry()
		for _, dp := range dutPorts {
			if got := dt.Interface(dp.Name()).OperStatus().Get(t); got != want {
				t.Errorf("%s oper-status got %v, want %v", dp, got, want)
			}
		}

		at := ate.Telemetry()
		for _, ap := range atePorts {
			if got := at.Interface(ap.Name()).OperStatus().Get(t); got != want {
				t.Errorf("%s oper-status got %v, want %v", ap, got, want)
			}
		}
	})
}
