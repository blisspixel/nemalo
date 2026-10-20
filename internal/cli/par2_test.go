package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/inventory/inventorytest"
)

func TestInspectPAR2UsesTheSharedInventoryReport(t *testing.T) {
	dir := t.TempDir()
	body := inventorytest.File(inventorytest.Main(1, 4096, 1, 2), inventorytest.Description(1, 2, []byte("../declared\x1b.epub"), 100), inventorytest.Creator(1, "synthetic"))
	if err := os.WriteFile(filepath.Join(dir, "renamed.data"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := execute(t, []string{"inspect", dir, "--json"}, nil)
	if code != 0 || stderr != "" || strings.Contains(out, "par2_metadata") || !strings.Contains(out, `"bytes_read":0`) {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"inspect", dir, "--par2", "--json"}, nil)
	var envelope struct {
		SchemaVersion int              `json:"schema_version"`
		Command       string           `json:"command"`
		Data          inventory.Report `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err, out)
	}
	if code != 0 || stderr != "" || envelope.SchemaVersion != 1 || envelope.Command != "inspect" || envelope.Data.PAR2 == nil || envelope.Data.PAR2.SchemaVersion != 1 || len(envelope.Data.PAR2.Sets) != 1 || envelope.Data.PAR2.ProtectedData != "not_verified" || envelope.Data.BytesRead == 0 || strings.ContainsRune(out, '\x1b') {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"inspect", dir, "--par2"}, nil)
	for _, want := range []string{"PAR2 packet metadata only", "Declared set", "declared", "Protected data: not verified", "Recoverability: not assessed"} {
		if code != 0 || stderr != "" || !strings.Contains(out, want) || strings.ContainsRune(out, '\x1b') {
			t.Fatal(want, code, out, stderr)
		}
	}
	code, out, _ = execute(t, []string{"inspect", dir, "--par2", "--max-total-bytes=10", "--json"}, nil)
	if code != 1 || !strings.Contains(out, `"complete":false`) || !strings.Contains(out, "byte_limit") {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{{"search", "book", "--par2"}, {"check", filepath.Join(dir, "renamed.data"), "--par2"}, {"library", "snapshot", dir, "--output", filepath.Join(t.TempDir(), "unused.json"), "--par2"}} {
		if code, out, _ := execute(t, args, nil); code != 2 {
			t.Fatal("metadata opt-in accepted by another operation", args, code, out)
		}
	}
}
