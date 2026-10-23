// SPDX-License-Identifier: Apache-2.0
package tooling

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type point [2]float64
type contours [][]point

func sampleContours(loops contours) contours {
	result := make(contours, len(loops))
	for n, loop := range loops {
		for i, p := range loop {
			previous, next := loop[(i+len(loop)-1)%len(loop)], loop[(i+1)%len(loop)]
			for step := 0; step < 8; step++ {
				t := float64(step) / 8
				u := 1 - t
				result[n] = append(result[n], point{u*u*(previous[0]+p[0])/2 + 2*u*t*p[0] + t*t*(p[0]+next[0])/2, u*u*(previous[1]+p[1])/2 + 2*u*t*p[1] + t*t*(p[1]+next[1])/2})
			}
		}
	}
	return result
}

func fillContours(img *image.NRGBA, loops contours, ink color.NRGBA) {
	scale := float64(img.Bounds().Dx()) / 64
	for y := 0; y < img.Bounds().Dy(); y++ {
		scanY := (float64(y) + 0.5) / scale
		var crossings []float64
		for _, loop := range loops {
			for i, p := range loop {
				q := loop[(i+1)%len(loop)]
				if (p[1] > scanY) != (q[1] > scanY) {
					crossings = append(crossings, (p[0]+(scanY-p[1])*(q[0]-p[0])/(q[1]-p[1]))*scale)
				}
			}
		}
		sort.Float64s(crossings)
		for i := 0; i+1 < len(crossings); i += 2 {
			for x := max(0, int(math.Ceil(crossings[i]-0.5))); x < min(img.Bounds().Dx(), int(math.Ceil(crossings[i+1]-0.5))); x++ {
				img.SetNRGBA(x, y, ink)
			}
		}
	}
}

func rasterIcon(loops contours) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	white := color.NRGBA{255, 255, 255, 255}
	sampled := sampleContours(loops)
	fillContours(img, contours{sampled[0]}, white)
	// The original artwork uses a 2.2-unit white outer halo.
	outer := sampled[0]
	radius := 1.1 * 16
	for i, p := range outer {
		q := outer[(i+1)%len(outer)]
		steps := max(1, int(math.Hypot(q[0]-p[0], q[1]-p[1])*32))
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			cx, cy := (p[0]+t*(q[0]-p[0]))*16, (p[1]+t*(q[1]-p[1]))*16
			for y := max(0, int(cy-radius)); y < min(1024, int(cy+radius)+1); y++ {
				dy := float64(y) + 0.5 - cy
				dx := math.Sqrt(math.Max(0, radius*radius-dy*dy))
				for x := max(0, int(cx-dx)); x < min(1024, int(cx+dx)+1); x++ {
					img.SetNRGBA(x, y, white)
				}
			}
		}
	}
	fillContours(img, sampled, color.NRGBA{23, 23, 23, 255})
	return img
}

func resizeIcon(src *image.NRGBA, size int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	ratio := float64(src.Bounds().Dx()) / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var a, r, g, b float64
			left, right, top, bottom := float64(x)*ratio, float64(x+1)*ratio, float64(y)*ratio, float64(y+1)*ratio
			for sy := int(top); sy < int(math.Ceil(bottom)); sy++ {
				for sx := int(left); sx < int(math.Ceil(right)); sx++ {
					weight := (math.Min(right, float64(sx+1)) - math.Max(left, float64(sx))) * (math.Min(bottom, float64(sy+1)) - math.Max(top, float64(sy)))
					c := src.NRGBAAt(sx, sy)
					alpha := float64(c.A) * weight
					a += alpha
					r += float64(c.R) * alpha
					g += float64(c.G) * alpha
					b += float64(c.B) * alpha
				}
			}
			if a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{uint8(r/a + 0.5), uint8(g/a + 0.5), uint8(b/a + 0.5), uint8(a/(ratio*ratio) + 0.5)})
			}
		}
	}
	return out
}

