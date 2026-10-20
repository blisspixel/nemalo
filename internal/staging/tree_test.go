package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type treeProfile struct {
	body    []byte
	kind    string
	members []sevenMember
	stderr  string
	header  string
	wait    bool
}

func treeProfiles(t *testing.T, profiles ...treeProfile) string {
	t.Helper()
	dir := t.TempDir()
	for _, profile := range profiles {
		_, _ = sevenFixture(t, profile.members)
		state := os.Getenv("NEMALO_FAKE_7Z")
		writeFakeRARFile(t, "kind.txt", profile.kind)
		if profile.kind == "-tzip" {
			writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(profile.body)))
		} else if profile.kind == "-tRar" || profile.kind == "-tRar5" {
			writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = -\nVolumes = 1\n", strings.TrimPrefix(profile.kind, "-t"), len(profile.body)))
		}
		writeFakeRARFile(t, "stderr", profile.stderr)
		if profile.header != "" {
			writeFakeRARFile(t, "header.txt", profile.header)
		}
		if profile.wait {
			writeFakeRARFile(t, "wait", "wait for cancellation")
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("profile-%x", sha256.Sum256(profile.body))), []byte(state), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tree-profiles"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEMALO_FAKE_7Z", dir)
	return dir
}

func treeZIP(t *testing.T, members ...member) []byte {
	t.Helper()
	for i := range members {
		members[i].method = zip.Store
	}
	return archive(t, members)
}

func treeRoots(t *testing.T, name string, data []byte) (*os.Root, *os.Root) {
	t.Helper()
	src, dst, _ := roots(t, data)
	if err := src.Rename("a.zip", name); err != nil {
		t.Fatal(err)
	}
	return src, dst
}

func TestArchiveTreeMixedNodesAndExactChains(t *testing.T) {
	book := []byte("A synthetic book.")
	zipBody := treeZIP(t, member{name: "sub/book.epub", body: book}, member{name: "notes.txt", body: []byte("preserve")})
	rarBody := []byte("Rar!\x1a\x07\x01\x00 synthetic RAR")
	sevenBody := []byte("7z\xbc\xaf\x27\x1c synthetic 7z")
	outer := treeZIP(t, member{name: "direct.epub", body: book}, member{name: "inner.7z", body: sevenBody}, member{name: "inner.7z!/book.epub", body: []byte("a distinct direct member")})
	treeProfiles(t,
		treeProfile{body: sevenBody, kind: "-t7z", members: []sevenMember{{name: "next.rar", body: rarBody}}},
		treeProfile{body: rarBody, kind: "-tRar5", members: []sevenMember{{name: "last.zip", body: zipBody, extra: "Volume Index = \nOffset = \n"}}},
		treeProfile{body: zipBody, kind: "-tzip"})
	src, dst := treeRoots(t, "outer.zip", outer)
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, Defaults())
	if err != nil || !x.Complete || x.Whole || len(x.Members) != 7 {
		t.Fatal(x, err)
	}
	var chains [][]string
	for _, m := range x.Members {
		if m.Disposition == "extracted" {
			got, err := dst.ReadFile(m.Staged)
			if err != nil || !bytes.Equal(got, book) && !bytes.Equal(got, []byte("a distinct direct member")) {
				t.Fatal(m, string(got), err)
			}
			if !strings.HasPrefix(filepath.ToSlash(m.Staged), "node-") || strings.Contains(m.Staged, "sub") {
				t.Fatal("member names became filesystem paths", m)
			}
			chains = append(chains, m.Chain)
		}
	}
	want := [][]string{{"direct.epub"}, {"inner.7z", "next.rar", "last.zip", "sub/book.epub"}, {"inner.7z!/book.epub"}}
	if !reflect.DeepEqual(chains, want) {
		t.Fatal(chains)
	}
	got, _ := src.ReadFile("outer.zip")
	if !bytes.Equal(got, outer) {
		t.Fatal("outer source changed")
	}
	// Traversal must not change the extractor cleanup uses.
	plain := t.TempDir()
	plainRoot, err := os.OpenRoot(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer plainRoot.Close()
	y, err := Extract(context.Background(), src, "outer.zip", plainRoot, books, Defaults())
	if err != nil || y.Members[1].Disposition != "nested_archive" || y.Members[1].Staged != "" {
		t.Fatal("single-container extraction expanded its permissions", y, err)
	}
}

