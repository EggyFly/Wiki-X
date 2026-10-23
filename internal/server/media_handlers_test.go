package server

import "testing"

func TestDetectAudioMedia(t *testing.T) {
	cases := []struct {
		name        string
		data        []byte
		contentType string
		ext         string
	}{
		{"wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), "audio/wav", ".wav"},
		{"rf64", []byte("RF64\x00\x00\x00\x00WAVEfmt "), "audio/wav", ".wav"},
		{"flac", []byte("fLaC\x00\x00\x00\x22"), "audio/flac", ".flac"},
		{"ogg", []byte("OggS\x00\x02\x00\x00"), "audio/ogg", ".ogg"},
		{"mp3 id3", []byte("ID3\x04\x00\x00\x00"), "audio/mpeg", ".mp3"},
		{"mp3 frame", []byte{0xFF, 0xFB, 0x90, 0x64}, "audio/mpeg", ".mp3"},
	}
	for _, item := range cases {
		contentType, ext := detectMedia(item.data)
		if contentType != item.contentType || ext != item.ext {
			t.Fatalf("%s = %q, %q", item.name, contentType, ext)
		}
	}
}

func TestMediaNamePatternAudio(t *testing.T) {
	for _, name := range []string{
		"0123456789abcdef0123456789abcdef.mp3",
		"pr-0123456789abcdef.flac",
		"0123456789abcdef0123456789abcdef.wav",
	} {
		if !mediaNamePattern.MatchString(name) {
			t.Fatalf("%s should match", name)
		}
	}
	if mediaNamePattern.MatchString("0123456789abcdef0123456789abcdef.exe") {
		t.Fatal("exe should not match")
	}
}

func TestMediaContentTypes(t *testing.T) {
	cases := map[string]string{
		"a.wav":  "audio/wav",
		"a.mp3":  "audio/mpeg",
		"a.ogg":  "audio/ogg",
		"a.flac": "audio/flac",
		"a.png":  "image/png",
	}
	for name, want := range cases {
		if got := mediaContentType(name); got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
}
