package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestMakeThumb(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, src); err != nil {
		t.Fatal(err)
	}
	thumb, ok := MakeThumb(raw.Bytes(), 320)
	if !ok {
		t.Fatal("thumb was not generated")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
		t.Fatalf("thumb size = %v", decoded.Bounds())
	}
	if _, ok := MakeThumb(raw.Bytes(), 800); ok {
		t.Fatal("small image should not get a thumb")
	}
}

func TestThumbNameAndURL(t *testing.T) {
	name := "0123456789abcdef0123456789abcdef.png"
	if got := ThumbName(name); got != "t-0123456789abcdef0123456789abcdef.jpg" {
		t.Fatalf("thumb name = %q", got)
	}
	if got := ThumbURL("/media/" + name); got != "/media/t-0123456789abcdef0123456789abcdef.jpg" {
		t.Fatalf("thumb url = %q", got)
	}
	if got := ThumbURL("https://example.com/a.png"); got != "https://example.com/a.png" {
		t.Fatalf("external url changed = %q", got)
	}
	if got := ThumbURL("/media/t-0123456789abcdef0123456789abcdef.jpg"); got != "/media/t-0123456789abcdef0123456789abcdef.jpg" {
		t.Fatalf("thumb url rewritten = %q", got)
	}
}
