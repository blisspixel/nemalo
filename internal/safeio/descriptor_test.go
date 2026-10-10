package safeio

import (
	"runtime"
	"testing"
)

func TestDescriptorBridgeMatchesPlatform(t *testing.T) {
	err := DescriptorBridge()
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		if err != nil {
			t.Fatal(err)
		}
	default:
		if err == nil {
			t.Fatal("unsupported platform reported a descriptor bridge")
		}
	}
}
