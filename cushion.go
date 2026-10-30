package main

import (
	"image"
	"math"

	"github.com/egoist/mygo/ui"
)

type cushion struct {
	xSlope, ySlope, xOffset, yOffset float64
	height                           float64
}

func shadeMap(tiles []tile, width, height float32) *ui.Bitmap {
	w, h := int(math.Ceil(float64(width))), int(math.Ceil(float64(height)))
	if w < 1 || h < 1 {
		return nil
	}
	pixels := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(pixels.Pix); i += 4 {
		pixels.Pix[i], pixels.Pix[i+1], pixels.Pix[i+2], pixels.Pix[i+3] = 37, 37, 37, 255
	}
	surfaces := make(map[*Node]cushion)
	for _, t := range tiles {
		r := t.rect
		surface, hasParent := surfaces[t.node.Parent]
		if hasParent {
			surface.height *= 0.91
		} else {
			surface.height = 0.38
		}
		sx, sy := 8*surface.height/float64(r.W), 8*surface.height/float64(r.H)
		surface.xSlope += sx
		surface.ySlope += sy
		surface.xOffset -= sx * float64(r.X+r.W/2)
		surface.yOffset -= sy * float64(r.Y+r.H/2)
		if t.branch {
			surfaces[t.node] = surface
			continue
		}
		base := nodeColor(t.node)
		x0, y0 := max(0, int(math.Round(float64(r.X)))), max(0, int(math.Round(float64(r.Y))))
		x1, y1 := min(w, int(math.Round(float64(r.X+r.W)))), min(h, int(math.Round(float64(r.Y+r.H))))
		for y := y0; y < y1; y++ {
			ny := surface.ySlope*(float64(y)+0.5) + surface.yOffset
			for x := x0; x < x1; x++ {
				nx := surface.xSlope*(float64(x)+0.5) + surface.xOffset
				light := max(0, (10-nx-ny)/math.Sqrt(102*(nx*nx+ny*ny+1)))
				intensity := (0.13 + 0.87*light) * 1.65
				red, green, blue := float64(base.R)*intensity, float64(base.G)*intensity, float64(base.B)*intensity
				excess := max(0, max(red, max(green, blue))-255) * 0.65
				i := y*pixels.Stride + x*4
				pixels.Pix[i] = uint8(min(255, red+excess))
				pixels.Pix[i+1] = uint8(min(255, green+excess))
				pixels.Pix[i+2] = uint8(min(255, blue+excess))
			}
		}
	}
	return ui.NewBitmap(pixels)
}
