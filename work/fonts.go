package main

import (
	_ "embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed fonts/MiSans-Regular.subset.ttf
var fontRegular []byte

//go:embed fonts/MiSans-Bold.subset.ttf
var fontBold []byte

var fontsOnce sync.Once

func registerFonts() {
	fontsOnce.Do(func() {
		for _, data := range [][]byte{fontRegular, fontBold} {
			ui.RegisterFont(data, "MiSans")
		}
	})
}
