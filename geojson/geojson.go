package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/fogleman/gg"
	"github.com/paulmach/go.geojson"
	"github.com/pkg/errors"
)

var (
	geoName    string
	styleName  string
	resultName string
	style      *styleModel
	path       string
)

const (
	width         = 1366
	height        = 1024
	scaleX        = 7
	scaleY        = 10
	x0            = 0
	y0            = 0
	pointRadius   = 5.0
	backgroundHex = "888"
	dataPath      = "./data"
	stylePath     = "./style"
	resultPath    = "./result"
	layerProp     = "admin_level"
)

type layer struct {
	ID        string      `json:"id"`
	Level     string      `json:"level"`
	Order     int         `json:"order,string"`
	Color     string      `json:"color"`
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

func init() {
	flag.StringVar(&geoName, "geo", "admin_level_4.geojson", "geojson file")
	flag.StringVar(&styleName, "style", "style.json", "style file")
	flag.StringVar(&resultName, "res", "admin_level_4.png", "result file")
}

func main() {
	flag.Parse()
	path = filepath.Join(resultPath, resultName)
	var err error
	fc, err := prepareData()
	if err != nil {
		log.Printf("%+v", err)
		return
	}
	draw(fc)
}

func draw(fc *geojson.FeatureCollection) {
	dc := initContext(width, height, backgroundHex)
	var vLayer layer
	drawLineString := func(coords [][]float64) {
		for _, coord := range coords {
			x := coord[0]
			y := coord[1]
			dc.LineTo(x, y)
		}
		dc.NewSubPath()
	}
	drawPolygon := func(coords [][][]float64) {
		for _, polygon := range coords {
			for _, coord := range polygon {
				x := coord[0]
				y := coord[1]
				dc.LineTo(x, y)
			}
			dc.NewSubPath()
		}
	}
	for _, f := range fc.Features {
		g := f.Geometry
		for _, vLayer = range style.Layer {
			if vLayer.Level == f.Properties[layerProp] {
				applyStyle(dc, &vLayer)
				break
			}
		}
		if g.IsMultiPolygon() {
			coords := g.MultiPolygon
			for _, polygon := range coords {
				drawPolygon(polygon)
			}
			continue
		}
		if g.IsPolygon() {
			coords := g.Polygon
			drawPolygon(coords)
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
	if vLayer.Fill.State {
		dc.SetHexColor(vLayer.Fill.Color)
	} else {
		dc.SetHexColor("FFF")
	}
	dc.FillPreserve()
	dc.SetLineWidth(vLayer.LineWidth * 2)
	dc.SetHexColor("#FFF")
	dc.StrokePreserve()
	dc.SetLineWidth(vLayer.LineWidth)
	dc.SetHexColor(vLayer.Color)
	dc.StrokePreserve()
	dc.SavePNG(path)
}

func errorHandler(err *error, msg string) {
	log.Println(msg)
	*err = errors.WithStack(*err)
}

func prepareData() (fc *geojson.FeatureCollection, err error) {
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
		errorHandler(&err, "it failed to unmarshal featureCollection")
		return
	}
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

func initContext(width int, height int, hex string) (dc *gg.Context) {
	dc = gg.NewContext(width, height)
	dc.InvertY()
	dc.SetHexColor(hex)
	dc.Clear()
	dc.Translate(x0, y0)
	dc.Scale(scaleX, scaleY)
	return
}

func applyStyle(dc *gg.Context, vLayer *layer) {
	dc.SetHexColor(vLayer.Color)
	dc.SetLineWidth(vLayer.LineWidth)
}
