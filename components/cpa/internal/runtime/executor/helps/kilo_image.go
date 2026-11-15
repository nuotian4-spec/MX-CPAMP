package helps

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/MoeForge/moego/base64"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func kiloInlineImage(value string) (string, bool) {
	for _, kind := range []string{"png", "jpeg", "webp"} {
		prefix := "data:image/" + kind + ";base64,"
		if strings.HasPrefix(value, prefix) {
			return kind, true
		}
	}
	return "", false
}

func kiloReduceImage(ctx context.Context, value string) []string {
	kind, ok := kiloInlineImage(value)
	if !ok || ctx.Err() != nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(value[strings.IndexByte(value, ',')+1:])
	if err != nil {
		return nil
	}
	if kiloAnimatedImage(kind, raw) {
		return nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format != kind || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
		return nil
	}
	orientation, known := kiloImageOrientation(kind, raw)
	if !known || ctx.Err() != nil {
		return nil
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	if orientation != 1 {
		source = kiloOrientImage(source, orientation)
	}
	bounds := source.Bounds()
	transparent := false
	if opaque, ok := source.(interface{ Opaque() bool }); ok {
		transparent = !opaque.Opaque()
	} else {
		for y := bounds.Min.Y; y < bounds.Max.Y && !transparent; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				_, _, _, a := source.At(x, y).RGBA()
				if a != 65535 {
					transparent = true
					break
				}
			}
		}
	}
	var candidates []string
	previous := value
	for _, edge := range []int{2048, 1536} {
		if ctx.Err() != nil {
			return candidates
		}
		w, h := bounds.Dx(), bounds.Dy()
		if w > edge || h > edge {
			if w >= h {
				h = max(1, h*edge/w)
				w = edge
			} else {
				w = max(1, w*edge/h)
				h = edge
			}
		}
		var output image.Image = source
		if w != bounds.Dx() || h != bounds.Dy() {
			scaled := image.NewNRGBA(image.Rect(0, 0, w, h))
			draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), source, bounds, draw.Src, nil)
			output = scaled
		}
		if ctx.Err() != nil {
			return candidates
		}
		var buffer bytes.Buffer
		mime := "jpeg"
		if transparent {
			mime = "png"
			err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buffer, output)
		} else {
			err = jpeg.Encode(&buffer, output, &jpeg.Options{Quality: 90})
		}
		if err != nil {
			return candidates
		}
		next := "data:image/" + mime + ";base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
		if len(next) < len(previous) {
			candidates = append(candidates, next)
			previous = next
		}
	}
	return candidates
}

func kiloAnimatedImage(kind string, raw []byte) bool {
	if kind == "png" {
		for offset := 8; offset+12 <= len(raw); {
			n := int(binary.BigEndian.Uint32(raw[offset:]))
			if n > len(raw)-offset-12 {
				return true
			}
			if string(raw[offset+4:offset+8]) == "acTL" {
				return true
			}
			offset += 12 + n
		}
	}
	if kind == "webp" {
		for offset := 12; offset+8 <= len(raw); {
			n := int(binary.LittleEndian.Uint32(raw[offset+4:]))
			if n > len(raw)-offset-8 {
				return true
			}
			name := string(raw[offset : offset+4])
			if name == "ANIM" || name == "ANMF" || (name == "VP8X" && n > 0 && raw[offset+8]&2 != 0) {
				return true
			}
			offset += 8 + n + (n & 1)
		}
	}
	return false
}

// Unknown orientation metadata keeps the original rather than risking a rotated image.
func kiloImageOrientation(kind string, raw []byte) (int, bool) {
	var exif []byte
	if kind == "jpeg" {
		for offset := 2; offset+4 <= len(raw); {
			if raw[offset] != 0xff {
				return 1, false
			}
			marker := raw[offset+1]
			if marker == 0xda || marker == 0xd9 {
				break
			}
			if marker == 0x00 || marker == 0xff {
				offset++
				continue
			}
			n := int(binary.BigEndian.Uint16(raw[offset+2:]))
			if n < 2 || n > len(raw)-offset-2 {
				return 1, false
			}
			payload := raw[offset+4 : offset+2+n]
			if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				exif = payload[6:]
				break
			}
			offset += 2 + n
		}
	} else if kind == "png" || kind == "webp" {
		offset := 8
		if kind == "webp" {
			offset = 12
		}
		for offset+12 <= len(raw) {
			var n int
			var name string
			var payload []byte
			if kind == "png" {
				n = int(binary.BigEndian.Uint32(raw[offset:]))
				name = string(raw[offset+4 : offset+8])
				if n > len(raw)-offset-12 {
					return 1, false
				}
				payload = raw[offset+8 : offset+8+n]
				offset += n + 12
			} else {
				n = int(binary.LittleEndian.Uint32(raw[offset+4:]))
				name = string(raw[offset : offset+4])
				if n > len(raw)-offset-8 {
					return 1, false
				}
				payload = raw[offset+8 : offset+8+n]
				offset += 8 + n + (n & 1)
			}
			if name == "eXIf" || name == "EXIF" {
				exif = bytes.TrimPrefix(payload, []byte("Exif\x00\x00"))
				break
			}
		}
	}
	if exif == nil {
		return 1, true
	}
	if len(exif) < 8 {
		return 1, false
	}
	var order binary.ByteOrder
	switch string(exif[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1, false
	}
	if order.Uint16(exif[2:]) != 42 {
		return 1, false
	}
	offset := int(order.Uint32(exif[4:]))
	if offset < 8 || offset+2 > len(exif) {
		return 1, false
	}
	count := int(order.Uint16(exif[offset:]))
	offset += 2
	for i := 0; i < count; i++ {
		if offset+12 > len(exif) {
			return 1, false
		}
		entry := exif[offset : offset+12]
		if order.Uint16(entry) == 0x0112 {
			if order.Uint16(entry[2:]) != 3 || order.Uint32(entry[4:]) != 1 {
				return 1, false
			}
			value := int(order.Uint16(entry[8:]))
			return value, value >= 1 && value <= 8
		}
		offset += 12
	}
	return 1, true
}

func kiloOrientImage(source image.Image, orientation int) image.Image {
	b := source.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	result := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			result.SetNRGBA(dx, dy, color.NRGBAModel.Convert(source.At(x+b.Min.X, y+b.Min.Y)).(color.NRGBA))
		}
	}
	return result
}
