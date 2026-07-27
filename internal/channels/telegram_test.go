package channels

import (
	"errors"
	"testing"
)

func TestBindingDead(t *testing.T) {
	dead := []string{
		"Forbidden: bot was blocked by the user",
		"Forbidden: bot can't initiate conversation with a user",
		"Bad Request: chat not found",
		"Forbidden: user is deactivated",
	}
	for _, m := range dead {
		if !bindingDead(errors.New(m)) {
			t.Errorf("should be treated as dead binding: %q", m)
		}
	}
	// Transient / unrelated errors must NOT clear the binding.
	live := []string{
		"Too Many Requests: retry after 5",
		"Bad Request: message is too long",
		"dial tcp: i/o timeout",
		"connection reset by peer",
	}
	for _, m := range live {
		if bindingDead(errors.New(m)) {
			t.Errorf("should NOT be treated as dead binding: %q", m)
		}
	}
}
