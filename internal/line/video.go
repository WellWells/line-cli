package line

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"time"
)

// videoExt reports whether a file extension is sent as a video message. LINE
// plays MP4 in its chats; other formats (MOV, MKV, AVI ...) go as files.
func videoExt(ext string) bool { return ext == "mp4" || ext == "m4v" }

// maxMoovBytes bounds the metadata box read to find a video's duration.
const maxMoovBytes = 64 << 20

type video struct {
	durationMs int64
	preview    []byte
}

// makeVideo checks that an MP4 is H.264 video with AAC sound (or none), reads
// its duration and makes its JPEG preview from the frame Windows shows for it.
// LINE's apps convert every video to H.264 MP4 before they send it, so its
// players are only sure to play that.
func makeVideo(path string) (video, error) {
	info, err := readMP4(path)
	if err != nil {
		return video{}, err
	}
	if err := info.playable(); err != nil {
		return video{}, err
	}
	frame, err := videoFrame(path, previewMaxSide, 30*time.Second)
	if err != nil {
		return video{}, err
	}
	small := downscale(frame, previewMaxSide)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, &jpeg.Options{Quality: 85}); err != nil {
		return video{}, err
	}
	return video{durationMs: info.durationMs, preview: out.Bytes()}, nil
}

var errNotMP4 = errors.New("not a readable MP4 file")

// mp4Info is what line-cli reads from an MP4 (ISO base media) file.
type mp4Info struct {
	durationMs int64
	video      string // sample entry of the first video track: avc1, hvc1 ...
	audio      string // of the first sound track: mp4a, Opus ...
}

func (m mp4Info) playable() error {
	switch m.video {
	case "avc1", "avc3":
	case "":
		return errors.New("it has no video track")
	default:
		return fmt.Errorf("its video is %s, not H.264, which LINE's apps convert videos to before they send them", codecName(m.video))
	}
	if m.audio != "" && m.audio != "mp4a" {
		return fmt.Errorf("its sound is %s, not AAC, which LINE's apps convert videos to before they send them", codecName(m.audio))
	}
	return nil
}

func codecName(fourcc string) string {
	switch fourcc {
	case "hvc1", "hev1":
		return "HEVC (H.265)"
	case "av01":
		return "AV1"
	case "vp08":
		return "VP8"
	case "vp09":
		return "VP9"
	case "mp4v":
		return "MPEG-4 Part 2"
	case "ac-3":
		return "Dolby Digital (AC-3)"
	case "ec-3":
		return "Dolby Digital Plus (E-AC-3)"
	case "Opus":
		return "Opus"
	case "fLaC":
		return "FLAC"
	case ".mp3":
		return "MP3"
	}
	return fmt.Sprintf("%q", fourcc)
}

// readMP4 reads an MP4's duration (moov/mvhd) and the codec of its first video
// and sound tracks (moov/trak/mdia/minf/stbl/stsd).
func readMP4(path string) (mp4Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return mp4Info{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return mp4Info{}, err
	}
	end := st.Size()
	first := true
	for off := int64(0); off+8 <= end; {
		typ, size, hdr, err := readBoxHeader(f, off, end)
		if err != nil {
			return mp4Info{}, err
		}
		if first && typ != "ftyp" {
			return mp4Info{}, errNotMP4
		}
		first = false
		if typ == "moov" {
			if size-hdr > maxMoovBytes {
				return mp4Info{}, errNotMP4
			}
			moov := make([]byte, size-hdr)
			if _, err := f.ReadAt(moov, off+hdr); err != nil {
				return mp4Info{}, errNotMP4
			}
			return parseMoov(moov)
		}
		off += size
	}
	return mp4Info{}, errNotMP4
}

// readBoxHeader reads the box at off: its type, total size and header size.
func readBoxHeader(r io.ReaderAt, off, end int64) (typ string, size, hdr int64, err error) {
	var b [16]byte
	if _, err := r.ReadAt(b[:8], off); err != nil {
		return "", 0, 0, errNotMP4
	}
	size, hdr, typ = int64(binary.BigEndian.Uint32(b[:4])), 8, string(b[4:8])
	switch size {
	case 0: // to the end of the file
		size = end - off
	case 1: // 64-bit size follows
		if _, err := r.ReadAt(b[8:16], off+8); err != nil {
			return "", 0, 0, errNotMP4
		}
		size, hdr = int64(binary.BigEndian.Uint64(b[8:16])), 16
	}
	if size < hdr || off+size > end {
		return "", 0, 0, errNotMP4
	}
	return typ, size, hdr, nil
}

// boxes calls fn with the type and body of each box in b, until fn returns
// false.
func boxes(b []byte, fn func(typ string, body []byte) bool) error {
	for p := 0; p+8 <= len(b); {
		size, hdr := uint64(binary.BigEndian.Uint32(b[p:])), 8
		if size == 1 {
			if p+16 > len(b) {
				return errNotMP4
			}
			size, hdr = binary.BigEndian.Uint64(b[p+8:]), 16
		} else if size == 0 {
			size = uint64(len(b) - p)
		}
		if size < uint64(hdr) || size > uint64(len(b)-p) {
			return errNotMP4
		}
		if !fn(string(b[p+4:p+8]), b[p+hdr:p+int(size)]) {
			return nil
		}
		p += int(size)
	}
	return nil
}

// child returns the body of the first box of type typ in b.
func child(b []byte, typ string) []byte {
	var out []byte
	boxes(b, func(t string, body []byte) bool {
		if t == typ {
			out = body
			return false
		}
		return true
	})
	return out
}

func parseMoov(moov []byte) (mp4Info, error) {
	var info mp4Info
	var durErr error
	found := false
	err := boxes(moov, func(typ string, body []byte) bool {
		switch typ {
		case "mvhd":
			found = true
			info.durationMs, durErr = mvhdDurationMs(body)
		case "trak":
			mdia := child(body, "mdia")
			hdlr := child(mdia, "hdlr")
			stsd := child(child(child(mdia, "minf"), "stbl"), "stsd")
			if len(hdlr) < 12 || len(stsd) < 16 {
				return true
			}
			entry := string(stsd[12:16]) // version+flags, entry count, then the first entry's size and type
			switch string(hdlr[8:12]) {
			case "vide":
				if info.video == "" {
					info.video = entry
				}
			case "soun":
				if info.audio == "" {
					info.audio = entry
				}
			}
		}
		return true
	})
	switch {
	case err != nil:
		return mp4Info{}, err
	case !found:
		return mp4Info{}, errNotMP4
	case durErr != nil:
		return mp4Info{}, durErr
	}
	return info, nil
}

// mvhdDurationMs reads the duration in a movie header box's body.
func mvhdDurationMs(b []byte) (int64, error) {
	var scale, dur uint64
	switch {
	case len(b) >= 20 && b[0] == 0:
		scale, dur = uint64(binary.BigEndian.Uint32(b[12:])), uint64(binary.BigEndian.Uint32(b[16:]))
	case len(b) >= 32 && b[0] == 1:
		scale, dur = uint64(binary.BigEndian.Uint32(b[20:])), binary.BigEndian.Uint64(b[24:])
	default:
		return 0, errNotMP4
	}
	if scale == 0 || dur == 0 || dur == 1<<32-1 || dur == 1<<64-1 {
		return 0, errors.New("the MP4 file does not say how long it is")
	}
	return int64(dur * 1000 / scale), nil
}
