package inventory

import (
	"bytes"
	"context"
	"crypto/md5" // PAR2 packet checksum only, never a security identity.
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	par2FileBytes    = 1 << 20
	par2TotalBytes   = 16 << 20
	par2FilePackets  = 256
	par2Packets      = 4096
	par2Sets         = 128
	par2MainIDs      = 1024
	par2NameBytes    = 1024
	par2Magic        = "PAR2\x00PKT"
	par2MainType     = "PAR 2.0\x00Main\x00\x00\x00\x00"
	par2DescType     = "PAR 2.0\x00FileDesc"
	par2CreatorType  = "PAR 2.0\x00Creator\x00"
	par2RecoveryType = "PAR 2.0\x00RecvSlic"
	par2InputType    = "PAR 2.0\x00IFSC\x00\x00\x00\x00"
)

var errPAR2Limit = errors.New("PAR2 metadata work limit reached")

// PAR2Report inventories declarations, never the condition of protected data.
type PAR2Report struct {
	SchemaVersion  int       `json:"schema_version"`
	Scope          string    `json:"scope"`
	Complete       bool      `json:"complete"`
	ProtectedData  string    `json:"protected_data"`
	Recoverability string    `json:"recoverability"`
	FilesProbed    int       `json:"file_probe_attempts"`
	Packets        int       `json:"packet_attempts"`
	BytesRead      int64     `json:"bytes_read"`
	Sets           []PAR2Set `json:"sets"`
	Limitations    []string  `json:"limitations"`
}

type PAR2Finding struct {
	Offset int64  `json:"offset"`
	Detail string `json:"detail"`
}

type PAR2File struct {
	State            string        `json:"state"`
	ProbeComplete    bool          `json:"signature_probe_complete"`
	HeadersComplete  bool          `json:"headers_complete"`
	MetadataComplete bool          `json:"metadata_complete"`
	Packets          int           `json:"packet_attempts"`
	Headers          int           `json:"packet_headers"`
	ParityHeaders    int           `json:"recovery_slice_headers"`
	InputHeaders     int           `json:"input_checksum_headers"`
	UnknownHeaders   int           `json:"unknown_packet_headers"`
	Matching         int           `json:"matching_packet_checksums"`
	Mismatched       int           `json:"mismatched_packet_checksums"`
	Unchecked        int           `json:"unchecked_packet_checksums"`
	SetIDs           []string      `json:"declared_set_ids"`
	Findings         []PAR2Finding `json:"findings"`
}

type PAR2Location struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

type PAR2Main struct {
	SliceBytes       uint64         `json:"declared_slice_bytes"`
	RecoverableFiles uint32         `json:"declared_recoverable_files"`
	FileIDs          []string       `json:"declared_file_ids"`
	Locations        []PAR2Location `json:"locations"`
}

type PAR2Description struct {
	FileID    string         `json:"declared_file_id"`
	MD5       string         `json:"declared_md5"`
	MD5First  string         `json:"declared_md5_first_16k"`
	Bytes     uint64         `json:"declared_bytes"`
	Name      string         `json:"declared_name,omitempty"`
	NameHex   string         `json:"declared_name_hex,omitempty"`
	Locations []PAR2Location `json:"locations"`
}

type PAR2Creator struct {
	Text      string         `json:"declared_text,omitempty"`
	TextHex   string         `json:"declared_text_hex,omitempty"`
	Locations []PAR2Location `json:"locations"`
}

type PAR2Set struct {
	ID             string            `json:"declared_set_id"`
	Files          []string          `json:"observed_files"`
	Packets        int               `json:"packet_headers"`
	ParityHeaders  int               `json:"recovery_slice_headers"`
	InputHeaders   int               `json:"input_checksum_headers"`
	UnknownHeaders int               `json:"unknown_packet_headers"`
	Matching       int               `json:"matching_packet_checksums"`
	Mismatched     int               `json:"mismatched_packet_checksums"`
	Unchecked      int               `json:"unchecked_packet_checksums"`
	Mains          []PAR2Main        `json:"main_declarations"`
	Descriptions   []PAR2Description `json:"file_declarations"`
	Creators       []PAR2Creator     `json:"creator_declarations"`
	Conflicts      []string          `json:"conflicts"`
}

type par2Observation struct {
	set, kind, checksum string
	offset              int64
	body                []byte
}

