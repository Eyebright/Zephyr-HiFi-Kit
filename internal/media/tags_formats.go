package media

import (
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/simonhull/audiometa"
)

func preferTags(primary, fallback Tags) Tags {
	if primary.Title == "" {
		primary.Title = fallback.Title
	}
	if primary.Artist == "" {
		primary.Artist = fallback.Artist
	}
	if primary.Album == "" {
		primary.Album = fallback.Album
	}
	if primary.Genre == "" {
		primary.Genre = fallback.Genre
	}
	if primary.AlbumArtist == "" {
		primary.AlbumArtist = fallback.AlbumArtist
	}
	if primary.Composer == "" {
		primary.Composer = fallback.Composer
	}
	if primary.Year == 0 {
		primary.Year = fallback.Year
	}
	if primary.DiscNumber == 0 {
		primary.DiscNumber = fallback.DiscNumber
	}
	if primary.TrackNumber == 0 {
		primary.TrackNumber = fallback.TrackNumber
	}
	return primary
}

func readDSFTags(r io.ReaderAt, size int64, encoding string) (Tags, error) {
	var tags Tags
	if size < 28 {
		return tags, errInvalidTags
	}
	var h [28]byte
	if _, err := r.ReadAt(h[:], 0); err != nil {
		return tags, err
	}
	if string(h[:4]) != "DSD " {
		return tags, errInvalidTags
	}
	var p [16]byte
	if _, err := r.ReadAt(p[:], 20); err != nil {
		return tags, err
	}
	offset := int64(binary.LittleEndian.Uint64(p[:8]))
	n := int64(binary.LittleEndian.Uint64(p[8:]))
	if n == 0 {
		return tags, nil
	}
	if offset < 28 || offset > size {
		return tags, errInvalidTags
	}
	remaining := size - offset
	if n > 0 && n <= maxTagBytes && n <= remaining {
		return parseID3v2(r, offset, n, encoding)
	}
	var magic [3]byte
	if _, err := r.ReadAt(magic[:], offset); err != nil {
		return tags, err
	}
	if string(magic[:]) == "ID3" {
		return parseID3v2(r, offset, remaining, encoding)
	}
	return tags, errInvalidTags
}

func readDFFTags(r io.ReaderAt, size int64, encoding string) (Tags, error) {
	var tags Tags
	if size < 12 {
		return tags, errInvalidTags
	}
	var h [12]byte
	if _, err := r.ReadAt(h[:], 0); err != nil {
		return tags, err
	}
	if string(h[:4]) != "FRM8" || string(h[8:11]) != "DFF" {
		return tags, errInvalidTags
	}
	end := int64(binary.BigEndian.Uint32(h[4:8])) + 8
	if end < 11 || end > size {
		return tags, errInvalidTags
	}
	off := int64(11)
	for i := 0; off < end && i < maxTagBlocks; i++ {
		if off > end-8 {
			return tags, errInvalidTags
		}
		var ch [8]byte
		if _, err := r.ReadAt(ch[:], off); err != nil {
			return tags, err
		}
		off += 8
		n := int64(binary.BigEndian.Uint32(ch[4:8]))
		padded := n + n%2
		if padded > end-off {
			return tags, errInvalidTags
		}
		if string(ch[:4]) == "ID3 " {
			if n > maxTagBytes {
				return tags, errInvalidTags
			}
			return parseID3v2(r, off, n, encoding)
		}
		off += padded
	}
	if off != end {
		return tags, errInvalidTags
	}
	return tags, nil
}

func readAIFFTags(r io.ReaderAt, size int64, encoding string) (Tags, error) {
	var native, embedded Tags
	if size < 12 {
		return native, errInvalidTags
	}
	var h [12]byte
	if _, err := r.ReadAt(h[:], 0); err != nil {
		return native, err
	}
	if string(h[:4]) != "FORM" || (string(h[8:]) != "AIFF" && string(h[8:]) != "AIFC") {
		return native, errInvalidTags
	}
	end := int64(binary.BigEndian.Uint32(h[4:8])) + 8
	if end < 12 || end > size {
		return native, errInvalidTags
	}
	off := int64(12)
	var embeddedErr error
	for i := 0; off < end && i < maxTagBlocks; i++ {
		if off > end-8 {
			return preferTags(embedded, native), errInvalidTags
		}
		var ch [8]byte
		if _, err := r.ReadAt(ch[:], off); err != nil {
			return preferTags(embedded, native), err
		}
		off += 8
		n := int64(binary.BigEndian.Uint32(ch[4:8]))
		padded := n + n%2
		if padded > end-off {
			return preferTags(embedded, native), errInvalidTags
		}
		id := string(ch[:4])
		if id == "ID3 " {
			if n > maxTagBytes {
				return preferTags(embedded, native), errInvalidTags
			}
			embedded, embeddedErr = parseID3v2(r, off, n, encoding)
			off += padded
			continue
		}
		if (id == "NAME" || id == "AUTH") && n <= maxTagValueBytes {
			buf := make([]byte, int(n))
			if n > 0 {
				if _, err := r.ReadAt(buf, off); err != nil {
					return preferTags(embedded, native), err
				}
			}
			field := "TITLE"
			if id == "AUTH" {
				field = "ARTIST"
			}
			native.set(field, decodeTagText(buf, encoding))
		}
		off += padded
	}
	if off != end {
		return preferTags(embedded, native), errInvalidTags
	}
	return preferTags(embedded, native), embeddedErr
}

func readAudioMetaTags(path string) (Tags, error) {
	file, err := audiometa.Open(path)
	if err != nil {
		var unsupported *audiometa.UnsupportedFormatError
		if errors.As(err, &unsupported) {
			return Tags{}, nil
		}
		return Tags{}, err
	}
	defer file.Close()
	var tags Tags
	tags.set("TITLE", file.Tags.Title)
	tags.set("ARTIST", file.Tags.Artist)
	tags.set("ALBUM", file.Tags.Album)
	tags.set("ALBUMARTIST", file.Tags.AlbumArtist)
	tags.set("GENRE", strings.Join(file.Tags.Genres, "; "))
	tags.set("COMPOSER", strings.Join(file.Tags.Composers, "; "))
	if file.Tags.Year > 0 {
		tags.set("YEAR", strconv.Itoa(file.Tags.Year))
	}
	if file.Tags.DiscNumber > 0 {
		tags.set("DISCNUMBER", strconv.Itoa(file.Tags.DiscNumber))
	}
	if file.Tags.TrackNumber > 0 {
		tags.set("TRACKNUMBER", strconv.Itoa(file.Tags.TrackNumber))
	}
	return tags, nil
}

func needsAudioMeta(h []byte) bool {
	return len(h) >= 8 && (string(h[:4]) == "OggS" || string(h[4:8]) == "ftyp")
}
