// Package probe identifies audio files by their real codec, never the extension
// (D-8), and measures their duration, in pure Go so installed apps need no external
// tools. Supported: MP3, AAC (ADTS or in MP4/M4A), FLAC and WAV (PCM or float).
// ALAC, OGG/Opus, WMA and AIFF are rejected. Tags are read for the import name and
// artist hint (§7.1); a tag parse failure means "no embedded value" (F-12).
package probe

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// Errors map to the server's error codes (R-LIM-4).
var (
	ErrUnsupported = errors.New("unsupported_format")
	ErrInvalid     = errors.New("invalid_audio_metadata")
)

type Facts struct {
	Format     string // "mp3" | "aac" | "flac" | "wav"
	DurationMs int64
	Bytes      int64
	SHA256     string
	Title      string
	Artist     string
}

// maxTagBytes bounds how much of a file the tag reader may consume (F-12).
const maxTagBytes = 16 << 20

// File probes path: format, duration, exact size, SHA-256 and tags.
func File(path string) (*Facts, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	facts, err := Reader(f, st.Size())
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	sum, n, err := Hash(f)
	if err != nil {
		return nil, err
	}
	facts.SHA256, facts.Bytes = sum, n
	return facts, nil
}

// Hash returns the SHA-256 over the complete stream and its exact length (R-ID-6).
func Hash(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, bufio.NewReaderSize(r, 1<<20))
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// Reader identifies the format and duration of r (size bytes) and reads its tags.
func Reader(r io.ReadSeeker, size int64) (*Facts, error) {
	head := make([]byte, 12)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, ErrInvalid
	}
	var facts *Facts
	var err error
	switch {
	case string(head[0:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		facts, err = probeWAV(r)
	case string(head[0:4]) == "fLaC":
		facts, err = probeFLAC(r)
	case string(head[4:8]) == "ftyp":
		facts, err = probeMP4(r, size)
	case string(head[0:4]) == "OggS", string(head[0:4]) == "FORM", string(head[0:4]) == "\x30\x26\xb2\x75":
		return nil, ErrUnsupported
	default:
		facts, err = probeMPEG(r, size)
	}
	if err != nil {
		return nil, err
	}
	if facts.DurationMs <= 0 {
		return nil, ErrInvalid
	}
	readTags(r, facts)
	return facts, nil
}

func readTags(r io.ReadSeeker, facts *Facts) {
	defer func() { _ = recover() }() // a hostile tag must never crash the importer
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return
	}
	m, err := tag.ReadFrom(&limitedSeeker{r: r, max: maxTagBytes})
	if err != nil {
		return
	}
	facts.Title = strings.TrimSpace(m.Title())
	facts.Artist = strings.TrimSpace(m.Artist())
}

type limitedSeeker struct {
	r   io.ReadSeeker
	max int64
	pos int64
}

func (l *limitedSeeker) Read(p []byte) (int, error) {
	if l.pos >= l.max {
		return 0, io.EOF
	}
	if int64(len(p)) > l.max-l.pos {
		p = p[:l.max-l.pos]
	}
	n, err := l.r.Read(p)
	l.pos += int64(n)
	return n, err
}

func (l *limitedSeeker) Seek(off int64, whence int) (int64, error) {
	n, err := l.r.Seek(off, whence)
	l.pos = n
	return n, err
}

// WAV ---------------------------------------------------------------------------

func probeWAV(r io.ReadSeeker) (*Facts, error) {
	if _, err := r.Seek(12, io.SeekStart); err != nil {
		return nil, ErrInvalid
	}
	var byteRate uint32
	fmtOK := false
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return nil, ErrInvalid
		}
		id := string(hdr[0:4])
		n := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		switch id {
		case "fmt ":
			if n < 16 {
				return nil, ErrInvalid
			}
			body := make([]byte, n)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, ErrInvalid
			}
			format := binary.LittleEndian.Uint16(body[0:2])
			if format == 0xFFFE && n >= 26 { // WAVE_FORMAT_EXTENSIBLE: the sub-format decides
				format = binary.LittleEndian.Uint16(body[24:26])
			}
			if format != 1 && format != 3 { // PCM or IEEE float only
				return nil, ErrUnsupported
			}
			byteRate = binary.LittleEndian.Uint32(body[8:12])
			fmtOK = true
			if n%2 == 1 {
				_, _ = r.Seek(1, io.SeekCurrent)
			}
		case "data":
			if !fmtOK || byteRate == 0 {
				return nil, ErrInvalid
			}
			return &Facts{Format: "wav", DurationMs: n * 1000 / int64(byteRate)}, nil
		default:
			if _, err := r.Seek(n+n%2, io.SeekCurrent); err != nil {
				return nil, ErrInvalid
			}
		}
	}
}

