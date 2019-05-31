package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/paulmach/go.geojson"
	"github.com/pkg/errors"
	"golang.org/x/image/font/gofont/goregular"
)

var (
	geoName    string
	styleName  string
	resultName string
	style      *styleModel
	font       *truetype.Font
	zoomX      float64
	zoomY      float64
	deltaX     float64
	deltaY     float64
	scale      float64
)

const (
	width         = 1366
	height        = 1024
	scaleX        = 7
	scaleY        = 10
	x0            = 0
	y0            = 0
	xn            = x0 + width/scaleX
	yn            = y0 + height/scaleY
	backgroundHex = "888"
	dataPath      = "./data"
	stylePath     = "./style"
	resultPath    = "./result"
	pointRadius   = 5.0
)

type layer struct {
	ID        string      `json:"id"`
	Level     string      `json:"level"`
	Order     int         `json:"order,string"`
	Color     string      `json:"color"`
	FontSize  float64     `json:"font-size,string"`
	LineWidth float64     `json:"line-width,string"`
	Fill      polygonFill `json:"fill"`
}

type polygonFill struct {
	State bool   `json:"state,string"`
	Color string `json:"color,omitempty"`
}

type styleModel struct {
	Layer []layer `json:"layer"`
}

type point struct {
	X float64
	Y float64
}

func init() {
	flag.StringVar(&geoName, "geo", "admin_level_4.geojson", "geojson file")
	flag.StringVar(&styleName, "style", "style.json", "style file")
	flag.StringVar(&resultName, "res", "admin_level_4.png", "result file")
	flag.Float64Var(&zoomX, "zx", 0, "zoom x")
	flag.Float64Var(&zoomY, "zy", 0, "zoom y")
	flag.Float64Var(&deltaX, "dx", 0, "offset x")
	flag.Float64Var(&deltaY, "dy", 0, "offset x")
	flag.Float64Var(&scale, "s", 1, "scale coefficient")
	err := initStyle()
	if err != nil {
		log.Printf("%+v", err)
		return
	}
	font, err = truetype.Parse(goregular.TTF)
	if err != nil {
		return
	}
}

func main() {
	flag.Parse()
	draw(style.Layer[2], zoomX, zoomY, deltaX, deltaY)
}

func min(x, y float64) float64 {
	if x < 0 || y < 0 {
		return max(x, y)
	}
	if x <= y {
		return x
	}
	return y
}
func max(x, y float64) float64 {
	if x >= y {
		return x
	}
	return y
}

