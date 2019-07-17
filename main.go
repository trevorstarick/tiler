package main

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/paulmach/orb"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmapi"
	"github.com/paulmach/osm/osmgeojson"
	"github.com/tdewolff/canvas"

	"github.com/pkg/profile"
)

var dejaVuSerif *canvas.FontFamily

var dimension = 256.0

func main() {
	dejaVuSerif = canvas.NewFontFamily("dejavu-serif")
	dejaVuSerif.Use(canvas.CommonLigatures)
	if err := dejaVuSerif.LoadFontFile("DejaVuSerif.ttf", canvas.FontRegular); err != nil {
		panic(err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		scale := 1.0
		scaleIndex := strings.Index(r.URL.Path, "@")

		if scaleIndex > -1 {
			fmt.Println(r.URL.Path[scaleIndex+1 : scaleIndex+2])
			scale, _ = strconv.ParseFloat(r.URL.Path[scaleIndex+1:scaleIndex+2], 64)
		}

		extension := strings.Split(r.URL.Path, ".")[1]
		xyz := strings.Split(strings.Split(r.URL.Path, ".")[0], "/")[1:]

		xyz[2] = strings.Split(xyz[2], "@")[0]

		t := Tile{}
		t.Y, _ = strconv.Atoi(xyz[2])
		t.X, _ = strconv.Atoi(xyz[1])
		t.Z, _ = strconv.Atoi(xyz[0])

		t.North, t.East, t.South, t.West = t.Bounds()

		spew.Dump(t)

		c := canvas.New(dimension, dimension)
		draw(&t, c)

		switch extension {
		case "png":
			img := c.WriteImage(scale)
			err := png.Encode(w, img)
			if err != nil {
				panic(err)
			}
		case "svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			c.WriteSVG(w)
		default:
			fmt.Println(extension)
		}
	})

	if os.Getenv("TESTING") == "" {
		log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), nil))
	} else {
		defer profile.Start().Stop()
		t := Tile{X: 8414, Y: 5384, Z: 14}
		t.North, t.East, t.South, t.West = t.Bounds()
		c := canvas.New(dimension, dimension)
		draw(&t, c)
		img := c.WriteImage(1.0)
		w := bytes.Buffer{}
		err := png.Encode(&w, img)
		if err != nil {
			panic(err)
		}
	}
}

type Tile struct {
	Z     int
	X     int
	Y     int
	Lat   float64
	Long  float64
	North float64
	South float64
	East  float64
	West  float64
}

func (t *Tile) Deg2num() (x int, y int) {
	x = int(math.Floor((t.Long + 180.0) / 360.0 * (math.Exp2(float64(t.Z)))))
	y = int(math.Floor((1.0 - math.Log(math.Tan(t.Lat*math.Pi/180.0)+1.0/math.Cos(t.Lat*math.Pi/180.0))/math.Pi) / 2.0 * (math.Exp2(float64(t.Z)))))
	return
}

func (t *Tile) Num2deg() (lat float64, long float64) {
	n := math.Pi - 2.0*math.Pi*float64(t.Y)/math.Exp2(float64(t.Z))
	lat = 180.0 / math.Pi * math.Atan(0.5*(math.Exp(n)-math.Exp(-n)))
	long = float64(t.X)/math.Exp2(float64(t.Z))*360.0 - 180.0
	return lat, long
}

func tileToLat(y, z int) float64 {
	n := math.Pi - (2.0*math.Pi*float64(y))/math.Pow(2.0, float64(z))
	return math.Atan(math.Sinh(n)) * 57.2958
}

func tileToLon(x, z int) float64 {
	return float64(x)/math.Pow(2.0, float64(z))*360.0 - 180
}

func (t *Tile) Bounds() (north, east, south, west float64) {
	north = tileToLat(t.Y, t.Z)
	south = tileToLat(t.Y+1, t.Z)
	west = tileToLon(t.X, t.Z)
	east = tileToLon(t.X+1, t.Z)
	return
}

