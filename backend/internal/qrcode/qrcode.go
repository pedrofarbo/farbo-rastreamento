// Package qrcode desenha QR Codes para imprimir: em SVG (vetor, serve para
// banner ou adesivo de qualquer tamanho) e em PNG, sempre com a margem
// branca de 4 módulos que os leitores precisam.
package qrcode

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"unicode/utf8"

	"rsc.io/qr"
)

// quiet é a margem branca, em módulos (a especificação pede 4).
const quiet = 4

// MaxText: um link curto. Mais que isso deixa o código denso demais para
// ler de longe.
const MaxText = 300

func encode(text string) (*qr.Code, error) {
	if text == "" || utf8.RuneCountInString(text) > MaxText {
		return nil, errors.New("texto vazio ou longo demais para o QR Code")
	}
	// M: recupera ~15% do código (um amassado, um reflexo) sem ficar denso.
	return qr.Encode(text, qr.M)
}

// SVG é o QR Code em vetor: um quadrado por módulo preto.
func SVG(text string) ([]byte, error) {
	code, err := encode(text)
	if err != nil {
		return nil, err
	}
	side := code.Size + 2*quiet
	var path strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, side, side)
	fmt.Fprintf(&out, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`, side, side, path.String())
	return out.Bytes(), nil
}

// PNG é o QR Code em pixels: cada módulo vira scale × scale.
func PNG(text string, scale int) ([]byte, error) {
	code, err := encode(text)
	if err != nil {
		return nil, err
	}
	scale = min(max(scale, 1), 40)
	side := (code.Size + 2*quiet) * scale
	img := image.NewPaletted(image.Rect(0, 0, side, side), color.Palette{color.White, color.Black})
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if !code.Black(x, y) {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				row := ((y+quiet)*scale + dy) * img.Stride
				for dx := 0; dx < scale; dx++ {
					img.Pix[row+(x+quiet)*scale+dx] = 1
				}
			}
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
