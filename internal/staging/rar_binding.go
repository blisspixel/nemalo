package staging

// Bind the complete physical record set to the table that drives exact-name
// extraction. Neither reader ordering nor normalized aliases grant authority.
func rar5TableMatches(files []RAR5File, entries []sevenEntry) bool {
	if len(files) != len(entries) {
		return false
	}
	byName := make(map[string]RAR5File, len(files))
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