func draw(t *Tile, c *canvas.Canvas) {
	xmin := t.West
	xmax := t.East
	ymin := t.South
	ymax := t.North

	fmt.Println(xmin, xmax, ymin, ymax)

	xmid := xmin + (xmax-xmin)/2.0

	ams0, err := osmapi.Map(context.Background(), &osm.Bounds{ymin, ymax, xmin, xmid})
	if err != nil {
		panic(err)
	}
	ams1, err := osmapi.Map(context.Background(), &osm.Bounds{ymin, ymax, xmid, xmax})
	if err != nil {
		panic(err)
	}

	categories := map[string]color.RGBA{
		"route_primary":     {248, 201, 103, 255},
		"route_secondary":   {253, 252, 248, 255},
		"route_residential": {245, 241, 230, 255},
		"route_pedestrian":  {245, 241, 230, 255},
		"route_transit":     {223, 210, 174, 255},
		"water":             {185, 211, 194, 255},
		"park":              {165, 176, 118, 255},
		"building":          {201, 178, 166, 255},
	}

	c.SetFillColor(color.RGBA{235, 227, 205, 255})
	c.DrawPath(0.0, 0.0, canvas.Rectangle(dimension, dimension))

	lines := map[string]*canvas.Path{}
	rings := map[string]*canvas.Path{}
	for _, ams := range []*osm.OSM{ams0, ams1} {

		fc, err := osmgeojson.Convert(ams,
			osmgeojson.NoID(true),
			osmgeojson.NoMeta(true),
			osmgeojson.NoRelationMembership(true))
		if err != nil {
			panic(err)
		}

		for _, f := range fc.Features {
			if tags, ok := f.Properties["tags"].(map[string]string); ok {

				var category string
				if hw, ok := tags["highway"]; ok {
					if hw != "primary" && hw != "secondary" && hw != "unclassified" && hw != "residential" && hw != "pedestrian" {
						continue
					}
					if hw == "unclassified" {
						hw = "residential"
					}
					category = "route_" + hw
				} else if manMade, ok := tags["man_made"]; ok && manMade == "bridge" {
					category = "route_residential"
				} else if _, ok := tags["natural"]; ok {
					category = "water"
				} else if railway, ok := tags["railway"]; ok && railway == "rail" {
					category = "route_transit"
				} else if leisure, ok := tags["leisure"]; ok {
					if leisure != "park" && leisure != "garden" && leisure != "playground" {
						continue
					}
					category = "park"
				} else if _, ok := tags["amenity"]; ok {
					category = "building"
				} else {
					continue
				}

				if g, ok := f.Geometry.(orb.LineString); ok && 1 < len(g) {
					p := &canvas.Path{}
					p.MoveTo(g[0][0], g[0][1])
					for _, point := range g {
						p.LineTo(point[0], point[1])
					}
					if _, ok := lines[category]; !ok {
						lines[category] = p
					} else {
						lines[category] = lines[category].Append(p)
					}
				} else if g, ok := f.Geometry.(orb.Polygon); ok {
					for _, ring := range g {
						if len(ring) == 0 {
							continue
						}

						p := &canvas.Path{}
						p.MoveTo(ring[0][0], ring[0][1])
						for _, point := range ring {
							p.LineTo(point[0], point[1])
						}
						p.Close()
						if _, ok := rings[category]; !ok {
							rings[category] = p
						} else {
							rings[category] = rings[category].Append(p)
						}
					}
				} else if g, ok := f.Geometry.(orb.MultiPolygon); ok {
					for _, poly := range g {
						for _, ring := range poly {
							if len(ring) == 0 {
								continue
							}

							p := &canvas.Path{}
							p.MoveTo(ring[0][0], ring[0][1])
							for _, point := range ring {
								p.LineTo(point[0], point[1])
							}
							p.Close()
							if _, ok := rings[category]; !ok {
								rings[category] = p
							} else {
								rings[category] = rings[category].Append(p)
							}
						}
					}
				} else if _, ok := f.Geometry.(orb.Point); ok {
				} else {
					fmt.Println("unsupported geometry:", f.Geometry)
				}
			}
		}
	}

	xscale := dimension / (xmax - xmin)
	yscale := dimension / (ymax - ymin)
	c.SetView(canvas.Identity.Translate(0.0, 0.0).Scale(xscale, yscale).Translate(-xmin, -ymin))

	catOrder := []string{"water", "route_pedestrian", "route_residential", "route_secondary", "route_primary", "route_transit", "park", "building"}
	for _, cat := range catOrder {
		c.SetFillColor(categories[cat])
		if lines[cat] != nil {
			width := 0.00015
			if cat == "route_residential" {
				width /= 1.5
			} else if cat == "route_primary" {
				width *= 1.5
			} else if cat == "route_pedestrian" {
				width /= 2.5
			} else if cat == "route_transit" {
				width /= 8.0
			}
			c.DrawPath(0.0, 0.0, lines[cat].Stroke(width, canvas.RoundCapper, canvas.RoundJoiner))
		}
		if rings[cat] != nil {
			c.DrawPath(0.0, 0.0, rings[cat])
		}
	}
}
