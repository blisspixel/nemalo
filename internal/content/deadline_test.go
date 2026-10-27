//go:build !race

package content

import (
	"testing"
	"time"
)

// Outside race builds the content request deadline is the production value.
func TestRequestDeadlineIsFifteenSeconds(t *testing.T) {
	if requestDeadline != 15*time.Second {
		t.Fatal(requestDeadline)
	}
}
