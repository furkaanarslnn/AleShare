//go:build ignore

// icon_gen draws AleShare's two signs and writes every file that shows them, so that all of them are one drawing:
//
//	icon.ico, icon-off.ico                           the program's icon, and the grey one of the tray while not connected
//	android/.../drawable/ic_launcher_foreground.xml  the app's launcher icon, with its background and its one-colour
//	                     ic_launcher_background.xml  version for themed icons
//	                     ic_launcher_monochrome.xml
//	                     ic_hotspot.xml              the Quick Settings tile and the widget while sharing is on
//	                     ic_widget_logo.xml          the widget while sharing is off
//	                     ic_logo.xml                 the sign at the top of the app
//	                     ic_stat_hotspot.xml         the notifications' icon in the status bar: the word
//	logo.svg                                         the picture at the top of the README
//
// and prints the symbol that ui/index.html carries for the window's header.
//
// The logo is the hotspot sign everyone knows: a dot with two arcs around it, open at the bottom. Only the
// notifications show the name instead: "Ale" in rounded letters, the A without a bar, the l a mast that sends a
// wave to both sides.
//
// Run from this folder: go run icon_gen.go          add a file name to also get a sheet of the icons to look at
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type pt struct{ x, y float64 }

// A stroke is one line of a drawing: as points to join, and as SVG path data (Android's vectors read the same).
type stroke struct {
	pts            []pt
	d              string
	x0, y0, x1, y1 float64 // the box around the points
}

type layer []stroke

func (l layer) path() string {
	var d []string
	for _, s := range l {
		d = append(d, s.d)
	}
	return strings.Join(d, "")
}

