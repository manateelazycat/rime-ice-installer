package fcitx

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"path/filepath"

	"rime-ice-installer/internal/system"
)

const customThemeConf = `[Metadata]
Name[zh_CN]=安装器黑色科幻
Name[zh_TW]=安裝器黑色科幻
Name=Installer Dark Sci-Fi
Version=1
Author=rime-ice-installer
Description=Black sci-fi theme for Fcitx5
ScaleWithDPI=True

[InputPanel]
NormalColor=#d7dde5
HighlightCandidateColor=#f4f7fb
HighlightColor=#d2d9e2
HighlightBackgroundColor=#27303a

[InputPanel/TextMargin]
Left=7
Right=7
Top=5
Bottom=5

[InputPanel/ContentMargin]
Left=3
Right=3
Top=3
Bottom=3

[InputPanel/Background]
Color=#090c11
BorderColor=#090c11
BorderWidth=0

[InputPanel/Background/Margin]
Left=2
Right=2
Top=2
Bottom=2

[InputPanel/Highlight]
Color=#1b2129

[InputPanel/Highlight/Margin]
Left=6
Right=6
Top=4
Bottom=4

[Menu]
NormalColor=#d7dde5
HighlightCandidateColor=#f4f7fb

[Menu/Background]
Color=#0b0f14
BorderColor=#56606c
BorderWidth=1

[Menu/Background/Margin]
Left=2
Right=2
Top=2
Bottom=2

[Menu/ContentMargin]
Left=3
Right=3
Top=3
Bottom=3

[Menu/CheckBox]
Image=radio.png

[Menu/Highlight]
Color=#202730

[Menu/Highlight/Margin]
Left=6
Right=6
Top=4
Bottom=4

[Menu/Separator]
Color=#3b4550

[Menu/TextMargin]
Left=6
Right=6
Top=5
Bottom=5
`

var (
	iconShadow = color.NRGBA{R: 0x48, G: 0x52, B: 0x5d, A: 0xb0}
	iconBody   = color.NRGBA{R: 0xd6, G: 0xde, B: 0xe7, A: 0xff}
	iconGlow   = color.NRGBA{R: 0xf6, G: 0xf9, B: 0xfc, A: 0xff}
	iconRidge  = color.NRGBA{R: 0xa2, G: 0xad, B: 0xb8, A: 0xcc}
)

type themeAsset struct {
	name string
	data []byte
}

func writeCustomThemeAssets(targetDir string) error {
	assets, err := buildCustomThemeAssets()
	if err != nil {
		return err
	}
	for _, asset := range assets {
		path := filepath.Join(targetDir, asset.name)
		if err := system.WriteFileAtomic(path, asset.data, 0o644); err != nil {
			return fmt.Errorf("写入主题资源失败 %s: %w", path, err)
		}
	}
	return nil
}

func buildCustomThemeAssets() ([]themeAsset, error) {
	icons, err := buildThemeIcons()
	if err != nil {
		return nil, err
	}

	assets := []themeAsset{{
		name: "theme.conf",
		data: []byte(customThemeConf),
	}}
	assets = append(assets, icons...)
	return assets, nil
}

func buildThemeIcons() ([]themeAsset, error) {
	renderers := []struct {
		name   string
		render func() image.Image
	}{
		{name: "prev.png", render: renderPrevPageIcon},
		{name: "next.png", render: renderNextPageIcon},
		{name: "arrow.png", render: renderSubMenuIcon},
		{name: "radio.png", render: renderRadioIcon},
	}

	assets := make([]themeAsset, 0, len(renderers))
	for _, item := range renderers {
		data, err := encodePNG(item.render())
		if err != nil {
			return nil, fmt.Errorf("生成主题图标失败 %s: %w", item.name, err)
		}
		assets = append(assets, themeAsset{
			name: item.name,
			data: data,
		})
	}
	return assets, nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderPrevPageIcon() image.Image {
	return renderPageArrowIcon(true)
}

func renderNextPageIcon() image.Image {
	return renderPageArrowIcon(false)
}

func renderSubMenuIcon() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 6, 12))
	drawSegment(img, image.Pt(1, 1), image.Pt(4, 6), 1.3, iconShadow)
	drawSegment(img, image.Pt(1, 11), image.Pt(4, 6), 1.3, iconShadow)
	drawSegment(img, image.Pt(0, 0), image.Pt(3, 6), 0.95, iconBody)
	drawSegment(img, image.Pt(0, 12), image.Pt(3, 6), 0.95, iconBody)
	drawSegment(img, image.Pt(1, 2), image.Pt(3, 6), 0.4, iconGlow)
	drawSegment(img, image.Pt(1, 10), image.Pt(3, 6), 0.4, iconGlow)
	setPixel(img, 4, 6, iconGlow)
	return img
}

