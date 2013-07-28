/*
Copyright (C) 2013 Lucy

Permission is hereby granted, free of charge, to any person obtaining a
copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the
Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
DEALINGS IN THE SOFTWARE.package main
*/

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/cmplx"
	"os"

	"github.com/jackvalmadre/go-fftw"
	flag "github.com/neeee/pflag"
	"github.com/neeee/termbox-go"
)

var (
	color = flag.StringP("color", "c", "default", "Color to use")
	dim   = flag.BoolP("dim", "d", false,
		"Turn off bright colors where possible")

	step  = flag.Int("step", 2, "Samples to average in each column (wave)")
	scale = flag.Float64("scale", 2, "Scale divisor (spectrum)")

	icolor = flag.BoolP("icolor", "i", false,
		"Color bars according to intensity (spectrum)")
	imode = flag.String("imode", "dumb",
		"Mode for intensity colorisation (dumb, 256 or grayscale)")

	filename = flag.StringP("file", "f", "/tmp/mpd.fifo",
		"Where to read pcm date from")
	vis = flag.StringP("viz", "v", "wave",
		"Visualisation (spectrum or wave)")
)

var colors = map[string]termbox.Attribute{
	"default": termbox.ColorDefault,
	"black":   termbox.ColorBlack,
	"red":     termbox.ColorRed,
	"green":   termbox.ColorGreen,
	"yellow":  termbox.ColorYellow,
	"blue":    termbox.ColorBlue,
	"magenta": termbox.ColorMagenta,
	"cyan":    termbox.ColorCyan,
	"white":   termbox.ColorWhite,
}

var iColors []termbox.Attribute

var (
	on  = termbox.ColorDefault
	off = termbox.ColorDefault
)

func warn(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func main() {
	flag.Parse()

	if cl, ok := colors[*color]; !ok {
		warn("Unknown color \"%s\"\n", *color)
		return
	} else {
		on = cl
	}

	if !*dim {
		on = on | termbox.AttrBold
	}

	switch *imode {
	case "dumb":
		iColors = []termbox.Attribute{
			termbox.ColorBlue,
			termbox.ColorCyan,
			termbox.ColorGreen,
			termbox.ColorYellow,
			termbox.ColorRed,
		}
		if !*dim {
			for i := range iColors {
				iColors[i] = iColors[i] + 8
			}
		}
	case "256":
		iColors = []termbox.Attribute{
			21, 27, 39, 45, 51, 86, 85, 84, 82,
			154, 192, 220, 214, 208, 202, 196,
		}
	case "grayscale":
		const num = 19
		iColors = make([]termbox.Attribute, num)
		for i := termbox.Attribute(0); i < num; i++ {
			iColors[i] = i + 255 - num
		}
	default:
		warn("Unsupported mode: \"%s\"\n", *imode)
		return
	}

	var draw func(*os.File, chan bool)
	switch *vis {
	case "spectrum":
		draw = drawSpectrum
	case "wave":
		draw = drawWave
	default:
		warn("Unknown visualisation \"%s\"\n"+
			"Supported: spectrum, wave\n", *vis)
		return
	}

	file, err := os.Open(*filename)
	if err != nil {
		warn("%s\n", err)
		return
	}
	defer file.Close()

	err = termbox.Init()
	if err != nil {
		warn("%s\b", err)
		return
	}
	defer termbox.Close()

	end := make(chan bool)

	// drawer
	go draw(file, end)

	// input handler
	go func() {
		for {
			ev := termbox.PollEvent()
			if ev.Ch == 0 && ev.Key == termbox.KeyCtrlC {
				close(end)
				return
			}
		}
	}()

	<-end
}

func size() (int, int) {
	w, h := termbox.Size()
	return w, h * 2
}

func drawWave(file *os.File, end chan bool) {
	w, h := size()
	inRaw := make([]int16, w**step)
	for pos := 0; ; pos++ {
		if pos >= w {
			pos = 0
			w, h = size()
			if s := w * *step; len(inRaw) != s {
				inRaw = make([]int16, s)
			}
			if binary.Read(file, binary.LittleEndian, &inRaw) == io.EOF {
				close(end)
				return
			}
			termbox.Flush()
			termbox.Clear(off, off)
		}

		var v float64
		for i := 0; i < *step; i++ {
			v += float64(inRaw[pos**step+i])
		}

		half_h := float64(h / 2)
		vi := int(v/float64(*step)/(math.MaxInt16/half_h) + half_h)
		if vi%2 == 0 {
			termbox.SetCell(pos, vi/2, '▀', on, off)
		} else {
			termbox.SetCell(pos, vi/2, '▄', on, off)
		}
	}
}

func drawSpectrum(file *os.File, end chan bool) {
	w, h := size()
	var (
		samples = (w - 1) * 2
		resn    = w
		in      = make([]float64, samples)
		inRaw   = make([]int16, samples)
		out     = fftw.Alloc1d(resn)
		plan    = fftw.PlanDftR2C1d(in, out, fftw.Measure)
	)

	for {
		if resn != w && w != 1 {
			fftw.Free1d(out)
			resn = w
			samples = (w - 1) * 2
			in = make([]float64, samples)
			inRaw = make([]int16, samples)
			out = fftw.Alloc1d(resn)
			plan = fftw.PlanDftR2C1d(in, out, fftw.Measure)
		}

		if binary.Read(file, binary.LittleEndian, &inRaw) == io.EOF {
			close(end)
			return
		}

		for i := 0; i < samples; i++ {
			in[i] = float64(inRaw[i])
		}

		plan.Execute()
		hf := float64(h)
		for i := 0; i < w; i++ {
			v := cmplx.Abs(out[i]) / 1e5 * hf / *scale
			vi := int(v)
			if *icolor {
				on = iColors[int(math.Min(float64(len(iColors)-1),
					(v/hf)*float64(len(iColors)-1)))]
			}
			for j := h - 1; j > h-vi; j-- {
				termbox.SetCell(i, j/2, '┃', on, off)
			}
			if vi%2 != 0 {
				termbox.SetCell(i, (h-vi)/2, '╻', on, off)
			}
		}

		termbox.Flush()
		termbox.Clear(off, off)
		w, h = size()
	}
}