func made(pts []pt, d string) stroke {
	s := stroke{pts: pts, d: d, x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
	for _, p := range pts {
		s.x0, s.y0, s.x1, s.y1 = math.Min(s.x0, p.x), math.Min(s.y0, p.y), math.Max(s.x1, p.x), math.Max(s.y1, p.y)
	}
	return s
}

func num(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func at(p pt) string { return num(p.x) + " " + num(p.y) }

func line(a, b pt) stroke { return made([]pt{a, b}, "M"+at(a)+"L"+at(b)) }

// arcPoints walks the circle around (cx, cy) from angle a0 to a1, in degrees: 0 is right, 90 is down, as on a screen.
func arcPoints(cx, cy, r, a0, a1 float64) []pt {
	steps := int(math.Abs(a1-a0)/4) + 2
	p := make([]pt, 0, steps+1)
	for i := 0; i <= steps; i++ {
		a := (a0 + (a1-a0)*float64(i)/float64(steps)) * math.Pi / 180
		p = append(p, pt{cx + r*math.Cos(a), cy + r*math.Sin(a)})
	}
	return p
}

// arcTo is the SVG command for an arc of less than a full turn that ends at p.
func arcTo(r, a0, a1 float64, p pt) string {
	large, sweep := 0, 0
	if math.Abs(a1-a0) > 180 {
		large = 1
	}
	if a1 > a0 {
		sweep = 1
	}
	return fmt.Sprintf("A%s %s 0 %d %d %s", num(r), num(r), large, sweep, at(p))
}

func arc(cx, cy, r, a0, a1 float64) stroke {
	p := arcPoints(cx, cy, r, a0, a1)
	return made(p, "M"+at(p[0])+arcTo(r, a0, a1, p[len(p)-1]))
}

// ---- the hotspot sign, on a grid of 24 ----

const (
	signX, signY = 12.0, 13.4 // the dot; the sign as a whole is centred on (12, 12)
	signDot      = 2.0        // the dot's radius
	signLine     = 1.7        // how wide the arcs are drawn where there is room

	teal, tealOnClear = "#0B6B80", "#1E98B3" // the app's colour; a lighter one that shows on light and on dark grounds
)

var (
	// Two arcs around the dot, from lower left over the top to lower right.
	signArcs = layer{arc(signX, signY, 5.6, 135, 405), arc(signX, signY, 9.6, 135, 405)}
	// The dot: a line too short to see, drawn with round ends as wide as the dot.
	signPoint = layer{line(pt{signX - 0.01, signY}, pt{signX + 0.01, signY})}
)

// ---- the word, for the notifications: 82 units wide, its letters stand on y = 48 and reach up to y = 12 ----

const (
	wordX, wordY = 51.0, 25.5 // its middle
	wordW        = 82.0
	mastX        = 56.0
)

// letterA is an A without a bar whose tip is rounded with radius r: its feet at (x0, bottom) and (x1, bottom), its
// highest point at top.
func letterA(x0, x1, top, bottom, r float64) stroke {
	xm, half := (x0+x1)/2, (x1-x0)/2
	meet := top // where the straight legs would meet, above the rounded tip
	var lean float64
	for i := 0; i < 50; i++ {
		lean = math.Atan2(half, bottom-meet)
		meet = top - r*(1/math.Sin(lean)-1)
	}
	deg := lean * 180 / math.Pi
	tip := arcPoints(xm, top+r, r, 180+deg, 360-deg)
	p := append(append([]pt{{x0, bottom}}, tip...), pt{x1, bottom})
	return made(p, "M"+at(p[0])+"L"+at(tip[0])+arcTo(r, 180+deg, 360-deg, tip[len(tip)-1])+"L"+at(pt{x1, bottom}))
}

// letterE is a round e: its bar, then the bowl from the bar's right end over the top and around to the lower right.
func letterE(cx, cy, r float64) stroke {
	bowl := arcPoints(cx, cy, r, 0, -320)
	p := append([]pt{{cx - r, cy}}, bowl...)
	return made(p, "M"+at(p[0])+"L"+at(bowl[0])+arcTo(r, 0, -320, bowl[len(bowl)-1]))
}

// The A, the mast with one wave to each side of its top, the e.
var word = layer{
	letterA(10, 38, 12, 48, 4),
	line(pt{mastX, 12}, pt{mastX, 48}), arc(mastX, 13, 10, -50, 50), arc(mastX, 13, 10, 230, 130),
	letterE(81, 37, 11),
}

// ---- pictures (the .ico files) ----

func (l layer) distance(p pt, reach float64) float64 {
	best := math.Inf(1)
	for _, s := range l {
		if p.x < s.x0-reach || p.x > s.x1+reach || p.y < s.y0-reach || p.y > s.y1+reach {
			continue
		}
		for i := 1; i < len(s.pts); i++ {
			a, b := s.pts[i-1], s.pts[i]
			dx, dy := b.x-a.x, b.y-a.y
			t := 0.0
			if n := dx*dx + dy*dy; n > 0 {
				t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/n))
			}
			best = math.Min(best, math.Hypot(p.x-a.x-t*dx, p.y-a.y-t*dy))
		}
	}
	return best
}

type part struct {
	l     layer
	width float64
}

// render draws parts in one colour on a clear ground, size pixels wide and high, showing span units of the drawing
// around (mx, my).
func render(size int, c color.NRGBA, mx, my, span float64, parts ...part) *image.NRGBA {
	scale := float64(size) / span // pixels per unit
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			p := pt{mx + (float64(x)+.5-float64(size)/2)/scale, my + (float64(y)+.5-float64(size)/2)/scale}
			clear := 1.0
			for _, part := range parts {
				d := part.l.distance(p, part.width/2+1/scale)
				clear *= 1 - math.Max(0, math.Min(1, .5+(part.width/2-d)*scale))
			}
			if clear < 1 {
				img.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8(math.Round(255 * (1 - clear)))})
			}
		}
	}
	return img
}

// picture is the hotspot sign, size pixels wide and high. Its arcs never get thinner than a good pixel, so the
// small pictures stay sharp.
func picture(size int, c color.NRGBA) *image.NRGBA {
	w := math.Max(signLine, 1.3*24/float64(size))
	return render(size, c, 12, 12, 24, part{signArcs, w}, part{signPoint, math.Max(2*signDot, 2*w)})
}

var icoSizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

