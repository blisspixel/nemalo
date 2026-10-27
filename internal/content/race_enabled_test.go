//go:build race

package content

import "time"

// The race detector slows tokenizing several times over, and CI runs many
// race packages at once. Full-size fixtures keep their production limits;
// only the wall-clock deadline is extended for race builds.
func init() { requestDeadline = 2 * time.Minute }
