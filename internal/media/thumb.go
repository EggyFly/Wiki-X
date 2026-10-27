package media

import (
	"bytes"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"path"
	"strings"
)

const DefaultThumbSize = 320

func MakeThumb(data []byte, maxSize int) ([]byte, bool) {
	if maxSize <= 0 {
		maxSize = DefaultThumbSize
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, false
	}
	if int64(config.Width)*int64(config.Height) > 16_000_000 {
		return nil, false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || (width <= maxSize && height <= maxSize) {
		return nil, false
	}
	targetWidth, targetHeight := width, height
	if width >= height {
		targetWidth = maxSize
		targetHeight = height * maxSize / width
	} else {
		targetHeight = maxSize
		targetWidth = width * maxSize / height
	}
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetHeight < 1 {
		targetHeight = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		y0 := y * height / targetHeight
		y1 := (y + 1) * height / targetHeight
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < targetWidth; x++ {
			x0 := x * width / targetWidth
			x1 := (x + 1) * width / targetWidth
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var red, green, blue, alpha uint64
			var count uint64
			for py := y0; py < y1; py++ {
				for px := x0; px < x1; px++ {
					r, g, b, a := img.At(bounds.Min.X+px, bounds.Min.Y+py).RGBA()
					red += uint64(r)
					green += uint64(g)
					blue += uint64(b)
					alpha += uint64(a)
					count++
				}
			}
			if count == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8((red / count) >> 8),
				G: uint8((green / count) >> 8),
				B: uint8((blue / count) >> 8),
				A: uint8((alpha / count) >> 8),
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func ThumbName(name string) string {
	ext := path.Ext(name)
	if ext == "" {
		return ""
	}
	return "t-" + strings.TrimSuffix(name, ext) + ".jpg"
}

func ThumbURL(value string) string {
	if !strings.HasPrefix(value, "/media/") {
		return value
	}
	name := strings.TrimPrefix(value, "/media/")
	if strings.HasPrefix(name, "t-") {
		return value
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		thumb := ThumbName(name)
		if thumb == "" {
			return value
		}
		return "/media/" + thumb
	default:
		return value
	}
}