func TestArchiveTreeDepthAndSharedMemberBudget(t *testing.T) {
	leaf := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	profiles := []treeProfile{{body: leaf, kind: "-tzip"}}
	outer := leaf
	for i := 0; i < archiveLevels; i++ {
		outer = treeZIP(t, member{name: "child.zip", body: outer})
		profiles = append(profiles, treeProfile{body: outer, kind: "-tzip"})
	}
	treeProfiles(t, profiles...)
	src, dst := treeRoots(t, "outer.zip", outer)
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, Defaults())
	if err != nil || x.Complete || len(x.Members) != archiveLevels || x.Members[len(x.Members)-1].Disposition != "failed" || !strings.Contains(x.Members[len(x.Members)-1].Detail, "four-level") {
		t.Fatal(x, err)
	}
	for _, m := range x.Members {
		if m.Staged != "" {
			t.Fatal("depth-limited content was staged", m)
		}
	}
	inner := treeZIP(t, member{name: "a.epub", body: []byte("one")}, member{name: "b.epub", body: []byte("two")})
	treeProfiles(t, treeProfile{body: inner, kind: "-tzip"})
	outer = treeZIP(t, member{name: "child.zip", body: inner}, member{name: "direct.epub", body: []byte("direct")})
	src, dst = treeRoots(t, "outer.zip", outer)
	limits := Defaults()
	limits.Members = 3 // The two outer entries leave one, not three, for the child.
	x, err = ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
	if err != nil || x.Complete || len(x.Members) != 2 || x.Members[0].Disposition != "failed" || x.Members[1].Disposition != "extracted" {
		t.Fatal(x, err)
	}
}

func TestArchiveTreeChargesContainerBytesAndFailedAttempts(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: bytes.Repeat([]byte("x"), 300)})
	outer := treeZIP(t, member{name: "one.zip", body: inner}, member{name: "two.zip", body: inner})
	treeProfiles(t, treeProfile{body: inner, kind: "-tzip"})
	src, dst := treeRoots(t, "outer.zip", outer)
	limits := Defaults()
	limits.TotalBytes = int64(len(inner))*2 + 2 + 301
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
	if err != nil || x.Complete {
		t.Fatal(x, err)
	}
	counts := map[string]int{}
	for _, m := range x.Members {
		counts[m.Disposition]++
	}
	if counts["extracted"] != 1 || counts["failed"] != 1 {
		t.Fatal(x)
	}
	// Both declared attempts are charged even when neither produces valid bytes.
	body := []byte("7z\xbc\xaf\x27\x1c CRC failure fixture")
	treeProfiles(t, treeProfile{body: body, kind: "-t7z", members: []sevenMember{
		{name: "bad.epub", body: []byte("first"), served: []byte("wrong")},
		{name: "good.epub", body: []byte("second")},
	}})
	src, dst = treeRoots(t, "outer.7z", body)
	limits = Defaults()
	limits.TotalBytes = 7
	x, err = ExtractTree(context.Background(), src, []string{"outer.7z"}, dst, books, limits)
	if err != nil || x.Complete || len(x.Members) != 2 || x.Members[0].Disposition != "failed" || x.Members[1].Disposition != "failed" || !strings.Contains(x.Members[1].Detail, "shared work budget") {
		t.Fatal(x, err)
	}
}

func TestArchiveTreePreservesVolumesUnsupportedAndMissingReaders(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	outer := treeZIP(t,
		member{name: "set.z01", body: []byte("part")}, member{name: "set.zip", body: inner},
		member{name: "old.r00", body: []byte("part")}, member{name: "old.rar", body: []byte("Rar!\x1a\x07\x01\x00")},
		member{name: "new.part1.rar", body: []byte("part")}, member{name: "split.7z.001", body: []byte("part")},
		member{name: "future.tar", body: []byte("tar")}, member{name: "unsupported.zip", body: inner})
	src, dst := treeRoots(t, "outer.zip", outer)
	saved := SevenZip
	SevenZip = func() (string, error) { return "", ErrNoArchiveTool }
	t.Cleanup(func() { SevenZip = saved })
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, Defaults())
	if err != nil || !x.Complete || len(x.Members) != 8 {
		t.Fatal(x, err)
	}
	for i, m := range x.Members {
		if m.Staged != "" || i < 6 && m.Disposition != "nested_volume" || i == 6 && m.Disposition != "nested_archive" || i == 7 && !strings.Contains(m.Detail, "not installed") {
			t.Fatal(m)
		}
	}
}

