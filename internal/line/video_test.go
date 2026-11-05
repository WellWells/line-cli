package line

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func box(typ string, body ...[]byte) []byte {
	n := 8
	for _, b := range body {
		n += len(b)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(n))
	out = append(out, typ...)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

func mvhd0(scale, dur uint32) []byte {
	b := make([]byte, 20+80)
	binary.BigEndian.PutUint32(b[12:], scale)
	binary.BigEndian.PutUint32(b[16:], dur)
	return box("mvhd", b)
}

func mvhd1(scale uint32, dur uint64) []byte {
	b := make([]byte, 32+80)
	b[0] = 1
	binary.BigEndian.PutUint32(b[20:], scale)
	binary.BigEndian.PutUint64(b[24:], dur)
	return box("mvhd", b)
}

func TestMP4Duration(t *testing.T) {
	ftyp := box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2avc1mp41"))
	mdat := box("mdat", make([]byte, 1000))
	// A 64-bit sized mdat (size field 1, then the real size).
	big := append(binary.BigEndian.AppendUint32(nil, 1), "mdat"...)
	big = binary.BigEndian.AppendUint64(big, 16+10)
	big = append(big, make([]byte, 10)...)
	dir := t.TempDir()
	for _, c := range []struct {
		name string
		data []byte
		want int64
		err  string
	}{
		{"faststart", concat(ftyp, box("moov", mvhd0(1000, 6000), box("trak")), mdat), 6000, ""},
		{"moov last", concat(ftyp, mdat, box("moov", box("udta"), mvhd0(90000, 900000))), 10000, ""},
		{"version 1", concat(ftyp, box("moov", mvhd1(600, 600*125))), 125000, ""},
		{"64-bit box", concat(ftyp, big, box("moov", mvhd0(30, 45))), 1500, ""},
		{"not mp4", concat(mdat, box("moov", mvhd0(1000, 6000))), 0, "not a readable MP4"},
		{"no moov", concat(ftyp, mdat), 0, "not a readable MP4"},
		{"zero timescale", concat(ftyp, box("moov", mvhd0(0, 6000))), 0, "how long"},
		{"unknown duration", concat(ftyp, box("moov", mvhd0(1000, 1<<32-1))), 0, "how long"},
		{"truncated", concat(ftyp, box("moov", mvhd0(1000, 6000)))[:40], 0, "not a readable MP4"},
		{"junk", []byte("definitely not a video"), 0, "not a readable MP4"},
	} {
		p := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "_")+".mp4")
		if err := os.WriteFile(p, c.data, 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := readMP4(p)
		got := info.durationMs
		switch {
		case c.err == "" && (err != nil || got != c.want):
			t.Errorf("%s: got %d, %v; want %d", c.name, got, err, c.want)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s: err %v, want %q", c.name, err, c.err)
		}
	}
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// trak is a track box of the given handler (vide, soun) whose first sample
// entry is entry (avc1, hvc1, mp4a ...).
func trak(handler, entry string) []byte {
	hdlr := box("hdlr", make([]byte, 8), []byte(handler), make([]byte, 13))
	stsd := box("stsd", []byte{0, 0, 0, 0, 0, 0, 0, 1}, box(entry, make([]byte, 70)))
	return box("trak", box("tkhd", make([]byte, 84)), box("mdia", box("mdhd", make([]byte, 24)), hdlr, box("minf", box("stbl", stsd))))
}

func TestMP4Codecs(t *testing.T) {
	ftyp := box("ftyp", []byte("isom\x00\x00\x02\x00isom"))
	dir := t.TempDir()
	for _, c := range []struct {
		name         string
		moov         []byte
		video, audio string
		refusal      string // what playable says, or "" when it is sent as a video
	}{
		{"h264 aac", box("moov", mvhd0(1000, 6000), trak("vide", "avc1"), trak("soun", "mp4a")), "avc1", "mp4a", ""},
		{"h264 silent", box("moov", mvhd0(1000, 6000), trak("vide", "avc3")), "avc3", "", ""},
		{"sound first", box("moov", mvhd0(1000, 6000), trak("soun", "mp4a"), trak("vide", "avc1")), "avc1", "mp4a", ""},
		{"hevc", box("moov", mvhd0(1000, 6000), trak("vide", "hvc1"), trak("soun", "mp4a")), "hvc1", "mp4a", "its video is HEVC (H.265), not H.264"},
		{"av1", box("moov", mvhd0(1000, 6000), trak("vide", "av01")), "av01", "", "its video is AV1"},
		{"h264 opus", box("moov", mvhd0(1000, 6000), trak("vide", "avc1"), trak("soun", "Opus")), "avc1", "Opus", "its sound is Opus, not AAC"},
		{"sound only", box("moov", mvhd0(1000, 6000), trak("soun", "mp4a")), "", "mp4a", "it has no video track"},
		{"odd track", box("moov", mvhd0(1000, 6000), box("trak", box("mdia")), trak("vide", "avc1")), "avc1", "", ""},
	} {
		p := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "_")+".mp4")
		os.WriteFile(p, concat(ftyp, c.moov), 0o600)
		info, err := readMP4(p)
		if err != nil || info.video != c.video || info.audio != c.audio || info.durationMs != 6000 {
			t.Errorf("%s: %+v %v", c.name, info, err)
			continue
		}
		err = info.playable()
		if (c.refusal == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.refusal)) {
			t.Errorf("%s: playable() = %v, want %q", c.name, err, c.refusal)
		}
	}
}
