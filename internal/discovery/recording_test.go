package discovery

import (
	"strings"
	"testing"
)

// librivoxFixture mirrors a LibriVox item: VBR originals carry track numbers,
// 64 kbps derivatives name their original, and durations use two notations.
func librivoxFixture() map[string]any {
	md5 := strings.Repeat("a", 32)
	return map[string]any{"metadata": map[string]any{"identifier": "demo", "title": "Reading", "mediatype": "audio"}, "files": []any{
		map[string]any{"name": "b_03.mp3", "format": "VBR MP3", "source": "original", "track": "2", "title": "Chapter two", "length": "465.67", "size": "300", "md5": md5},
		map[string]any{"name": "a_01.mp3", "format": "VBR MP3", "source": "original", "track": "1/2", "title": "Chapter one", "length": "507.1", "size": "200", "md5": md5},
		map[string]any{"name": "b_03_64kb.mp3", "format": "64Kbps MP3", "source": "derivative", "original": "b_03.mp3", "title": "Chapter two", "length": "07:45", "size": "150", "md5": md5},
		map[string]any{"name": "a_01_64kb.mp3", "format": "64Kbps MP3", "source": "derivative", "original": "a_01.mp3", "title": "Chapter one", "length": "08:26", "size": "100", "md5": md5},
		map[string]any{"name": "a_01.ogg", "format": "Ogg Vorbis", "size": "90", "md5": md5},
		map[string]any{"name": "cover.jpg", "format": "JPEG", "size": "10", "md5": md5},
	}}
}

func TestRecordingsFollowDeclaredTracksThroughOriginals(t *testing.T) {
	e, err := evaluateFixture(t, librivoxFixture())
	if err != nil || len(e.Recordings) != 2 {
		t.Fatal(e.Recordings, err)
	}
	for _, r := range e.Recordings {
		if !r.Acquirable || r.OrderSource != "declared_track" || len(r.Tracks) != 2 || len(r.Findings) != 0 {
			t.Fatal(r)
		}
		if !strings.HasPrefix(r.Tracks[0].File, "a_01") || r.Tracks[0].Position != 1 || r.Tracks[0].DeclaredTrack != 1 || r.Tracks[1].DeclaredTrack != 2 || r.Tracks[0].Title != "Chapter one" {
			t.Fatal("order", r.Tracks)
		}
	}
	slow := e.Recordings[0]
	if slow.Format != "64Kbps MP3" || slow.Bytes != 250 || slow.Tracks[0].DeclaredSeconds != 506 {
		t.Fatal(slow)
	}
	if fast := e.Recordings[1]; fast.Format != "VBR MP3" || fast.Bytes != 500 || fast.Tracks[0].DeclaredSeconds != 507.1 {
		t.Fatal(fast)
	}
}

func TestRecordingsDiscloseUncertainOrderAndUnusableTracks(t *testing.T) {
	x := librivoxFixture()
	files := x["files"].([]any)
	files[0].(map[string]any)["track"] = "1"                     // duplicate of a_01's track
	delete(files[2].(map[string]any), "md5")                     // a derivative without checksum
	files[3].(map[string]any)["original"] = []string{"a_01.mp3"} // not a string
	e, err := evaluateFixture(t, x)
	if err != nil {
		t.Fatal(err)
	}
	slow, fast := e.Recordings[0], e.Recordings[1]
	if fast.OrderSource != "file_name" || !fast.Acquirable || fast.Tracks[0].File != "a_01.mp3" || fast.Tracks[0].DeclaredTrack != 0 {
		t.Fatal("duplicate tracks not disclosed", fast)
	}
	if slow.Acquirable || slow.OrderSource != "file_name" || len(slow.Findings) != 2 {
		t.Fatal("unusable track not disclosed", slow)
	}
}

func TestRecordingsRefuseRestrictedItems(t *testing.T) {
	x := librivoxFixture()
	x["metadata"].(map[string]any)["access-restricted-item"] = true
	e, err := evaluateFixture(t, x)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range e.Recordings {
		if r.Acquirable {
			t.Fatal("restricted recording acquirable", r)
		}
	}
}

func TestDeclaredTrackAndSeconds(t *testing.T) {
	for raw, want := range map[string]int{`"7"`: 7, `"07"`: 7, `"7/12"`: 7, `7`: 0, `"0"`: 0, `"-1"`: 0, `"x"`: 0, `"10000"`: 0, ``: 0} {
		if got := declaredTrack([]byte(raw)); got != want {
			t.Fatal(raw, got, want)
		}
	}
	for s, want := range map[string]float64{"507.1": 507.1, "08:26": 506, "1:02:03": 3723, "": 0, "1:60": 0, "-3": 0, "NaN": 0, "Inf": 0, "1:2:3:4": 0, "2000000": 0} {
		if got := declaredSeconds(s); got != want {
			t.Fatal(s, got, want)
		}
	}
	if got := textField("a\nb\x00c\tdé", 100); got != "a b c dé" {
		t.Fatalf("%q", got)
	}
}

func TestRecordingsWithTheSameCleanedFormatAreNotAcquirable(t *testing.T) {
	x := librivoxFixture()
	files := x["files"].([]any)
	files[2].(map[string]any)["format"] = "VBR\nMP3"
	files[3].(map[string]any)["format"] = "VBR\nMP3"
	e, err := evaluateFixture(t, x)
	if err != nil || len(e.Recordings) != 2 {
		t.Fatal(e.Recordings, err)
	}
	for _, r := range e.Recordings {
		if r.Format != "VBR MP3" || r.Acquirable {
			t.Fatal("ambiguous format acquirable", r)
		}
	}
}