func TestArchiveTreeSharedSubprocessAndDiagnosticLimits(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	outer := treeZIP(t, member{name: "one.zip", body: inner}, member{name: "two.zip", body: inner})
	dir := treeProfiles(t, treeProfile{body: inner, kind: "-tzip", stderr: strings.Repeat("x", 700)})
	for _, diagnostic := range []bool{false, true} {
		src, dst := treeRoots(t, "outer.zip", outer)
		budget := &treeBudget{members: 10000, bytes: 1 << 30, processes: 1,
			reads: allowance{remaining: 8 << 30}, diagnostics: allowance{remaining: 16 << 20}}
		if diagnostic {
			budget.processes = 1000
			budget.diagnostics.remaining = 1000
		}
		limits := Defaults()
		limits.tree = budget
		x, err := extractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
		if err != nil || x.Complete {
			t.Fatal(x, err)
		}
		count := 0
		for _, m := range x.Members {
			if m.Disposition == "extracted" {
				count++
			}
		}
		if count != 1 {
			t.Fatal("a sibling reset the shared allowance", x)
		}
	}
	if calls, err := os.ReadFile(filepath.Join(dir, "calls")); err != nil || len(calls) != 3 {
		t.Fatal("unexpected process retries", string(calls), err)
	}
}

func TestArchiveTreeReadAllowanceAndExpiredContext(t *testing.T) {
	a := &allowance{remaining: 7}
	r := budgetReaderAt{bytes.NewReader([]byte("12345678")), a}
	var p [4]byte
	if _, err := r.ReadAt(p[:], 0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAt(p[:], 0); !errors.Is(err, errTreeBudget) || a.left() != 0 {
		t.Fatal(err)
	}
	if _, err := io.Copy(budgetWriter{io.Discard, a}, strings.NewReader("x")); !errors.Is(err, errTreeBudget) {
		t.Fatal(err)
	}
	inner := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	src, dst := treeRoots(t, "outer.zip", inner)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	x, err := ExtractTree(ctx, src, []string{"outer.zip"}, dst, books, Defaults())
	if !errors.Is(err, context.DeadlineExceeded) || x.Complete || len(x.Members) != 0 {
		t.Fatal(x, err)
	}
	files, err := os.ReadDir(dst.Name())
	if err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
}

func TestArchiveTreeZIPReadWorkDoesNotResetAtChildBoundaries(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: bytes.Repeat([]byte("x"), 16384)})
	outer := treeZIP(t, member{name: "one.zip", body: inner}, member{name: "two.zip", body: inner})
	treeProfiles(t, treeProfile{body: inner, kind: "-tzip"})
	src, dst := treeRoots(t, "outer.zip", outer)
	limits := Defaults()
	limits.tree = &treeBudget{members: 10000, bytes: 1 << 30, processes: 1000,
		reads: allowance{remaining: int64(len(outer) + len(inner) + 4096)}, diagnostics: allowance{remaining: 16 << 20}}
	x, err := extractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
	if err != nil || x.Complete || limits.tree.reads.left() != 0 {
		t.Fatal(x, err, limits.tree.reads.left())
	}
	var extracted, failed int
	for _, m := range x.Members {
		if m.Disposition == "extracted" {
			extracted++
		}
		if m.Disposition == "failed" {
			failed++
		}
	}
	if extracted != 1 || failed != 1 {
		t.Fatal("descendants did not share the same reader allowance", x)
	}
}