func writeIcon(name string, c color.NRGBA) {
	var images [][]byte
	for _, s := range icoSizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, picture(s, c)); err != nil {
			panic(err)
		}
		images = append(images, buf.Bytes())
	}
	// ICO: a header, one directory entry per size, then the PNGs.
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(icoSizes))})
	offset := 6 + 16*len(icoSizes)
	for i, s := range icoSizes {
		side := uint8(s)
		if s >= 256 {
			side = 0
		}
		binary.Write(&out, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, Bits           uint16
			Size, Offset           uint32
		}{side, side, 0, 0, 1, 32, uint32(len(images[i])), uint32(offset)})
		offset += len(images[i])
	}
	for _, im := range images {
		out.Write(im)
	}
	write(name, out.Bytes())
}

// ---- vectors (Android's drawables, the README's picture, the window's symbol) ----

type vpath struct {
	l      layer
	colour string
	width  float64
}

// vector writes an Android vector drawable, dp wide and high, that shows the drawing around (mx, my) at the given
// scale (dp per unit); the lines' widths are in the drawing's units.
func vector(file, about string, dp, mx, my, scale float64, paths ...vpath) {
	var b strings.Builder
	fmt.Fprintf(&b, "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!-- %s Written by core/cmd/hotspot-pc/winres/icon_gen.go: change the drawing there. -->\n", about)
	fmt.Fprintf(&b, "<vector xmlns:android=\"http://schemas.android.com/apk/res/android\"\n    android:width=\"%sdp\"\n    android:height=\"%sdp\"\n    android:viewportWidth=\"%s\"\n    android:viewportHeight=\"%s\">\n", num(dp), num(dp), num(dp), num(dp))
	fmt.Fprintf(&b, "    <group\n        android:scaleX=\"%s\"\n        android:scaleY=\"%s\"\n        android:translateX=\"%s\"\n        android:translateY=\"%s\">\n", num(scale), num(scale), num(dp/2-mx*scale), num(dp/2-my*scale))
	for _, p := range paths {
		fmt.Fprintf(&b, "        <path\n            android:fillColor=\"#00000000\"\n            android:pathData=\"%s\"\n", p.l.path())
		fmt.Fprintf(&b, "            android:strokeColor=\"%s\"\n            android:strokeLineCap=\"round\"\n            android:strokeLineJoin=\"round\"\n            android:strokeWidth=\"%s\" />\n", p.colour, num(p.width))
	}
	b.WriteString("    </group>\n</vector>\n")
	write(file, []byte(b.String()))
}

// sign is the hotspot sign in one colour.
func sign(colour string) []vpath {
	return []vpath{{signArcs, colour, signLine}, {signPoint, colour, 2 * signDot}}
}

func write(file string, data []byte) {
	if err := os.WriteFile(file, data, 0o644); err != nil {
		panic(err)
	}
	fmt.Println("wrote", file)
}

