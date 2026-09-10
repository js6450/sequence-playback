package main

import (
	"context"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	generic "go.viam.com/rdk/services/generic"

	"sequenceplayback"
)

func main() {
	ctx := context.Background()
	logger := logging.NewLogger("cli")
	conf := resource.Config{Name: "playback", API: generic.API, Model: sequenceplayback.ArmPlayback}
	thing, err := sequenceplayback.NewArmPlayback(ctx, resource.Dependencies{}, conf, logger)
	if err != nil {
		panic(err)
	}
	defer thing.Close(ctx)
}