type par2Run struct {
	report  *PAR2Report
	ids     map[string]bool
	groups  map[string]*par2Group
	stopped bool
}

func newPAR2Run() *par2Run {
	return &par2Run{ids: map[string]bool{}, groups: map[string]*par2Group{}, report: &PAR2Report{SchemaVersion: 1,
		Scope: "packet_metadata_only", Complete: true, ProtectedData: "not_verified", Recoverability: "not_assessed",
		Sets: []PAR2Set{}, Limitations: []string{
			"Set IDs and filenames are declarations. Protected files are never opened or verified, and recovery capacity is not assessed.",
			"Only bounded main, file-description, and creator packet bodies are checksummed. Parity, input-slice-checksum, and unknown bodies stay unread and unchecked.",
			"Header traversal starts at byte zero and does not resynchronize damaged data. Missing files or recovery completeness cannot be inferred.",
		}}}
}

func par2Unknown(detail string) *PAR2File {
	return &PAR2File{State: "not_inspected", SetIDs: []string{}, Findings: []PAR2Finding{{Detail: detail}}}
}

// inspectPAR2 reads through the inventory reader, whose actual I/O counters
// include every successful and failed metadata read across the whole run.
func inspectPAR2(ctx context.Context, r *fileReader, size int64, candidate bool) (*PAR2File, []par2Observation, error) {
	if r.par2.stopped {
		return par2Unknown("run_limit"), nil, errPAR2Limit
	}
	r.par2.report.FilesProbed++
	var probe [8]byte
	n := int(min(int64(len(probe)), size))
	if _, err := r.readAt(probe[:n], 0, true); err != nil {
		return par2Unknown(err.Error()), nil, err
	}
	if n != 8 || string(probe[:]) != par2Magic {
		if candidate && n > 0 && n < 8 && bytes.Equal(probe[:n], []byte(par2Magic)[:n]) {
			return par2Unknown("truncated_signature"), nil, errors.New("truncated PAR2 signature")
		}
		if candidate {
			m := par2Unknown("filename_candidate_only")
			m.State, m.ProbeComplete, m.MetadataComplete = "signature_not_matched", true, true
			return m, nil, nil
		}
		return nil, nil, nil
	}
	m := &PAR2File{State: "headers_read", ProbeComplete: true, MetadataComplete: true, SetIDs: []string{}, Findings: []PAR2Finding{}}
	var observations []par2Observation
	record := func(o par2Observation) {
		switch o.checksum {
		case "matching":
			m.Matching++
		case "mismatched":
			m.Mismatched++
		default:
			m.Unchecked++
		}
		observations = append(observations, o)
	}
	fail := func(off int64, detail string, err error) (*PAR2File, []par2Observation, error) {
		m.State, m.MetadataComplete = "partial", false
		m.Findings = append(m.Findings, PAR2Finding{off, detail})
		return m, observations, err
	}
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return fail(off, err.Error(), err)
		}
		if m.Packets >= par2FilePackets || r.par2.report.Packets >= par2Packets {
			r.par2.stopped = r.par2.report.Packets >= par2Packets
			return fail(off, "packet_limit", errPAR2Limit)
		}
		m.Packets++
		r.par2.report.Packets++
		var h [64]byte
		if size-off < int64(len(h)) {
			return fail(off, "truncated_header", errors.New("truncated PAR2 packet header"))
		}
		start := 0
		if off == 0 {
			copy(h[:8], probe[:])
			start = 8
		}
		if _, err := r.readAt(h[start:], off+int64(start), true); err != nil {
			return fail(off, err.Error(), err)
		}
		length := binary.LittleEndian.Uint64(h[8:16])
		if string(h[:8]) != par2Magic || length < 64 || length%4 != 0 || length > uint64(size-off) {
			return fail(off, "invalid_or_truncated_extent", errors.New("invalid or truncated PAR2 packet extent"))
		}
		set := hex.EncodeToString(h[32:48])
		if !r.par2.ids[set] {
			if len(r.par2.ids) >= par2Sets {
				r.par2.stopped = true
				return fail(off, "set_limit", errPAR2Limit)
			}
			r.par2.ids[set] = true
		}
		if !containsString(m.SetIDs, set) {
			m.SetIDs = append(m.SetIDs, set)
		}
		m.Headers++
		o := par2Observation{set: set, kind: string(h[48:64]), offset: off, checksum: "unchecked"}
		switch o.kind {
		case par2RecoveryType:
			m.ParityHeaders++
		case par2InputType:
			m.InputHeaders++
		case par2MainType, par2DescType, par2CreatorType:
		default:
			m.UnknownHeaders++
		}
		bodyBytes := length - 64
		maxBody := uint64(0)
		switch o.kind {
		case par2MainType:
			maxBody = 12 + 16*par2MainIDs
		case par2DescType:
			maxBody = 56 + par2NameBytes + 3
		case par2CreatorType:
			maxBody = par2NameBytes
		}
		if maxBody > 0 && bodyBytes > maxBody {
			m.MetadataComplete = false
			m.Findings = append(m.Findings, PAR2Finding{off, "metadata_body_limit"})
		} else if maxBody > 0 {
			body := make([]byte, int(bodyBytes))
			if _, err := r.readAt(body, off+64, true); err != nil {
				record(o)
				return fail(off, err.Error(), err)
			}
			sum := md5.New()
			_, _ = sum.Write(h[32:])
			_, _ = sum.Write(body)
			if bytes.Equal(sum.Sum(nil), h[16:32]) {
				o.checksum, o.body = "matching", body
			} else {
				o.checksum = "mismatched"
				m.Findings = append(m.Findings, PAR2Finding{off, "packet_checksum_mismatch"})
			}
		} else if o.kind != par2RecoveryType && o.kind != par2InputType {
			m.Findings = append(m.Findings, PAR2Finding{off, "unchecked_packet_type:" + hex.EncodeToString(h[48:64])})
		}
		record(o)
		off += int64(length) // length is bounded by size-off above.
	}
	m.HeadersComplete = true
	if !m.MetadataComplete {
		return m, observations, errPAR2Limit
	}
	return m, observations, nil
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (m *PAR2File) completed() bool {
	return m.ProbeComplete && (m.State == "signature_not_matched" || m.HeadersComplete && m.MetadataComplete)
}

