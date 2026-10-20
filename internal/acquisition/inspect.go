package acquisition

import (
	"errors"
	"fmt"
	"os"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
)

// States of an existing intake packet, read offline.
const (
	PacketImportable = "importable"
	PacketResumable  = "resumable"
	PacketRetained   = "retained"
)

// PacketState is what an existing packet holds. Remaining is, for a
// resumable packet, the most bytes continuing it may still transfer.
type PacketState struct {
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`
	Remaining int64  `json:"remaining_bytes,omitempty"`
	// Selection is the source and file or recording declared by this packet.
	// Callers must compare it with their requested selection before reuse.
	SourceID  string `json:"source_id,omitempty"`
	File      string `json:"source_file,omitempty"`
	Recording string `json:"source_format,omitempty"`
}

func retained(format string, args ...any) PacketState {
	return PacketState{State: PacketRetained, Detail: fmt.Sprintf(format, args...)}
}

// InspectPacket reads one file packet without changing it: importable when
// its receipt records completed untrusted intake, resumable when acquire
// --resume could continue it, and otherwise retained with the reason.
func InspectPacket(dir string) PacketState {
	root, err := safeio.OpenRoot(dir)
	if err != nil {
		return retained("the packet cannot be opened")
	}
	defer root.Close()
	if _, err := root.Lstat("receipt.json"); err == nil {
		var receipt Result
		if err := readRecord(root, "receipt.json", &receipt); err != nil {
			return retained("its receipt cannot be read")
		}
		if !receipt.Complete || receipt.Status != "untrusted_intake" {
			return retained("its receipt records %s; it is not imported", receipt.Status)
		}
		state := PacketState{State: PacketImportable}
		if receipt.Intent != nil {
			state.SourceID, state.File = receipt.Intent.Request.SourceID, receipt.Intent.Request.File
		}
		return state
	} else if !errors.Is(err, os.ErrNotExist) {
		return retained("its receipt cannot be read")
	}
	var intent Intent
	if err := readRecord(root, "intent.json", &intent); err != nil || intent.SchemaVersion != 1 || len(intent.Selection.Files) != 1 {
		return retained("it holds no intent from acquire")
	}
	file := intent.Selection.Files[0]
	if discovery.ValidArxivID(intent.Request.SourceID) {
		return retained("an arXiv transfer cannot be resumed")
	}
	if !resumable(root, file) {
		return retained("its transfer has no strong validator or no partial content, so it cannot be resumed")
	}
	info, err := root.Lstat("content." + contentFormat(file))
	if err != nil {
		return retained("its partial content cannot be read")
	}
	var total int64
	if file.Bytes != nil {
		total = *file.Bytes
	}
	var rec transferRecord
	if file.Access == discovery.GutenbergEPUBAccess && readRecord(root, "transfer.json", &rec) == nil && rec.ContentLength > 0 {
		total = rec.ContentLength
	}
	return PacketState{State: PacketResumable, Remaining: total - info.Size(), SourceID: intent.Request.SourceID, File: intent.Request.File}
}

// InspectRecording reads one recording directory without changing it:
// importable when its receipt records every track as untrusted intake,
// resumable when it holds its recording intent, and otherwise retained.
// Remaining is the recording's declared size, an upper bound.
func InspectRecording(dir string) PacketState {
	root, err := safeio.OpenRoot(dir)
	if err != nil {
		return retained("the recording directory cannot be opened")
	}
	defer root.Close()
	if _, err := root.Lstat(recordingReceiptName); err == nil {
		var receipt RecordingResult
		if err := readRecord(root, recordingReceiptName, &receipt); err != nil || !receipt.Complete || receipt.Status != "untrusted_intake" {
			return retained("its recording receipt is not completed untrusted intake")
		}
		return PacketState{State: PacketImportable, SourceID: receipt.SourceID, Recording: receipt.Format}
	} else if !errors.Is(err, os.ErrNotExist) {
		return retained("its recording receipt cannot be read")
	}
	var intent recordingIntent
	if err := readRecord(root, recordingIntentName, &intent); err != nil {
		return retained("it holds no recording intent from acquire")
	}
	for _, r := range intent.Selection.Recordings {
		if r.Format == intent.Request.Format {
			return PacketState{State: PacketResumable, Remaining: r.Bytes, SourceID: intent.Request.SourceID, Recording: intent.Request.Format}
		}
	}
	return retained("its recording intent names no declared recording")
}
