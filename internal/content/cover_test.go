package content

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverIsTheDeclaredMemberOfTheExactBytes(t *testing.T) {
	ctx := context.Background()
	withCover := func(files map[string]string) {
		files["book.opf"] = strings.Replace(files["book.opf"], "</manifest>", `<item id="k" href="art.png" media-type="image/png" properties="cover-image"/></manifest>`, 1)
		files["art.png"] = "png bytes"
	}
	req := fixture(t, []string{"<p>text</p>"}, withCover)
	got, err := GetCover(ctx, req.Catalog, req.Root, req.AssetID)
	if err != nil || got.Status != "declared" || got.Member != "art.png" || got.Media != "image/png" || got.Declaration != "cover-image" || string(got.Data) != "png bytes" {
		t.Fatalf("%+v %v", got, err)
	}
	plain := fixture(t, []string{"<p>text</p>"}, nil)
	if got, err := GetCover(ctx, plain.Catalog, plain.Root, plain.AssetID); err != nil || got.Status != "none" || got.Data != nil {
		t.Fatalf("%+v %v", got, err)
	}
	// Same size, different bytes: the whole-file hash refuses it.
	path := filepath.Join(req.Root, "book.epub")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copy(data[len(data)-30:], strings.Repeat("z", 8))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := GetCover(ctx, req.Catalog, req.Root, req.AssetID); err == nil || got.Data != nil || got.Status == "declared" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := GetCover(ctx, req.Catalog, req.Root, "sha256:bad"); err == nil || got.Status != "invalid_request" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := GetCover(ctx, req.Catalog, req.Root, req.AssetID); err == nil || got.Status != "missing_asset" {
		t.Fatalf("%+v %v", got, err)
	}
}
