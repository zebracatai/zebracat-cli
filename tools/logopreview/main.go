// logopreview writes a PNG comparing the real logo (logo.svg's geometry) with
// the terminal render (quadrant cells at a typical 1:2.2 cell aspect), on a dark
// and a light background:  go run ./tools/logopreview -o logo-preview.png
package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"github.com/zebracatai/zebracat-cli/internal/logo"
)

var (
	brand = color.RGBA{0x6B, 0x3B, 0xF4, 0xFF}
	dark  = color.RGBA{0x1e, 0x1e, 0x24, 0xFF}
	light = color.RGBA{0xfa, 0xfa, 0xfa, 0xFF}
)

const (
	svgScale = 6  // px per viewBox unit
	cellW    = 12 // px per terminal cell
	cellH    = 26
	pad      = 24
)

func fill(img *image.RGBA, r image.Rectangle, c color.Color) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}

// drawSVG paints the logo geometry at (ox, oy).
func drawSVG(img *image.RGBA, ox, oy int) {
	for y := 0; y < int(logo.Height*svgScale); y++ {
		for x := 0; x < int(logo.Width*svgScale); x++ {
			if logo.Inside((float64(x)+0.5)/svgScale, (float64(y)+0.5)/svgScale, false) {
				img.Set(ox+x, oy+y, brand)
			}
		}
	}
}

// quad bits per character (bit 0 top-left, 1 top-right, 2 bottom-left, 3 bottom-right).
var bits = map[rune]int{' ': 0, '▘': 1, '▝': 2, '▀': 3, '▖': 4, '▌': 5, '▞': 6, '▛': 7,
	'▗': 8, '▚': 9, '▐': 10, '▜': 11, '▄': 12, '▙': 13, '▟': 14, '█': 15}

// drawCells paints terminal block characters as a terminal would.
func drawCells(img *image.RGBA, ox, oy int, lines []string) {
	for r, line := range lines {
		for c, ch := range []rune(line) {
			b := bits[ch]
			for q := 0; q < 4; q++ {
				if b&(1<<q) == 0 {
					continue
				}
				x0 := ox + c*cellW + (q%2)*cellW/2
				y0 := oy + r*cellH + (q/2)*cellH/2
				fill(img, image.Rect(x0, y0, x0+cellW/2, y0+cellH/2), brand)
			}
		}
	}
}

func main() {
	out := flag.String("o", "logo-preview.png", "output PNG")
	sizes := flag.Bool("sizes", false, "draw every terminal size (34×20, 17×10, 9×5, 1-row) instead")
	flag.Parse()
	if *sizes {
		writeSizes(*out)
		return
	}
	big := logo.Big()
	svgW, svgH := int(logo.Width*svgScale), int(logo.Height*svgScale)
	termW, termH := 17*cellW, len(big)*cellH
	small := []string{logo.Small()}
	panelW := pad + svgW + pad + termW + pad + 2*cellW + pad
	panelH := pad + max(svgH, termH) + pad
	img := image.NewRGBA(image.Rect(0, 0, panelW, panelH*2))
	for i, bg := range []color.Color{dark, light} {
		oy := i * panelH
		fill(img, image.Rect(0, oy, panelW, oy+panelH), bg)
		drawSVG(img, pad, oy+pad)
		drawCells(img, pad+svgW+pad, oy+pad, big)
		drawCells(img, pad+svgW+pad+termW+pad, oy+pad, small)
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

// writeSizes draws every size the CLI picks from the window, side by side.
func writeSizes(out string) {
	var sets [][]string
	for i := len(logo.Sizes) - 1; i >= 0; i-- {
		sets = append(sets, logo.Sized(logo.Sizes[i]))
	}
	sets = append(sets, []string{logo.Small()})
	w, h := pad, 0
	for _, s := range sets {
		c := 0
		for _, l := range s {
			if n := len([]rune(l)); n > c {
				c = n
			}
		}
		w += c*cellW + pad
		if len(s)*cellH > h {
			h = len(s) * cellH
		}
	}
	panelH := pad + h + pad
	img := image.NewRGBA(image.Rect(0, 0, w, panelH*2))
	for i, bg := range []color.Color{dark, light} {
		oy := i * panelH
		fill(img, image.Rect(0, oy, w, oy+panelH), bg)
		x := pad
		for _, s := range sets {
			drawCells(img, x, oy+pad+h-len(s)*cellH, s) // bottoms aligned
			c := 0
			for _, l := range s {
				if n := len([]rune(l)); n > c {
					c = n
				}
			}
			x += c*cellW + pad
		}
	}
	f, err := os.Create(out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}