func renderPageArrowIcon(left bool) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 24))
	front := chevronPoints{
		top:    image.Pt(6, 4),
		tip:    image.Pt(2, 12),
		bottom: image.Pt(6, 20),
	}
	back := chevronPoints{
		top:    image.Pt(13, 4),
		tip:    image.Pt(9, 12),
		bottom: image.Pt(13, 20),
	}
	if !left {
		front = chevronPoints{
			top:    image.Pt(9, 4),
			tip:    image.Pt(13, 12),
			bottom: image.Pt(9, 20),
		}
		back = chevronPoints{
			top:    image.Pt(2, 4),
			tip:    image.Pt(6, 12),
			bottom: image.Pt(2, 20),
		}
	}

	drawDoubleChevron(img, back)
	drawDoubleChevron(img, front)
	return img
}

type chevronPoints struct {
	top    image.Point
	tip    image.Point
	bottom image.Point
}

func drawDoubleChevron(img *image.NRGBA, points chevronPoints) {
	shadowOffset := image.Pt(1, 1)
	drawSegment(img, points.top.Add(shadowOffset), points.tip.Add(shadowOffset), 1.9, iconShadow)
	drawSegment(img, points.bottom.Add(shadowOffset), points.tip.Add(shadowOffset), 1.9, iconShadow)

	drawSegment(img, points.top, points.tip, 1.5, iconBody)
	drawSegment(img, points.bottom, points.tip, 1.5, iconBody)

	drawSegment(img, points.top, points.tip, 0.75, iconRidge)
	drawSegment(img, points.bottom, points.tip, 0.75, iconRidge)

	highlightTop := interpolatePoint(points.top, points.tip, 0.55)
	highlightBottom := interpolatePoint(points.bottom, points.tip, 0.55)
	drawSegment(img, highlightTop, points.tip, 0.35, iconGlow)
	drawSegment(img, highlightBottom, points.tip, 0.35, iconGlow)
	setPixel(img, points.tip.X, points.tip.Y, iconGlow)
}

func renderRadioIcon() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	center := 12.0
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			dx := float64(x) + 0.5 - center
			dy := float64(y) + 0.5 - center
			dist := math.Hypot(dx, dy)
			switch {
			case dist >= 8.4 && dist <= 10.0:
				img.SetNRGBA(x, y, iconShadow)
			case dist >= 6.1 && dist <= 8.2:
				img.SetNRGBA(x, y, iconBody)
			case dist <= 2.8:
				img.SetNRGBA(x, y, iconGlow)
			}
		}
	}
	return img
}

func drawSegment(img *image.NRGBA, from, to image.Point, radius float64, col color.NRGBA) {
	minX := min(from.X, to.X) - int(math.Ceil(radius)) - 1
	maxX := max(from.X, to.X) + int(math.Ceil(radius)) + 1
	minY := min(from.Y, to.Y) - int(math.Ceil(radius)) - 1
	maxY := max(from.Y, to.Y) + int(math.Ceil(radius)) + 1

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if !image.Pt(x, y).In(img.Rect) {
				continue
			}
			if distanceToSegment(float64(x)+0.5, float64(y)+0.5, from, to) <= radius {
				img.SetNRGBA(x, y, col)
			}
		}
	}
}

func interpolatePoint(from, to image.Point, ratio float64) image.Point {
	x := float64(from.X) + (float64(to.X)-float64(from.X))*ratio
	y := float64(from.Y) + (float64(to.Y)-float64(from.Y))*ratio
	return image.Pt(int(math.Round(x)), int(math.Round(y)))
}

func distanceToSegment(px, py float64, from, to image.Point) float64 {
	ax := float64(from.X)
	ay := float64(from.Y)
	bx := float64(to.X)
	by := float64(to.Y)
	dx := bx - ax
	dy := by - ay
	if dx == 0 && dy == 0 {
		return math.Hypot(px-ax, py-ay)
	}

	projection := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	switch {
	case projection < 0:
		return math.Hypot(px-ax, py-ay)
	case projection > 1:
		return math.Hypot(px-bx, py-by)
	default:
		closestX := ax + projection*dx
		closestY := ay + projection*dy
		return math.Hypot(px-closestX, py-closestY)
	}
}

func setPixel(img *image.NRGBA, x, y int, col color.NRGBA) {
	if image.Pt(x, y).In(img.Rect) {
		img.SetNRGBA(x, y, col)
	}
}

func min(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func max(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}