func draw(mapLayer layer, zoomX, zoomY, deltaX, deltaY float64) (err error) {
	fc, err := dataToFeatureCollection()
	if err != nil {
		errorHandler(&err, "something went wrong at draw 1")
		return
	}
	resultName = filepath.Join(resultPath, resultName)

	face := truetype.NewFace(font, &truetype.Options{Size: mapLayer.FontSize})
	var minX, minY, maxX, maxY float64
	resetMinMax := func() {
		minX = xn
		minY = yn
		maxX = 0
		maxY = 0
	}
	resetMinMax()

	dc := initContext(width, height, backgroundHex)
	dc.SetFontFace(face)
	dc.ScaleAbout(scale, scale, zoomX, zoomY)
	dc.Translate(deltaX/scale, deltaY/scale)

	fillAndStroke := func() {
		dc.SetFillRuleWinding()
		if mapLayer.Fill.State {
			dc.SetHexColor(mapLayer.Fill.Color)
		} else {
			dc.SetHexColor("FFF")
		}
		dc.FillPreserve()
		dc.SetLineWidth(mapLayer.LineWidth * 2)
		dc.SetHexColor("#FFF")
		dc.StrokePreserve()
		dc.SetLineWidth(mapLayer.LineWidth)
		dc.SetHexColor(mapLayer.Color)
		dc.Stroke()
	}
	drawLineString := func(coords [][]float64) {
		for _, coord := range coords {
			x := coord[0]
			y := coord[1]
			dc.LineTo(x, y)
		}
		dc.Stroke()
	}
	drawPolygon := func(coords [][][]float64) {
		for _, polygon := range coords {
			for _, coord := range polygon {
				x := coord[0]
				y := coord[1]
				minX = min(x, minX)
				maxX = max(x, maxX)
				minY = min(y, minY)
				maxY = max(y, maxY)
				dc.LineTo(x, y)
			}
			dc.NewSubPath()
		}
		fillAndStroke()
	}
	drawString := func(name string) {
		xOffset, yOffset := dc.TransformPoint((minX + (maxX-minX)/2), (minY + (maxY-minY)/2))
		dc.Push()
		dc.Identity()
		dc.SetHexColor("FFF")
		dc.DrawStringWrapped(name, xOffset, yOffset, 0.5, 0.5, maxX-minX, 1, gg.AlignCenter)
		dc.Pop()
	}
	for _, f := range fc.Features {
		g := f.Geometry
		nameProp, hasName := f.Properties["name"]
		applyStyle(dc, &mapLayer)
		if g.IsMultiPolygon() {
			coords := g.MultiPolygon
			for _, polygon := range coords {
				drawPolygon(polygon)
			}
			if hasName {
				name := nameProp.(string)
				drawString(name)
			}
			resetMinMax()
			continue
		}
		if g.IsPolygon() {
			coords := g.Polygon
			drawPolygon(coords)
			if hasName {
				name := nameProp.(string)
				drawString(name)
			}
			resetMinMax()
			continue
		}
		if g.IsPoint() {
			coord := g.Point
			dc.DrawPoint(coord[0], coord[1], pointRadius)
			continue
		}
		if g.IsMultiPoint() {
			coords := g.MultiPoint
			for _, coord := range coords {
				dc.DrawPoint(coord[0], coord[1], pointRadius)
			}
			continue
		}
		if g.IsLineString() {
			coords := g.LineString
			drawLineString(coords)
			continue
		}
		if g.IsMultiLineString() {
			coords := g.MultiLineString
			for _, lineString := range coords {
				drawLineString(lineString)
			}
			continue
		}
	}
	dc.SavePNG(resultName)
	return
}

func errorHandler(err *error, msg string) {
	log.Println(msg)
	*err = errors.WithStack(*err)
}

func initStyle() (err error) {
	styleFile, err := os.Open(filepath.Join(stylePath, styleName))
	if err != nil {
		errorHandler(&err, "something went wrong")
		return
	}
	defer styleFile.Close()
	style = &styleModel{}
	err = json.NewDecoder(styleFile).Decode(style)
	if err != nil {
		errorHandler(&err, "something went wrong")
		return
	}
	return
}

func dataToFeatureCollection() (fc *geojson.FeatureCollection, err error) {
	geoFile, err := os.Open(filepath.Join(dataPath, geoName))
	if err != nil {
		errorHandler(&err, "geo file failed to open")
		return
	}
	defer geoFile.Close()
	var geoData []byte
	geoData, err = ioutil.ReadAll(geoFile)
	if err != nil {
		errorHandler(&err, "data failed to be read from geo file")
		return
	}
	fc, err = geojson.UnmarshalFeatureCollection(geoData)
	if err != nil {
		errorHandler(&err, "it failed to unmarshal featureCollection ")
	}
	return
}

func initContext(width int, height int, hex string) (dc *gg.Context) {
	dc = gg.NewContext(width, height)
	dc.InvertY()
	dc.SetHexColor(hex)
	dc.Clear()
	dc.Scale(scaleX, scaleY)
	dc.Translate(x0, y0)
	return
}

func applyStyle(dc *gg.Context, mapLayer *layer) {
	dc.SetHexColor(mapLayer.Color)
	dc.SetLineWidth(mapLayer.LineWidth)
}
