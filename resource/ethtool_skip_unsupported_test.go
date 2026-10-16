package resource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/goss-org/goss/system"
	"github.com/goss-org/goss/util"
)

// missingAttributeEthtool reports the same values as the shared fake, except for
// the attributes the interface is declared not to have, which fail the way the
// system layer fails when the hardware has nothing to say about a field.
type missingAttributeEthtool struct {
	fakeSysEthtool
	missing map[string]bool
}

func (f *missingAttributeEthtool) notReported(key string, get func() (string, error)) (string, error) {
	if f.missing[key] {
		return "", fmt.Errorf("%w %q for interface %s", system.ErrFieldNotReported, key, f.name)
	}
	return get()
}

func (f *missingAttributeEthtool) Speed() (string, error) {
	return f.notReported("speed", f.fakeSysEthtool.Speed)
}

func (f *missingAttributeEthtool) RingRxJumbo() (string, error) {
	return f.notReported("rx_jumbo_ring", f.fakeSysEthtool.RingRxJumbo)
}

func (f *missingAttributeEthtool) PrivFlags() ([]string, error) {
	if f.missing["priv_flags"] {
		return nil, fmt.Errorf("%w %q for interface %s", system.ErrFieldNotReported, "priv_flags", f.name)
	}
	return f.fakeSysEthtool.PrivFlags()
}

// missingAttributeSystem fixes the interface list and the ethtool values, so the
// skip behaviour can be driven without running ethtool(8) or touching the host.
func missingAttributeSystem(t *testing.T, ifaces map[string]*missingAttributeEthtool) *system.System {
	t.Helper()

	names := make([]string, 0, len(ifaces))
	for name := range ifaces {
		names = append(names, name)
	}
	sort.Strings(names)

	return &system.System{
		NewEthtool: func(_ context.Context, name string, _ *system.System, _ util.Config) system.Ethtool {
			iface, ok := ifaces[name]
			if !ok {
				return &fakeSysEthtool{name: name, err: errors.New("interface does not exist")}
			}
			iface.name = name
			return iface
		},
		InterfaceNames: func() ([]string, error) { return names, nil },
	}
}

// resultFor returns the single result for an interface and attribute, so a test
// can assert on it without depending on the order results are produced in.
func resultFor(t *testing.T, results []TestResult, id string, property string) TestResult {
	t.Helper()

	for _, r := range results {
		if r.ResourceId == id && r.Property == property {
			return r
		}
	}
	t.Fatalf("no result for %s.%s in %+v", id, property, results)
	return TestResult{}
}

// An attribute the interface does not report is a failure by default, and a
// skipped assertion when the entry asks for skip_unsupported: that is what lets
// one baseline name settings only some drivers have.
func TestSkipUnsupportedRecordsUnreportedAttributeAsSkipped(t *testing.T) {
	sys := missingAttributeSystem(t, map[string]*missingAttributeEthtool{
		"eth0": {fakeSysEthtool: goodInterface(), missing: map[string]bool{"rx_jumbo_ring": true}},
	})

	e := &Ethtool{id: EthtoolDefaultID, RxJumboRing: "0"}
	results := e.Validate(t.Context(), sys)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].Result != FAIL {
		t.Errorf("result = %d, want FAIL without skip_unsupported", results[0].Result)
	}

	e.SkipUnsupported = true
	results = e.Validate(t.Context(), sys)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	skipped := results[0]
	if skipped.Result != SKIP || !skipped.Skipped {
		t.Errorf("result = %d (skipped=%t), want SKIP", skipped.Result, skipped.Skipped)
	}
	if skipped.ResourceId != "default: eth0" || skipped.Property != "rx_jumbo_ring" {
		t.Errorf("skipped result names %s.%s, want default: eth0.rx_jumbo_ring", skipped.ResourceId, skipped.Property)
	}
}

