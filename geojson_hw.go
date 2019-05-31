package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/paulmach/go.geojson"
	"github.com/pkg/errors"
	"golang.org/x/image/font/gofont/goregular"
)

var (
	resultName string
	style      *styleModel
	font       *truetype.Font
	scales     = make(map[int]point, 10)
	translates = make(map[int]point, 10)
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
	styleName     = "style.json"
	fileServer    = "http://localhost:8100/"
	minIndex      = 0
	maxIndex      = 3
	pointRadius   = 5.0

	clientxQuery = "clientx"
	clientyQuery = "clienty"
	orderQuery   = "order"
	zoominQuery  = "zoomin"
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
	http.HandleFunc("/zoom", makeHandler(zoomHandler))
	http.HandleFunc("/drag", makeHandler(dragHandler))
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func makeHandler(handler func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		err := handler(w, r)
		if err != nil {
			log.Printf("%+v", err)
		}
	}
}

func zoomHandler(w http.ResponseWriter, r *http.Request) (err error) {
	var answer string
	err = func() (err error) {
		err = r.ParseForm()
		if err != nil {
			return
		}
		order, err := strconv.Atoi(r.Form.Get(orderQuery))
		if err != nil {
			return
		}
		clientX, err := strconv.ParseFloat(r.Form.Get(clientxQuery), 64)
		if err != nil {
			return
		}
		clientY, err := strconv.ParseFloat(r.Form.Get(clientyQuery), 64)
		if err != nil {
			return
		}
		index := order - 1
		if index > 0 {
			p := point{clientX, clientY}
			translates[index] = point{0, 0}
			scales[index] = p
		}
		answer = fmt.Sprintf("%s%s.png", fileServer, getLevelID(index))
		err = draw(index)
		return
	}()
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	w.Write([]byte(answer))
	return
}

func dragHandler(w http.ResponseWriter, r *http.Request) (err error) {
	var answer string
	err = func() (err error) {
		err = r.ParseForm()
		if err != nil {
			return
		}
		order, err := strconv.Atoi(r.Form.Get(orderQuery))
		if err != nil {
			return
		}
		clientX, err := strconv.ParseFloat(r.Form.Get(clientxQuery), 64)
		if err != nil {
			return
		}
		clientY, err := strconv.ParseFloat(r.Form.Get(clientyQuery), 64)
		if err != nil {
			return
		}
		index := order - 1
		if index > 0 {
			p := point{clientX, clientY}
			val := translates[index]
			p.X += val.X
			p.Y += val.Y
			translates[index] = p
		}
		answer = "OK"
		err = draw(index)
		return
	}()
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	w.Write([]byte(answer))
	return
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

func draw(index int) (err error) {
	fc, err := dataToFeatureCollection(index)
	if err != nil {
		errorHandler(&err, "something went wrong at draw 1")
		return
	}

	resultName = filepath.Join(resultPath, style.Layer[index].ID+".png")
	face := truetype.NewFace(font, &truetype.Options{Size: style.Layer[index].FontSize})
	scale := 2.5
	mapLayer := style.Layer[index]
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
	for i := 1; i < (index + 1); i++ {
		scaleRate := scale * float64(i)
		dc.ScaleAbout(scale, scale, scales[i].X, scales[i].Y)
		dc.Translate(translates[i].X/scaleRate, translates[i].Y/scaleRate)
	}
	applyStyle(dc, &mapLayer)

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

func dataToFeatureCollection(index int) (fc *geojson.FeatureCollection, err error) {
	if index < minIndex || index > maxIndex {
		index = minIndex
	}
	geoFile, err := os.Open(filepath.Join(dataPath, style.Layer[index].ID+".geojson"))
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

func getLevelID(index int) string {
	if index >= minIndex && index <= maxIndex {
		return style.Layer[index].ID
	}
	return style.Layer[0].ID
}
