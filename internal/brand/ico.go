package brand

import (
	"bytes"
	"encoding/binary"
)

const ApplicationID = "app.superflow.desktop"

// ICO contains native PNG images at common taskbar, panel and high-DPI sizes.
func ICO() ([]byte, error) {
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	images := make([][]byte, len(sizes))
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		data, err := PNG(size)
		if err != nil {
			return nil, err
		}
		images[i] = data
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(sizes)))
	for i, size := range sizes {
		out.Write([]byte{byte(size % 256), byte(size % 256), 0, 0})
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))
		_ = binary.Write(&out, binary.LittleEndian, uint16(32))
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(images[i])))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(images[i])
	}
	for _, data := range images {
		out.Write(data)
	}
	return out.Bytes(), nil
}
