package qrcode

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"rsc.io/qr"
)

const link = "https://farborastreadores.com.br/evento/encontro-insanos-mc"

// O desenho é o código: um quadrado por módulo preto (SVG) e cada módulo
// com scale × scale pixels na cor certa (PNG), com a margem branca.
func TestDrawings(t *testing.T) {
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	black := 0
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				black++
			}
		}
	}

	svg, err := SVG(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(svg), "h1v1h-1z"); got != black {
		t.Errorf("SVG com %d módulos pretos, o código tem %d", got, black)
	}
	side := code.Size + 2*quiet
	if !bytes.Contains(svg, []byte(`viewBox="0 0 `)) || !strings.Contains(string(svg), `width="`) {
		t.Errorf("SVG sem viewBox ou fundo: %.80s", svg)
	}

	const scale = 10
	raw, err := PNG(link, scale)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != side*scale || b.Dy() != side*scale {
		t.Fatalf("PNG de %v, quer %d px", b, side*scale)
	}
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			r, _, _, _ := img.At((x+quiet)*scale+scale/2, (y+quiet)*scale+scale/2).RGBA()
			if (r == 0) != code.Black(x, y) {
				t.Fatalf("módulo (%d,%d) com a cor trocada", x, y)
			}
		}
	}
	// A margem é branca.
	if r, _, _, _ := img.At(1, 1).RGBA(); r == 0 {
		t.Error("margem preta")
	}

	for _, bad := range []string{"", strings.Repeat("a", MaxText+1)} {
		if _, err := SVG(bad); err == nil {
			t.Errorf("texto de %d letras devia ser recusado", len(bad))
		}
	}
}
