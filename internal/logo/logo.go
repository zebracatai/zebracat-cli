// Package logo draws the real Zebracat mark in a terminal.
//
// The source of truth is squanchy's public/logo.svg (viewBox 37×47): a cat-head
// shield — two pointed ears at the top corners with a flat notch between them,
// straight sides, a pointed chin — masked by 9 vertical zebra stripes (2.35 wide,
// every 4.23), colour #6B3BF4. Render rasterises that exact geometry into
// quadrant block characters (2×2 sub-pixels per cell), sampling each sub-pixel
// and keeping it when most of it is inside the shield AND inside a stripe.
package logo

import (
	"math"
	"strings"
)

// Color is the logo colour from logo.svg.
const Color = "#6B3BF4"

// ViewBox of logo.svg.
const (
	Width  = 37.0
	Height = 47.0
)

// shield is the SVG path (M27.73 13.4578 L36.1899 0 L36.2023 38.1055
// L18.0444 46.9173 L-0.0774886 38.0316 L0 -0.018 L8.46001 13.4206 Z).
var shield = [][2]float64{
	{27.73, 13.4578}, {36.1899, 0}, {36.2023, 38.1055}, {18.0444, 46.9173},
	{-0.0774886, 38.0316}, {0, -0.0179824}, {8.46001, 13.4206},
}

// stripes are the mask's rects: x, width, height (all start at y=0).
var stripes = [][3]float64{
	{0, 2.35, 39.245}, {4.23001, 2.35, 46.06}, {8.45999, 2.35, 46.06}, {12.69, 2.35, 46.06},
	{16.92, 2.35, 47}, {21.15, 2.35, 46.06}, {25.38, 2.35, 46.06}, {29.61, 2.35, 46.06},
	{33.84, 2.35, 39.245},
}

// inShield is the even-odd point-in-polygon test.
func inShield(x, y float64) bool {
	in := false
	n := len(shield)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := shield[i][0], shield[i][1]
		xj, yj := shield[j][0], shield[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

func inStripe(x, y float64) bool {
	for _, s := range stripes {
		if x >= s[0] && x < s[0]+s[1] && y >= 0 && y < s[2] {
			return true
		}
	}
	return false
}

// Inside reports whether a viewBox point is painted (shield ∩ stripes; solid
// skips the stripes, for the 1-line mark).
func Inside(x, y float64, solid bool) bool {
	return inShield(x, y) && (solid || inStripe(x, y))
}

// quadrants maps the 4 sub-pixels (bit 0 top-left, 1 top-right, 2 bottom-left,
// 3 bottom-right) to a block character.
var quadrants = [16]string{
	" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛",
	"▗", "▚", "▐", "▜", "▄", "▙", "▟", "█",
}

// Render draws the logo cols×rows cells tall, plain (no colour). solid=true
// draws the shield without its stripes.
func Render(cols, rows int, solid bool) []string {
	return render(cols, rows, Width/float64(cols*2), Height/float64(rows*2), solid)
}

// Period is the stripe spacing in viewBox units.
const Period = 4.23

// Striped draws the logo rows tall with the stripe period at exactly 2 cells, so
// every stripe is one full column and every gap one empty column.
//
// The SVG is symmetric about its centre stripe (x≈18.1), so the left half is
// rendered and mirrored: sampling can't make the two ears differ.
func Striped(rows int) []string {
	sw := Period / 4
	cols := 17 // 9 stripes + 8 gaps; the centre stripe is column 8
	half := render(cols, rows, sw, Height/float64(rows*2), false)
	out := make([]string, rows)
	for r, line := range half {
		cells := []rune(line)
		for len(cells) < cols {
			cells = append(cells, ' ')
		}
		for c := cols/2 + 1; c < cols; c++ {
			cells[c] = mirror(cells[cols-1-c])
		}
		out[r] = strings.TrimRight(string(cells), " ")
	}
	return out
}

// mirror flips a quadrant character left↔right.
func mirror(r rune) rune {
	for i, q := range quadrants {
		if []rune(q)[0] == r {
			m := (i&1)<<1 | (i&2)>>1 | (i&4)<<1 | (i&8)>>1
			return []rune(quadrants[m])[0]
		}
	}
	return r
}

func render(cols, rows int, sw, sh float64, solid bool) []string {
	const ss = 6 // samples per sub-pixel edge
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			bits := 0
			for q := 0; q < 4; q++ {
				px, py := c*2+q%2, r*2+q/2
				hit := 0
				for a := 0; a < ss; a++ {
					for d := 0; d < ss; d++ {
						x := (float64(px) + (float64(a)+0.5)/ss) * sw
						y := (float64(py) + (float64(d)+0.5)/ss) * sh
						if Inside(x, y, solid) {
							hit++
						}
					}
				}
				if hit*2 > ss*ss {
					bits |= 1 << q
				}
			}
			b.WriteString(quadrants[bits])
		}
		out[r] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// Sized draws the striped logo with each stripe s half-cells wide (and each gap
// the same), so stripes stay whole columns at every size: s=1 is 9×5 cells
// (half-block stripes), s=2 is 17×10, s=4 is 34×20. Rows follow the SVG's
// 37:47 at a 1:2.2 terminal cell. The left half is mirrored onto the right at
// sub-pixel level about the centre stripe, so the ears always match.
func Sized(s int) []string {
	if s < 1 {
		s = 1
	}
	const ss = 6
	sw := Period / float64(2*s) // viewBox units per sub-pixel column
	wsub := 17 * s              // 9 stripes + 8 gaps, s sub-pixels each
	rows := int(math.Round(Height / (4.4 * sw)))
	hsub := rows * 2
	sh := Height / float64(hsub)
	grid := make([][]bool, hsub)
	for y := range grid {
		grid[y] = make([]bool, wsub+1)
		for x := 0; x < wsub; x++ {
			hit := 0
			for a := 0; a < ss; a++ {
				for d := 0; d < ss; d++ {
					if Inside((float64(x)+(float64(a)+0.5)/ss)*sw, (float64(y)+(float64(d)+0.5)/ss)*sh, false) {
						hit++
					}
				}
			}
			grid[y][x] = hit*2 > ss*ss
		}
		for x := 0; x < wsub/2; x++ {
			grid[y][wsub-1-x] = grid[y][x]
		}
	}
	cols := (wsub + 1) / 2
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			bits := 0
			for q := 0; q < 4; q++ {
				if grid[2*r+q/2][2*c+q%2] {
					bits |= 1 << q
				}
			}
			b.WriteString(quadrants[bits])
		}
		out[r] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// Sizes, smallest first: the cells each takes, for picking one that fits.
// The 34×20 size (Sized(4)) is not offered: the owner found it far too big.
var Sizes = []int{1, 2}

// Dimensions of Sized(s): columns and rows.
func Dimensions(s int) (cols, rows int) {
	l := Sized(s)
	for _, x := range l {
		if n := len([]rune(x)); n > cols {
			cols = n
		}
	}
	return cols, len(l)
}

// Big is the 17×10 welcome logo.
func Big() []string { return Sized(2) }

// Small is the 2-cell, 1-row mark (ears and body of the same shield) used next
// to the prompt and as the base of the working animation.
func Small() string { return strings.Join(Render(2, 1, true), "") }
