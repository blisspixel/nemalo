package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/blisspixel/nemalo/internal/assessment"
)

// ErrLegacyPendingImport preserves an older intent whose original metadata
// cannot be reconstructed safely from today's source.
var ErrLegacyPendingImport = errors.New("pending import predates provenance-bound recovery; state was retained; finish it with the earlier Nemalo executable and the original source before upgrading, or retain it for review")

// ErrImportEvidenceChanged means a resumable operation no longer gives its
// synced source evidence and holding proposal. Neither is silently replaced.
var ErrImportEvidenceChanged = errors.New("pending import evidence or proposed holding changed; the journal and stored bytes were preserved")

type importProposal struct {
	holding     Holding
	sourceHash  string
	holdingHash string
}

// importPlan binds normalized input evidence and the actual resulting holding,
// not raw receipt fields or a fresh-source disposition that apply might replace.
func importPlan(libraryID, source, sourceHash, holdingHash string, scan bool) string {
	h := sha256.New()
	for _, part := range []string{"nemalo-import-plan/2", libraryID, source, sourceHash, holdingHash, strconv.FormatBool(scan)} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalDigest hashes the same JSON field order as journal canonicalization,
// without a line feed. These DTOs contain no maps or generated timestamps.
func canonicalDigest(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// proposeImport is the single policy for preview and apply. The caller supplies
// current state under its reader/writer lock, projecting a pending rescan when
// previewing, or completing it before applying.
func proposeImport(result ImportResult, report assessment.Report, selected selected, request ImportRequest, current Holding, intent *operation) (ImportResult, importProposal, error) {
	var empty importProposal
	if request.Label != "" && sourceTooLong(request.Label) {
		return result, empty, errors.New("archive member label is outside the record limits")
	}
	result.Source = request.provenance(result.Source)
	live := request.Scan && intent == nil
	report, scanned := withReceiptScan(report, selected.receipt, live)
	result.Antivirus = report.Antivirus.Status
	status, reason, err := classify(report, scanned)
	if err != nil {
		return result, empty, err
	}
	if intent != nil {
		if result.AssetID != intent.AssetID || result.Bytes != intent.Bytes || result.Format != intent.Format {
			return result, empty, errors.New("pending import source changed; the journal was preserved")
		}
		status, reason = intent.Status, intent.Reason
		result.Limitations = append(result.Limitations, "Recovery retains the synced intent's disposition; it does not run another scan.")
	} else if current.ID == result.AssetID && !live && (!scanned || !(current.Status == "review" && rescannable[current.Reason])) {
		// An acquisition receipt is older evidence. It can fill an unscanned
		// holding, but never replace a detection or a later scan result.
		status, reason = current.Status, current.Reason
		result.Limitations = append(result.Limitations, "The proposed holding retains its existing disposition; this assessment adds no live scan.")
	}
	if !live && scanned {
		result.Limitations = append(result.Limitations, "Antivirus status comes from the acquisition receipt for these same bytes, not a new scan.")
	}
	result.Status, result.Reason = status, reason
	result.Path = assetRel(result.SHA256, result.Format)
	result.Titles, result.Languages = metaList(report), languageList(report)
	applyIdentities(&result, report)
	add := sourceRecord(selected)
	if request.Label != "" {
		add.Kind, add.Path = "archive_member", request.Label
	}
	holding := Holding{SchemaVersion: 1, LibraryID: result.LibraryID, ID: result.AssetID, SHA256: result.SHA256, Bytes: result.Bytes, Format: result.Format, Status: status, Reason: reason, Path: result.Path, Titles: result.Titles, Languages: result.Languages, IdentitiesRecorded: result.IdentitiesRecorded, IdentifiersOmitted: result.IdentifiersOmitted, Identities: result.Identities, Sources: mergeSources(current, add)}
	if !validHolding(holding, result.LibraryID) {
		return result, empty, errors.New("import record is outside the managed journal limits")
	}
	if _, err := recordBytes(holding); err != nil {
		return result, empty, err
	}
	sourceHash, err := canonicalDigest(add)
	if err != nil {
		return result, empty, err
	}
	holdingHash, err := canonicalDigest(holding)
	if err != nil {
		return result, empty, err
	}
	result.Plan, result.ScanRequested = importPlan(result.LibraryID, result.Source, sourceHash, holdingHash, request.Scan), request.Scan
	if request.Plan != "" && request.Plan != result.Plan {
		result.Plan = ""
		return result, empty, ErrPlanChanged
	}
	if intent != nil && (intent.ImportSourceSHA256 != sourceHash || intent.ImportHoldingSHA256 != holdingHash) {
		result.Plan = ""
		return result, empty, ErrImportEvidenceChanged
	}
	if note := omittedDeclared(selected.receipt); note != "" {
		result.Limitations = append(result.Limitations, note)
	}
	result.SourceEvidence, result.Holding = &add, &holding
	return result, importProposal{holding: holding, sourceHash: sourceHash, holdingHash: holdingHash}, nil
}

// A binding is optional only for legacy operation pairs. Both hashes must be
// present together on an import, never on a disposition-only rescan.
func validImportBinding(event operation) bool {
	if event.ImportSourceSHA256 == "" && event.ImportHoldingSHA256 == "" {
		return true
	}
	return (event.Event == "import_intent" || event.Event == "import_completed") && validHash(event.ImportSourceSHA256) && validHash(event.ImportHoldingSHA256)
}