func main() {
	on, off := color.NRGBA{0x1E, 0x98, 0xB3, 0xFF}, color.NRGBA{0x8A, 0x93, 0x99, 0xFF}
	writeIcon("icon.ico", on)
	writeIcon("icon-off.ico", off)

	root := filepath.Join("..", "..", "..", "..")
	res := filepath.Join(root, "android", "app", "src", "main", "res", "drawable")
	// The launcher shows the middle 72 of the 108 units and may cut it to a circle of 66: the sign is 46 across.
	const launcher = 2.2
	vector(filepath.Join(res, "ic_launcher_foreground.xml"), "The launcher icon: the hotspot sign, a dot with two arcs around it.", 108, 12, 12, launcher, sign(teal)...)
	vector(filepath.Join(res, "ic_launcher_monochrome.xml"), "Themed-icon version of the launcher icon: the same sign, the system colours it.", 108, 12, 12, launcher, sign("#FF000000")...)
	write(filepath.Join(res, "ic_launcher_background.xml"), []byte("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<!-- The launcher icon's ground: white, the sign is drawn in the app's colour. Written by core/cmd/hotspot-pc/winres/icon_gen.go. -->\n<vector xmlns:android=\"http://schemas.android.com/apk/res/android\"\n    android:width=\"108dp\"\n    android:height=\"108dp\"\n    android:viewportWidth=\"108\"\n    android:viewportHeight=\"108\">\n    <path\n        android:fillColor=\"#FFFFFF\"\n        android:pathData=\"M0,0h108v108h-108z\" />\n</vector>\n"))
	vector(filepath.Join(res, "ic_hotspot.xml"), "The hotspot sign as a white mask: the Quick Settings tile, the widget while sharing is on.", 24, 12, 12, 1, sign("#FFFFFFFF")...)
	vector(filepath.Join(res, "ic_widget_logo.xml"), "The hotspot sign in the app's colour: the widget while sharing is off.", 24, 12, 12, 1, sign("@color/widget_icon")...)
	vector(filepath.Join(res, "ic_logo.xml"), "The hotspot sign at the top of the app; the app tints it.", 24, 12, 12, 1, sign("#FF000000")...)
	vector(filepath.Join(res, "ic_stat_hotspot.xml"), "The notifications' icon in the status bar: the name, Ale, as a white mask.", 24, wordX, wordY, 22/(wordW+5.6), vpath{word, "#FFFFFFFF", 5.6})

	dot := fmt.Sprintf("<circle cx=\"%s\" cy=\"%s\" r=\"%s\" fill=\"%%s\" stroke=\"none\"/>", num(signX), num(signY), num(signDot))
	write(filepath.Join(root, "logo.svg"), []byte(fmt.Sprintf("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\" width=\"192\" height=\"192\" fill=\"none\" stroke=\"%s\" stroke-width=\"%s\" stroke-linecap=\"round\">\n  <title>AleShare</title>\n  <path d=\"%s\"/>\n  %s\n</svg>\n", tealOnClear, num(signLine), signArcs.path(), fmt.Sprintf(dot, tealOnClear))))

	fmt.Printf("\nui/index.html, the header's symbol:\n    <symbol id=\"logo\" viewBox=\"0 0 24 24\"><path d=\"%s\"/>%s</symbol>\n", signArcs.path(), fmt.Sprintf(dot, "currentColor"))

	if len(os.Args) > 1 { // a sheet to look at: every size on a light and on a dark ground, the small ones also enlarged
		sheet(os.Args[1], on, off)
	}
}

func sheet(file string, on, off color.NRGBA) {
	grounds := []color.NRGBA{{0xF3, 0xF3, 0xF3, 0xFF}, {0x20, 0x20, 0x20, 0xFF}}
	const rowH, width = 276, 1180
	img := image.NewNRGBA(image.Rect(0, 0, width, rowH*len(grounds)))
	put := func(src *image.NRGBA, ox, oy, zoom int, ground color.NRGBA) {
		for y := 0; y < src.Bounds().Dy()*zoom; y++ {
			for x := 0; x < src.Bounds().Dx()*zoom; x++ {
				c := src.NRGBAAt(x/zoom, y/zoom)
				a := float64(c.A) / 255
				mix := func(g, f uint8) uint8 { return uint8(float64(g)*(1-a) + float64(f)*a) }
				img.SetNRGBA(ox+x, oy+y, color.NRGBA{mix(ground.R, c.R), mix(ground.G, c.G), mix(ground.B, c.B), 255})
			}
		}
	}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	for row, ground := range grounds {
		for y := row * rowH; y < (row+1)*rowH; y++ {
			for x := 0; x < width; x++ {
				img.SetNRGBA(x, y, ground)
			}
		}
		oy := row*rowH + 10
		put(picture(256, on), 10, oy, 1, ground)
		x := 280
		for _, s := range []int{64, 48, 40, 32, 24, 20, 16} {
			put(picture(s, on), x, oy, 1, ground)
			x += s + 10
		}
		put(picture(24, off), x+10, oy, 1, ground)
		put(picture(16, off), x+44, oy, 1, ground)
		x = 280
		for _, s := range []int{32, 24, 20, 16} {
			put(picture(s, on), x, oy+80, 4, ground)
			x += s*4 + 12
		}
		// the notifications' word, as the status bar shows it (white on dark), at 24 and at 72 pixels
		dark := grounds[1]
		letters := func(size int) *image.NRGBA {
			return render(size, white, wordX, wordY, 24*(wordW+5.6)/22, part{word, 5.6})
		}
		put(letters(24), x+10, oy+80, 1, dark)
		put(letters(72), x+44, oy+80, 1, dark)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	write(file, buf.Bytes())
}
