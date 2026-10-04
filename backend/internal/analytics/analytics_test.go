package analytics

import "testing"

const (
	iphoneSafari  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	iphoneInsta   = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Instagram 330.0.0.0"
	androidChrome = "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
	androidTablet = "Mozilla/5.0 (Linux; Android 13; SM-X200) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	windowsEdge   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0"
	macSafari     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
	samsung       = "Mozilla/5.0 (Linux; Android 14; SM-A546B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36"
	googlebot     = "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	headless      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/126.0.0.0 Safari/537.36"
)

func TestUserAgent(t *testing.T) {
	cases := []struct {
		ua                   string
		width                int
		device, browser, sys string
	}{
		{iphoneSafari, 390, "mobile", "Safari", "iOS"},
		{iphoneInsta, 390, "mobile", "Instagram", "iOS"},
		{androidChrome, 412, "mobile", "Chrome", "Android"},
		{androidTablet, 800, "tablet", "Chrome", "Android"},
		{windowsEdge, 1366, "desktop", "Edge", "Windows"},
		{macSafari, 1440, "desktop", "Safari", "macOS"},
		{samsung, 384, "mobile", "Samsung Internet", "Android"},
		// Navegador que não se diz celular, com a tela de celular.
		{"Mozilla/5.0 (X11; Linux x86_64) Firefox/127.0", 360, "mobile", "Firefox", "Linux"},
	}
	for _, c := range cases {
		if got := Device(c.ua, c.width); got != c.device {
			t.Errorf("Device(%q) = %s, quer %s", c.ua, got, c.device)
		}
		if got := Browser(c.ua); got != c.browser {
			t.Errorf("Browser(%q) = %s, quer %s", c.ua, got, c.browser)
		}
		if got := OS(c.ua); got != c.sys {
			t.Errorf("OS(%q) = %s, quer %s", c.ua, got, c.sys)
		}
	}
}

func TestBots(t *testing.T) {
	for _, ua := range []string{googlebot, headless, "curl/8.5.0", "facebookexternalhit/1.1", "WhatsApp/2.23 preview"} {
		if !bots.MatchString(ua) {
			t.Errorf("robô não reconhecido: %q", ua)
		}
	}
	cubot := "Mozilla/5.0 (Linux; Android 10; CUBOT X30) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"
	for _, ua := range []string{iphoneSafari, iphoneInsta, androidChrome, windowsEdge, samsung, cubot} {
		if bots.MatchString(ua) {
			t.Errorf("gente tratada como robô: %q", ua)
		}
	}
}

func TestReferrerHost(t *testing.T) {
	own := "farborastreadores.com.br"
	cases := map[string]string{
		"https://www.google.com/search?q=rastreador": "google.com",
		"https://l.instagram.com/?u=x":               "l.instagram.com",
		"http://m.facebook.com/":                     "m.facebook.com",
		"https://farborastreadores.com.br/#planos":   "",
		"https://www.farborastreadores.com.br/":      "",
		"https://painel.farborastreadores.com.br/":   "",
		"":                         "",
		"android-app://com.google": "",
		"nada disso":               "",
	}
	for raw, want := range cases {
		if got := ReferrerHost(raw, own); got != want {
			t.Errorf("ReferrerHost(%q) = %q, quer %q", raw, got, want)
		}
	}
	if got := ReferrerHost("https://google.com/", "localhost:5173"); got != "google.com" {
		t.Errorf("com porta no próprio host: %q", got)
	}
}