func pngBytes(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return b.Bytes(), err
}
func icoBytes(src *image.NRGBA) ([]byte, error) {
	sizes := []int{16, 24, 32, 48, 64, 256}
	var output bytes.Buffer
	_ = binary.Write(&output, binary.LittleEndian, []uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	var images [][]byte
	for _, size := range sizes {
		data, err := pngBytes(resizeIcon(src, size))
		if err != nil {
			return nil, err
		}
		images = append(images, data)
		width := byte(size)
		output.Write([]byte{width, width, 0, 0})
		_ = binary.Write(&output, binary.LittleEndian, []uint16{1, 32})
		_ = binary.Write(&output, binary.LittleEndian, []uint32{uint32(len(data)), uint32(offset)})
		offset += len(data)
	}
	for _, data := range images {
		output.Write(data)
	}
	return output.Bytes(), nil
}

func svgMarkup(loops contours) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><path fill="currentColor" fill-rule="evenodd" d="`)
	for _, loop := range loops {
		p, q := loop[len(loop)-1], loop[0]
		fmt.Fprintf(&b, "M%.2f %.2f", (p[0]+q[0])/2, (p[1]+q[1])/2)
		for i, p := range loop {
			q := loop[(i+1)%len(loop)]
			fmt.Fprintf(&b, "Q%.2f %.2f %.2f %.2f", p[0], p[1], (p[0]+q[0])/2, (p[1]+q[1])/2)
		}
		b.WriteString("Z ")
	}
	b.WriteString(`"/></svg>`)
	return b.String() + "\n"
}

// GenerateIcons preserves the original vector coordinates and writes a new output directory.
func GenerateIcons(vectors, output string) error {
	data, err := os.ReadFile(vectors)
	if err != nil {
		return err
	}
	var shapes map[string]contours
	if err := json.Unmarshal(data, &shapes); err != nil {
		return err
	}
	for _, key := range []string{"sleeping", "ready", "running"} {
		loops := shapes[key]
		if len(loops) == 0 {
			return fmt.Errorf("missing %s artwork", key)
		}
		for _, loop := range loops {
			if len(loop) < 3 {
				return fmt.Errorf("invalid %s contour", key)
			}
			for _, p := range loop {
				if math.IsNaN(p[0]) || math.IsNaN(p[1]) || p[0] < 0 || p[0] > 64 || p[1] < 0 || p[1] > 64 {
					return fmt.Errorf("invalid %s coordinate", key)
				}
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return err
	}
	rendered := map[string]*image.NRGBA{}
	for _, key := range []string{"sleeping", "ready", "running"} {
		rendered[key] = rasterIcon(shapes[key])
	}
	states := []string{"sleeping", "ready", "running", "starting", "offline"}
	preview := image.NewNRGBA(image.Rect(0, 0, 960, 360))
	draw.Draw(preview, preview.Bounds(), image.NewUniform(color.NRGBA{245, 243, 236, 255}), image.Point{}, draw.Src)
	for index, state := range states {
		key := state
		if state == "offline" {
			key = "sleeping"
		}
		if state == "starting" {
			key = "ready"
		}
		src := rendered[key]
		pngData, err := pngBytes(src)
		if err != nil {
			return err
		}
		ico, err := icoBytes(src)
		if err != nil {
			return err
		}
		for name, data := range map[string][]byte{"watchdog-" + state + ".png": pngData, "watchdog-" + state + ".ico": ico, "watchdog-" + state + ".svg": []byte(svgMarkup(shapes[key]))} {
			if err := writeNew(filepath.Join(output, name), data); err != nil {
				return err
			}
		}
		draw.Draw(preview, image.Rect(index*192+20, 12, index*192+172, 164), resizeIcon(src, 152), image.Point{}, draw.Over)
		for i, size := range []int{16, 24, 32} {
			x := index*192 + 24 + i*51
			draw.Draw(preview, image.Rect(x, 231, x+size, 231+size), resizeIcon(src, size), image.Point{}, draw.Over)
		}
		rect := image.Rect(index*192, 306, (index+1)*192, 360)
		draw.Draw(preview, rect, image.NewUniform(color.NRGBA{36, 38, 32, 255}), image.Point{}, draw.Src)
		draw.Draw(preview, image.Rect(index*192+80, 316, index*192+112, 348), resizeIcon(src, 32), image.Point{}, draw.Over)
	}
	guardian, err := pngBytes(rendered["ready"])
	if err != nil {
		return err
	}
	if err := writeNew(filepath.Join(output, "guardian-1024.png"), guardian); err != nil {
		return err
	}
	var icns bytes.Buffer
	icns.WriteString("icns")
	_ = binary.Write(&icns, binary.BigEndian, uint32(16+len(guardian)))
	icns.WriteString("ic10")
	_ = binary.Write(&icns, binary.BigEndian, uint32(8+len(guardian)))
	icns.Write(guardian)
	if err := writeNew(filepath.Join(output, "guardian.icns"), icns.Bytes()); err != nil {
		return err
	}
	p, err := pngBytes(preview)
	if err != nil {
		return err
	}
	return writeNew(filepath.Join(output, "watchdog-states-preview.png"), p)
}
