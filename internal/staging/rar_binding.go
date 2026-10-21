package staging

type rarBindingFile struct {
	Name              string
	Bytes, Packed     int64
	CRC32             uint32
	HasCRC, Directory bool
}

func (l Limits) rarCoverage(kind string) bool {
	return l.Coverage && (kind == "Rar5" || (kind == "Rar" && l.RAR4Coverage))
}

func rarCoverageError(kind string) error {
	if kind == "Rar" {
		return ErrRAR4Coverage
	}
	return ErrRAR5Coverage
}

func rar5BindingFiles(files []RAR5File) []rarBindingFile {
	if files == nil {
		return nil
	}
	facts := make([]rarBindingFile, len(files))
	for i, f := range files {
		facts[i] = rarBindingFile{Name: f.Name, Bytes: f.Bytes, Packed: f.Packed, CRC32: f.CRC32, HasCRC: f.HasCRC, Directory: f.Directory}
	}
	return facts
}

func rar4BindingFiles(files []RAR4File) []rarBindingFile {
	if files == nil {
		return nil
	}
	facts := make([]rarBindingFile, len(files))
	for i, f := range files {
		facts[i] = rarBindingFile{Name: f.Name, Bytes: f.Bytes, Packed: f.Packed, CRC32: f.CRC32, HasCRC: true, Directory: f.Directory}
	}
	return facts
}

// Bind the complete physical record set to the table that drives exact-name
// extraction. Neither reader ordering nor normalized aliases grant authority.
func rarTableMatches(files []rarBindingFile, entries []sevenEntry) bool {
	if len(files) != len(entries) {
		return false
	}
	byName := make(map[string]rarBindingFile, len(files))
	for _, file := range files {
		if _, exists := byName[file.Name]; exists {
			return false
		}
		byName[file.Name] = file
	}
	for _, e := range entries {
		file, exists := byName[e.raw]
		if !exists || e.name != file.Name || !e.valid || e.malformed || e.encrypted || e.link || e.special || e.split || e.splitBefore || e.splitAfter ||
			!e.sizeKnown || !e.packedKnown || file.Bytes != e.size || file.Packed != e.packed || file.Directory != e.folder ||
			(!file.Directory && (!file.HasCRC || !e.crcKnown || file.CRC32 != e.crc)) || (file.Directory && e.crcKnown && e.crc != 0) {
			return false
		}
		delete(byName, e.raw)
	}
	return len(byName) == 0
}
