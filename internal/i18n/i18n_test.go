package i18n

import "testing"

func TestCatalogsComplete(t *testing.T) {
	zh := For("zh-CN")
	en := For("en")
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Fatalf("english catalog misses %q", key)
		}
	}
}

func TestMessageTranslation(t *testing.T) {
	if got := Message("en", "用户名或密码错误."); got != "Invalid username or password." {
		t.Fatalf("translation = %q", got)
	}
	if got := Message("zh-CN", "用户名或密码错误."); got != "用户名或密码错误." {
		t.Fatalf("zh translation = %q", got)
	}
	if got := Message("en", "not translated"); got != "not translated" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh-CN": "zh-CN",
		"zh-TW": "zh-CN",
		"en-US": "en",
		"fr":    "zh-CN",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}