// FLAC --------------------------------------------------------------------------

func probeFLAC(r io.ReadSeeker) (*Facts, error) {
	if _, err := r.Seek(4, io.SeekStart); err != nil {
		return nil, ErrInvalid
	}
	block := make([]byte, 4+34)
	if _, err := io.ReadFull(r, block); err != nil || block[0]&0x7F != 0 { // first block must be STREAMINFO
		return nil, ErrInvalid
	}
	si := block[4:]
	sampleRate := uint64(si[10])<<12 | uint64(si[11])<<4 | uint64(si[12])>>4
	total := uint64(si[13]&0x0F)<<32 | uint64(binary.BigEndian.Uint32(si[14:18]))
	if sampleRate == 0 || total == 0 {
		return nil, ErrInvalid
	}
	return &Facts{Format: "flac", DurationMs: int64(total * 1000 / sampleRate)}, nil
}

// MP4 / M4A ---------------------------------------------------------------------

type atom struct {
	typ        string
	start, end int64 // payload range
}

func readAtom(r io.ReadSeeker, pos, limit int64) (atom, error) {
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return atom{}, err
	}
	h := make([]byte, 8)
	if _, err := io.ReadFull(r, h); err != nil {
		return atom{}, err
	}
	size := int64(binary.BigEndian.Uint32(h[0:4]))
	start := pos + 8
	switch size {
	case 1:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return atom{}, err
		}
		size = int64(binary.BigEndian.Uint64(ext))
		start = pos + 16
	case 0:
		size = limit - pos
	}
	if size < start-pos || pos+size > limit {
		return atom{}, ErrInvalid
	}
	return atom{typ: string(h[4:8]), start: start, end: pos + size}, nil
}

func children(r io.ReadSeeker, parent atom, fn func(a atom) (stop bool, err error)) error {
	for pos := parent.start; pos+8 <= parent.end; {
		a, err := readAtom(r, pos, parent.end)
		if err != nil {
			return err
		}
		stop, err := fn(a)
		if err != nil || stop {
			return err
		}
		pos = a.end
	}
	return nil
}

func find(r io.ReadSeeker, parent atom, typ string) (atom, bool, error) {
	var out atom
	found := false
	err := children(r, parent, func(a atom) (bool, error) {
		if a.typ == typ {
			out, found = a, true
			return true, nil
		}
		return false, nil
	})
	return out, found, err
}

