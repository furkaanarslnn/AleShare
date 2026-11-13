package release

import (
	"archive/zip"
	"encoding/binary"
	"io"
)

// versionCodeAttr is the resource number of android:versionCode.
const versionCodeAttr = 0x0101021b

// APKCode reads the release number (versionCode) out of an APK, 0 when the file is not an APK or says none. The
// published APK is always called AleShare.apk, so its name does not tell which release it is.
func APKCode(path string) int {
	z, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != "AndroidManifest.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return 0
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, 4<<20))
		if err != nil {
			return 0
		}
		return manifestVersionCode(data)
	}
	return 0
}

// manifestVersionCode finds android:versionCode in a compiled AndroidManifest.xml. The file is a row of chunks,
// each beginning with its type (2 bytes), the size of its header (2) and its whole size (4): the table that gives
// every attribute name its resource number, then the elements. The first element is <manifest>; each of its
// attributes is 20 bytes: namespace, name, raw value, then the value's size (2), a zero byte, its type (1) and
// the value itself (4).
func manifestVersionCode(data []byte) int {
	u16 := func(at int) int { return int(binary.LittleEndian.Uint16(data[at:])) }
	u32 := func(at int) int { return int(binary.LittleEndian.Uint32(data[at:])) }
	var numbers []int // the resource number of each name, by the name's place among the strings
	for at := 8; at+8 <= len(data); {
		kind, size := u16(at), u32(at+4)
		if size < 8 || at+size > len(data) {
			return 0
		}
		switch kind {
		case 0x0180: // the resource numbers
			for i := at + 8; i+4 <= at+size; i += 4 {
				numbers = append(numbers, u32(i))
			}
		case 0x0102: // the first element
			ext := at + u16(at+2)
			if ext+20 > at+size {
				return 0
			}
			first, each, count := ext+u16(ext+8), u16(ext+10), u16(ext+12)
			for i := 0; i < count; i++ {
				a := first + i*each
				if each < 20 || a+20 > at+size {
					return 0
				}
				if name := u32(a + 4); name < len(numbers) && numbers[name] == versionCodeAttr {
					return u32(a + 16)
				}
			}
			return 0
		}
		at += size
	}
	return 0
}
