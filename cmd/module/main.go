package main

import (
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	generic "go.viam.com/rdk/services/generic"

	"sequenceplayback"
)

func main() {
	module.ModularMain(resource.APIModel{API: generic.API, Model: sequenceplayback.ArmPlayback})
}