// A value the interface does report is still compared, so skip_unsupported
// cannot be used to make a baseline that does not test anything.
func TestSkipUnsupportedStillFailsOnAReportedValue(t *testing.T) {
	reported := goodInterface()
	reported.rxJumboRing = "512"
	sys := missingAttributeSystem(t, map[string]*missingAttributeEthtool{
		"eth0": {fakeSysEthtool: reported},
	})

	e := &Ethtool{id: EthtoolDefaultID, RxJumboRing: "0", SkipUnsupported: true}
	results := e.Validate(t.Context(), sys)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].Result != FAIL {
		t.Errorf("result = %d, want FAIL for a value that does not match", results[0].Result)
	}
}

// A failure that says something is wrong on the host -- no ethtool binary, an
// interface that is gone -- stays a failure, because skipping it would hide a
// host goss cannot read at all.
func TestSkipUnsupportedDoesNotHideHostFailures(t *testing.T) {
	sys := missingAttributeSystem(t, map[string]*missingAttributeEthtool{
		"eth0": {fakeSysEthtool: fakeSysEthtool{err: system.ErrEthtoolNotFound}},
	})

	e := &Ethtool{id: "eth0", Speed: "10000Mb/s", SkipUnsupported: true}
	results := e.Validate(t.Context(), sys)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].Result != FAIL {
		t.Errorf("result = %d, want FAIL when ethtool cannot be run", results[0].Result)
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), system.ErrEthtoolNotFound.Error()) {
		t.Errorf("error = %v, want the missing binary to be reported", results[0].Err)
	}
}

// An interface whose driver exposes no private flag is skipped under
// skip_unsupported, so a rule naming a vendor specific flag can be kept in a
// baseline for a mixed fleet.
func TestSkipUnsupportedSkipsInterfaceWithoutPrivateFlags(t *testing.T) {
	sys := missingAttributeSystem(t, map[string]*missingAttributeEthtool{
		"eth0": {fakeSysEthtool: goodInterface(), missing: map[string]bool{"priv_flags": true}},
	})

	e := &Ethtool{id: EthtoolDefaultID, PrivFlags: []any{"ctpio: on"}}
	results := e.Validate(t.Context(), sys)
	if len(results) != 1 || results[0].Result != FAIL {
		t.Fatalf("results = %+v, want a single failure without skip_unsupported", results)
	}

	e.SkipUnsupported = true
	results = e.Validate(t.Context(), sys)
	if len(results) != 1 || results[0].Result != SKIP {
		t.Fatalf("results = %+v, want a single skipped result", results)
	}
}

// A mixed fleet keeps every assertion it can make: only the attributes the
// interface has nothing to report for are skipped.
func TestSkipUnsupportedKeepsTheRestOfTheBaseline(t *testing.T) {
	sys := missingAttributeSystem(t, map[string]*missingAttributeEthtool{
		"eth0": {fakeSysEthtool: goodInterface()},
		"eth1": {fakeSysEthtool: goodInterface(), missing: map[string]bool{"rx_jumbo_ring": true}},
	})

	e := &Ethtool{id: EthtoolDefaultID, Speed: "10000Mb/s", RxJumboRing: "0", SkipUnsupported: true}
	results := e.Validate(t.Context(), sys)

	if len(results) != 4 {
		t.Fatalf("got %d results, want 4: %+v", len(results), results)
	}
	counts := map[int]int{}
	for _, r := range results {
		counts[r.Result]++
	}
	if counts[SUCCESS] != 3 || counts[SKIP] != 1 || counts[FAIL] != 0 {
		t.Errorf("got %d passed, %d skipped, %d failed, want 3, 1, 0", counts[SUCCESS], counts[SKIP], counts[FAIL])
	}
	if got := resultFor(t, results, "default: eth0", "rx_jumbo_ring").Result; got != SUCCESS {
		t.Errorf("eth0 rx_jumbo_ring = %d, want SUCCESS", got)
	}
	if got := resultFor(t, results, "default: eth1", "speed").Result; got != SUCCESS {
		t.Errorf("eth1 speed = %d, want SUCCESS", got)
	}
	if got := resultFor(t, results, "default: eth1", "rx_jumbo_ring").Result; got != SKIP {
		t.Errorf("eth1 rx_jumbo_ring = %d, want SKIP", got)
	}
}
