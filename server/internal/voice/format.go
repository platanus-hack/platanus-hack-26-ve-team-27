package voice

import "bytes"

// MaxAudioBytes is the largest audio file a voice is cloned from.
const MaxAudioBytes = 10 << 20

// Format is an accepted audio container.
type Format struct {
	Ext         string // file extension, without the dot
	ContentType string
}

var (
	formatMP3  = Format{"mp3", "audio/mpeg"}
	formatWAV  = Format{"wav", "audio/wav"}
	formatM4A  = Format{"m4a", "audio/mp4"}
	formatOGG  = Format{"ogg", "audio/ogg"}
	formatWebM = Format{"webm", "audio/webm"}
)

// Sniff names the format from the file's first bytes, not from its name or
// declared type, which the phone may get wrong. ok is false for anything but
// mp3, wav, m4a, ogg or webm.
func Sniff(b []byte) (f Format, ok bool) {
	switch {
	case bytes.HasPrefix(b, []byte("ID3")),
		len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0 && b[1]&0x06 != 0: // MPEG audio frame sync, layer set
		return formatMP3, true
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WAVE")):
		return formatWAV, true
	case len(b) >= 8 && bytes.Equal(b[4:8], []byte("ftyp")):
		return formatM4A, true
	case bytes.HasPrefix(b, []byte("OggS")):
		return formatOGG, true
	case bytes.HasPrefix(b, []byte{0x1A, 0x45, 0xDF, 0xA3}): // EBML: webm (and mkv)
		return formatWebM, true
	}
	return Format{}, false
}