func TestArchiveTreeRetainsHostileAmbiguousAndCorruptChildren(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	bad := []byte("PK\x03\x04 not a readable directory")
	outer := treeZIP(t,
		member{name: "../escape.zip", body: inner}, member{name: "link.zip", body: inner, mode: fs.ModeSymlink | 0o777},
		member{name: "secret.zip", body: inner, flags: 1},
		member{name: "duplicate.zip", body: inner}, member{name: "duplicate.zip", body: inner},
		member{name: "wrong.7z", body: inner}, member{name: "bad.zip", body: bad},
		member{name: "safe.zip", body: inner})
	treeProfiles(t, treeProfile{body: inner, kind: "-tzip"}, treeProfile{body: bad, kind: "-tzip"})
	src, dst := treeRoots(t, "outer.zip", outer)
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, Defaults())
	if err != nil || x.Complete || len(x.Members) != 9 {
		t.Fatal(x, err)
	}
	want := []string{"unsafe_name", "link", "encrypted", "duplicate_name", "duplicate_name", "failed", "failed", "nested_archive", "extracted"}
	for i, m := range x.Members {
		if m.Disposition != want[i] || i < 8 && m.Staged != "" {
			t.Fatal(i, m)
		}
	}
	if got, _ := src.ReadFile("outer.zip"); !bytes.Equal(got, outer) {
		t.Fatal("hostile archive changed")
	}
}

func TestArchiveTreeRootVolumeProbesShareTheRunAllowance(t *testing.T) {
	src, dst, names, _ := segmentZIPFixture(t, 2)
	limits := Defaults()
	limits.tree = &treeBudget{members: 10000, bytes: 1 << 30, processes: 1,
		reads: allowance{remaining: 8 << 30}, diagnostics: allowance{remaining: 16 << 20}}
	x, err := extractTree(context.Background(), src, names, dst, books, limits)
	if err == nil || x.Complete || len(x.Members) != 0 || limits.tree.processes != 0 {
		t.Fatal("volume probes bypassed the shared run allowance", x, err)
	}
}

func TestArchiveTreeLoneNestedVolumesRequireStandaloneEvidence(t *testing.T) {
	zipBody := treeZIP(t, member{name: "hidden.epub", body: []byte("hidden")})
	for _, tc := range []struct {
		name, kind, format string
		body               []byte
	}{
		{"terminal.zip", "-tzip", "zip", zipBody},
		{"lone.rar", "-tRar", "Rar", []byte("Rar!\x1a\x07\x00 isolated RAR4")},
		{"lone.rar", "-tRar5", "Rar5", []byte("Rar!\x1a\x07\x01\x00 isolated RAR5")},
	} {
		t.Run(tc.format, func(t *testing.T) {
			outer := treeZIP(t, member{name: tc.name, body: tc.body}, member{name: "direct.epub", body: []byte("direct")})
			header := fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = +\nVolumes = 1\n", tc.format, len(tc.body))
			dir := treeProfiles(t, treeProfile{body: tc.body, kind: tc.kind, header: header, members: []sevenMember{{name: "hidden.epub", body: []byte("hidden")}}})
			src, dst := treeRoots(t, "outer.zip", outer)
			x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, Defaults())
			if err != nil || x.Complete || len(x.Members) != 2 || x.Members[0].Disposition != "failed" || x.Members[1].Disposition != "extracted" {
				t.Fatal(x, err)
			}
			if calls, err := os.ReadFile(filepath.Join(dir, "calls")); err != nil || len(calls) != 1 {
				t.Fatal("a nested volume reached member extraction", string(calls), err)
			}
		})
	}
}

func TestArchiveTreeCancellationStopsAnActiveChildReader(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	outer := treeZIP(t, member{name: "inner.zip", body: inner})
	dir := treeProfiles(t, treeProfile{body: inner, kind: "-tzip", wait: true})
	src, dst := treeRoots(t, "outer.zip", outer)
	temp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, temp)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ExtractTree(ctx, src, []string{"outer.zip"}, dst, books, Defaults())
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	started := false
	for !started {
		select {
		case <-ticker.C:
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			started = len(calls) == 1
		case err := <-done:
			t.Fatal("reader stopped before cancellation", err)
		case <-ctx.Done():
			t.Fatal("reader did not start", ctx.Err())
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("active child lost cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active child reader did not stop")
	}
	if entries, err := os.ReadDir(temp); err != nil || len(entries) != 0 {
		t.Fatal("private reader snapshot survived cancellation", entries, err)
	}
	if got, _ := src.ReadFile("outer.zip"); !bytes.Equal(got, outer) {
		t.Fatal("cancellation changed source bytes")
	}
}
