//go:build !cli

package ui

import (
	"image"
	"math"
	"path/filepath"
	"strings"

	"disklens/app/utils"

	"github.com/egoist/mygo/ui"
)

type cushion struct {
	xSlope, ySlope, xOffset, yOffset float64
	height                           float64
}

var cushionPalette = func() []ui.Color {
	colors := []ui.Color{ui.Hex("#0000ff"), ui.Hex("#ff0000"), ui.Hex("#00ff00"), ui.Hex("#ffff00"), ui.Hex("#00ffff"), ui.Hex("#ff00ff"), ui.Hex("#ffaa00"), ui.Hex("#0055ff"), ui.Hex("#ff0055"), ui.Hex("#55ff00"), ui.Hex("#aa00ff"), ui.Hex("#00ff55"), ui.Hex("#ff00aa"), ui.Hex("#00aaff"), ui.Hex("#ff5500"), ui.Hex("#00ffaa"), ui.Hex("#5500ff"), ui.Hex("#ffffff")}
	for i, color := range colors {
		factor := 0.6 * 3 * 255 / float64(int(color.R)+int(color.G)+int(color.B))
		r, g, b := normalizeCushionColor(int(float64(color.R)*factor), int(float64(color.G)*factor), int(float64(color.B)*factor))
		colors[i].R, colors[i].G, colors[i].B = r, g, b
	}
	return colors
}()

func normalizeCushionColor(red, green, blue int) (uint8, uint8, uint8) {
	channels := [3]int{red, green, blue}
	for i := range channels {
		if channels[i] <= 255 {
			continue
		}
		j, k := (i+1)%3, (i+2)%3
		excess := (channels[i] - 255) / 2
		channels[i] = 255
		channels[j] += excess
		channels[k] += excess
		if channels[j] > 255 {
			channels[k] += channels[j] - 255
			channels[j] = 255
		} else if channels[k] > 255 {
			channels[j] += channels[k] - 255
			channels[k] = 255
		}
		break
	}
	return uint8(channels[0]), uint8(channels[1]), uint8(channels[2])
}

func shadeMap(tiles []tile, width, height float32, colors map[string]ui.Color) *ui.Bitmap {
	pixels := shadePixels(tiles, width, height, colors)
	if pixels == nil {
		return nil
	}
	return ui.NewBitmap(pixels)
}

func shadePixels(tiles []tile, width, height float32, colors map[string]ui.Color) *image.RGBA {
	w, h := int(math.Ceil(float64(width))), int(math.Ceil(float64(height)))
	if w < 1 || h < 1 {
		return nil
	}
	pixels := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(pixels.Pix); i += 4 {
		pixels.Pix[i], pixels.Pix[i+1], pixels.Pix[i+2], pixels.Pix[i+3] = 37, 37, 37, 255
	}
	surfaces := make(map[*utils.Node]cushion)
	for _, t := range tiles {
		r := t.rect
		surface, hasParent := surfaces[t.node.Parent]
		if hasParent {
			surface.height *= 0.91
		} else {
			surface.height = 0.38
		}
		if hasParent {
			sx, sy := 8*surface.height/float64(r.W), 8*surface.height/float64(r.H)
			surface.xSlope += sx
			surface.ySlope += sy
			surface.xOffset -= sx * float64(r.X+r.W/2)
			surface.yOffset -= sy * float64(r.Y+r.H/2)
		}
		if t.branch {
			surfaces[t.node] = surface
			continue
		}
		base, ok := colors[strings.ToLower(filepath.Ext(t.node.Name))]
		if !ok || t.node.Dir {
			base = cushionPalette[len(cushionPalette)-1]
		}
		x0, y0 := max(0, int(math.Round(float64(r.X)))), max(0, int(math.Round(float64(r.Y))))
		x1, y1 := min(w, int(math.Round(float64(r.X+r.W)))), min(h, int(math.Round(float64(r.Y+r.H))))
		for y := y0; y < y1; y++ {
			ny := surface.ySlope*(float64(y)+0.5) + surface.yOffset
			for x := x0; x < x1; x++ {
				nx := surface.xSlope*(float64(x)+0.5) + surface.xOffset
				light := max(0, (10-nx-ny)/math.Sqrt(102*(nx*nx+ny*ny+1)))
				intensity := (0.13 + 0.87*light) * (0.88 / 0.6)
				red, green, blue := normalizeCushionColor(int(float64(base.R)*intensity), int(float64(base.G)*intensity), int(float64(base.B)*intensity))
				i := y*pixels.Stride + x*4
				pixels.Pix[i] = red
				pixels.Pix[i+1] = green
				pixels.Pix[i+2] = blue
			}
		}
	}
	return pixels
}
