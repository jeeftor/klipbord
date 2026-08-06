package main

import (
	"github.com/jeeftor/klipbord/internal/app"
	"github.com/jeeftor/klipbord/internal/webassets"
)

var version = "dev"

func main() {
	app.Run(version, webassets.Embedded())
}