func metadataText(body []byte) (text, opaque string) {
	if utf8.Valid(body) {
		return string(body), ""
	}
	return "", hex.EncodeToString(body)
}

func mainDeclaration(body []byte) (PAR2Main, error) {
	if len(body) < 12 || (len(body)-12)%16 != 0 {
		return PAR2Main{}, errors.New("invalid_main_packet")
	}
	m := PAR2Main{SliceBytes: binary.LittleEndian.Uint64(body), RecoverableFiles: binary.LittleEndian.Uint32(body[8:]), FileIDs: []string{}}
	if m.SliceBytes == 0 || m.SliceBytes%4 != 0 || uint64(m.RecoverableFiles) > uint64((len(body)-12)/16) {
		return PAR2Main{}, errors.New("invalid_main_packet")
	}
	seen := map[string]bool{}
	for i := 12; i < len(body); i += 16 {
		id := hex.EncodeToString(body[i : i+16])
		if seen[id] {
			return PAR2Main{}, errors.New("duplicate_main_file_id")
		}
		seen[id] = true
		m.FileIDs = append(m.FileIDs, id)
	}
	return m, nil
}

func fileDeclaration(body []byte) (PAR2Description, error) {
	if len(body) < 60 {
		return PAR2Description{}, errors.New("invalid_file_description")
	}
	name := bytes.TrimRight(body[56:], "\x00")
	if len(body)-56-len(name) > 3 || len(name) == 0 || len(name) > par2NameBytes || bytes.IndexByte(name, 0) >= 0 {
		return PAR2Description{}, errors.New("invalid_declared_filename")
	}
	d := PAR2Description{FileID: hex.EncodeToString(body[:16]), MD5: hex.EncodeToString(body[16:32]),
		MD5First: hex.EncodeToString(body[32:48]), Bytes: binary.LittleEndian.Uint64(body[48:56])}
	d.Name, d.NameHex = metadataText(name)
	return d, nil
}

func par2Problem(m *PAR2File, off int64, err error) {
	m.Findings = append(m.Findings, PAR2Finding{off, fmt.Sprint(err)})
}

// No declared filename is resolved or opened here, including strings that
// name absolute paths, parent components, URLs, or platform-specific devices.
func creatorDeclaration(body []byte) PAR2Creator {
	t, h := metadataText([]byte(strings.TrimRight(string(body), "\x00")))
	return PAR2Creator{Text: t, TextHex: h}
}