func readAt(r io.ReadSeeker, pos int64, n int) ([]byte, error) {
	if _, err := r.Seek(pos, io.SeekStart); err != nil {
		return nil, err
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func probeMP4(r io.ReadSeeker, size int64) (*Facts, error) {
	root := atom{start: 0, end: size}
	moov, ok, err := find(r, root, "moov")
	if err != nil || !ok {
		return nil, ErrInvalid
	}
	var facts *Facts
	err = children(r, moov, func(trak atom) (bool, error) {
		if trak.typ != "trak" {
			return false, nil
		}
		mdia, ok, err := find(r, trak, "mdia")
		if err != nil || !ok {
			return false, err
		}
		hdlr, ok, err := find(r, mdia, "hdlr")
		if err != nil || !ok {
			return false, err
		}
		h, err := readAt(r, hdlr.start+8, 4)
		if err != nil || string(h) != "soun" {
			return false, err
		}
		mdhd, ok, err := find(r, mdia, "mdhd")
		if err != nil || !ok {
			return false, ErrInvalid
		}
		ver, err := readAt(r, mdhd.start, 1)
		if err != nil {
			return false, err
		}
		var timescale, duration uint64
		if ver[0] == 1 {
			b, err := readAt(r, mdhd.start+20, 12)
			if err != nil {
				return false, err
			}
			timescale, duration = uint64(binary.BigEndian.Uint32(b[0:4])), binary.BigEndian.Uint64(b[4:12])
		} else {
			b, err := readAt(r, mdhd.start+12, 8)
			if err != nil {
				return false, err
			}
			timescale, duration = uint64(binary.BigEndian.Uint32(b[0:4])), uint64(binary.BigEndian.Uint32(b[4:8]))
		}
		codec, err := mp4Codec(r, mdia)
		if err != nil {
			return true, err
		}
		if timescale == 0 {
			return true, ErrInvalid
		}
		facts = &Facts{Format: codec, DurationMs: int64(duration * 1000 / timescale)}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if facts == nil {
		return nil, ErrInvalid
	}
	return facts, nil
}

// mp4Codec reads the first sample description: only AAC ("mp4a" with an AAC object
// type) is supported; ALAC and everything else are rejected.
func mp4Codec(r io.ReadSeeker, mdia atom) (string, error) {
	minf, ok, err := find(r, mdia, "minf")
	if err != nil || !ok {
		return "", ErrInvalid
	}
	stbl, ok, err := find(r, minf, "stbl")
	if err != nil || !ok {
		return "", ErrInvalid
	}
	stsd, ok, err := find(r, stbl, "stsd")
	if err != nil || !ok {
		return "", ErrInvalid
	}
	entry, err := readAtom(r, stsd.start+8, stsd.end)
	if err != nil {
		return "", ErrInvalid
	}
	if entry.typ != "mp4a" {
		return "", ErrUnsupported
	}
	// AudioSampleEntry is 28 bytes, then child boxes (esds).
	esds, ok, err := find(r, atom{start: entry.start + 28, end: entry.end}, "esds")
	if err != nil || !ok {
		return "aac", nil // mp4a without esds: treat as AAC, the server verifies
	}
	b, err := readAt(r, esds.start, int(min(esds.end-esds.start, 64)))
	if err != nil {
		return "", ErrInvalid
	}
	// Find DecoderConfigDescriptor (tag 0x04); the next byte after its length is the
	// object type: 0x40 (MPEG-4 audio) or 0x66–0x68 (MPEG-2 AAC).
	for i := 4; i < len(b)-1; i++ {
		if b[i] == 0x04 {
			j := i + 1
			for j < len(b) && b[j]&0x80 != 0 {
				j++
			}
			j++
			if j < len(b) {
				switch b[j] {
				case 0x40, 0x66, 0x67, 0x68:
					return "aac", nil
				default:
					return "", ErrUnsupported
				}
			}
		}
	}
	return "aac", nil
}

// MPEG audio (MP3) and ADTS AAC -----------------------------------------------

var (
	mp3Bitrates = [2][16]int{ // kbps, Layer III: [MPEG-1, MPEG-2/2.5]
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	}
	mp3Rates  = [3][3]int{{44100, 48000, 32000}, {22050, 24000, 16000}, {11025, 12000, 8000}}
	adtsRates = [16]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
)

func probeMPEG(r io.ReadSeeker, size int64) (*Facts, error) {
	start := int64(0)
	h, err := readAt(r, 0, 10)
	if err != nil {
		return nil, ErrInvalid
	}
	if string(h[0:3]) == "ID3" {
		tagSize := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
		start = 10 + tagSize
		if h[5]&0x10 != 0 {
			start += 10 // footer
		}
	}
	// Find the first frame sync within a bounded window.
	buf, _ := readAt(r, start, int(min(size-start, 64<<10)))
	if len(buf) < 4 {
		return nil, ErrInvalid
	}
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		layer := (buf[i+1] >> 1) & 0x03
		if layer == 0 && buf[i+1]&0xF6 == 0xF0 {
			if adtsAt(buf, i) {
				return probeADTS(r, start+int64(i), size)
			}
			continue
		}
		if layer != 1 {
			// A well-formed Layer I or II header (MP1/MP2) is unsupported, not a
			// reason to keep scanning into the audio data for a false sync.
			if (buf[i+1]>>3)&0x03 != 1 && buf[i+2]>>4 != 0 && buf[i+2]>>4 != 15 && (buf[i+2]>>2)&0x03 != 3 {
				return nil, ErrUnsupported
			}
			continue
		}
		if f, ok := mp3Frame(r, buf, i, start, size); ok {
			return f, nil
		}
	}
	return nil, ErrUnsupported
}

func mp3Frame(r io.ReadSeeker, buf []byte, i int, base, size int64) (*Facts, bool) {
	verBits := (buf[i+1] >> 3) & 0x03 // 3 = MPEG-1, 2 = MPEG-2, 0 = MPEG-2.5
	if verBits == 1 {
		return nil, false
	}
	brIdx := buf[i+2] >> 4
	srIdx := (buf[i+2] >> 2) & 0x03
	if brIdx == 0 || brIdx == 15 || srIdx == 3 {
		return nil, false
	}
	v := map[byte]int{3: 0, 2: 1, 0: 2}[verBits]
	rate := mp3Rates[v][srIdx]
	bitrate := mp3Bitrates[min(v, 1)][brIdx] * 1000
	samplesPerFrame := 1152
	if v != 0 {
		samplesPerFrame = 576
	}
	mono := buf[i+3]>>6 == 3
	// The Xing/Info or VBRI header in the first frame gives the exact frame count.
	side := 32
	switch {
	case v == 0 && mono:
		side = 17
	case v != 0 && !mono:
		side = 17
	case v != 0 && mono:
		side = 9
	}
	frames := int64(0)
	if x := i + 4 + side; x+12 <= len(buf) && (string(buf[x:x+4]) == "Xing" || string(buf[x:x+4]) == "Info") {
		if buf[x+7]&0x01 != 0 {
			frames = int64(binary.BigEndian.Uint32(buf[x+8 : x+12]))
		}
	} else if x := i + 4 + 32; x+18 <= len(buf) && string(buf[x:x+4]) == "VBRI" {
		frames = int64(binary.BigEndian.Uint32(buf[x+14 : x+18]))
	}
	if frames > 0 {
		return &Facts{Format: "mp3", DurationMs: frames * int64(samplesPerFrame) * 1000 / int64(rate)}, true
	}
	// Constant bit rate: audio bytes over the bit rate, minus an ID3v1 tag.
	audio := size - (base + int64(i))
	if t, err := readAt(r, size-128, 3); err == nil && string(t) == "TAG" {
		audio -= 128
	}
	if audio <= 0 || bitrate == 0 {
		return nil, false
	}
	return &Facts{Format: "mp3", DurationMs: audio * 8 * 1000 / int64(bitrate)}, true
}

// adtsAt reports whether two consecutive ADTS frame headers start at buf[i].
func adtsAt(buf []byte, i int) bool {
	if i+7 > len(buf) {
		return false
	}
	length := int(buf[i+3]&0x03)<<11 | int(buf[i+4])<<3 | int(buf[i+5])>>5
	j := i + length
	if length < 7 || (buf[i+2]>>2)&0x0F > 12 {
		return false
	}
	if j+2 > len(buf) {
		return j >= len(buf) // a single-frame file
	}
	return buf[j] == 0xFF && buf[j+1]&0xF6 == 0xF0
}

// probeADTS counts ADTS frames (1024 samples each) through the file.
func probeADTS(r io.ReadSeeker, pos, size int64) (*Facts, error) {
	var frames int64
	rate := 0
	h := make([]byte, 7)
	for pos+7 <= size {
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			break
		}
		if _, err := io.ReadFull(r, h); err != nil {
			break
		}
		if h[0] != 0xFF || h[1]&0xF6 != 0xF0 {
			break
		}
		if rate == 0 {
			rate = adtsRates[(h[2]>>2)&0x0F]
		}
		length := int64(h[3]&0x03)<<11 | int64(h[4])<<3 | int64(h[5])>>5
		if length < 7 {
			break
		}
		frames++
		pos += length
	}
	if frames == 0 || rate == 0 {
		return nil, ErrInvalid
	}
	return &Facts{Format: "aac", DurationMs: frames * 1024 * 1000 / int64(rate)}, nil
}
