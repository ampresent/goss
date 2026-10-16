package system

import (
	"errors"
	"strings"
	"testing"
)

// A field ethtool never prints for an interface is marked as "not reported"
// rather than left as an ordinary failure: that is the signal the ethtool
// resource turns into a skipped assertion when an entry asks for
// skip_unsupported.
func TestFieldNotReportedIsMarked(t *testing.T) {
	e := &defEthtool{name: "eth9", loaded: true, exists: true, settings: map[string]string{}}

	_, err := e.Speed()
	if !errors.Is(err, ErrFieldNotReported) {
		t.Errorf("Speed() error = %v, want it to wrap %v", err, ErrFieldNotReported)
	}
	if !strings.Contains(err.Error(), "eth9") || !strings.Contains(err.Error(), "Speed") {
		t.Errorf("error = %q, want it to name eth9 and Speed", err)
	}
}

// A driver that does not support a setting at all answers "Operation not
// supported", which is the same kind of fact as a field it never prints, so a
// baseline can skip the two together.
func TestSubCommandNotSupportedIsMarked(t *testing.T) {
	stubEthtool(t, "if [ \"$1\" = \"-g\" ]; then\n"+
		"  echo 'Cannot get device ring settings: Operation not supported' >&2\n"+
		"  exit 1\n"+
		"fi\n")

	e := &defEthtool{name: "lo", loaded: true, exists: true}

	_, err := e.RingRx()
	if !errors.Is(err, ErrFieldNotReported) {
		t.Errorf("RingRx() error = %v, want it to wrap %v", err, ErrFieldNotReported)
	}
	if !strings.Contains(err.Error(), "Operation not supported") {
		t.Errorf("error = %q, want it to keep the command's own message", err)
	}
}

// A driver that exposes no private flag at all has nothing to assert on, which
// is reported as such rather than as an empty list no rule could ever match.
func TestPrivateFlagsWithoutFlagsIsMarked(t *testing.T) {
	stubEthtool(t, "if [ \"$1\" = \"--show-priv-flags\" ]; then\n"+
		"  echo 'Private flags for lo:'\n"+
		"fi\n")

	e := &defEthtool{name: "lo", loaded: true, exists: true}

	_, err := e.PrivFlags()
	if !errors.Is(err, ErrFieldNotReported) {
		t.Errorf("PrivFlags() error = %v, want it to wrap %v", err, ErrFieldNotReported)
	}
}

// Everything that is not about the hardware is left unmarked, so that
// skip_unsupported can never hide a host goss cannot read at all.
func TestFailuresOnTheHostAreNotMarked(t *testing.T) {
	t.Run("a command that fails for another reason", func(t *testing.T) {
		stubEthtool(t, "if [ \"$1\" = \"-c\" ]; then\n"+
			"  echo 'Cannot get device coalesce settings: Operation not permitted' >&2\n"+
			"  exit 1\n"+
			"fi\n")

		e := &defEthtool{name: "lo", loaded: true, exists: true}

		_, err := e.RxUsecs()
		if err == nil {
			t.Fatal("RxUsecs() = nil error, want the failure")
		}
		if errors.Is(err, ErrFieldNotReported) {
			t.Errorf("RxUsecs() error = %v, want it left unmarked", err)
		}
	})

	t.Run("a missing ethtool binary", func(t *testing.T) {
		e := &defEthtool{name: "lo", loaded: true, err: ErrEthtoolNotFound}

		if _, err := e.RxUsecs(); errors.Is(err, ErrFieldNotReported) {
			t.Errorf("RxUsecs() error = %v, want it left unmarked", err)
		}
	})

	t.Run("an interface that does not exist", func(t *testing.T) {
		e := &defEthtool{name: "definitely-not-an-interface", loaded: true, exists: false}

		if _, err := e.Speed(); errors.Is(err, ErrFieldNotReported) {
			t.Errorf("Speed() error = %v, want it left unmarked", err)
		}
	})
}
