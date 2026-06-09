package main

import (
	sdkgrpc "github.com/DouDOU-start/airgate-sdk/runtimego/grpc"
	"github.com/DouDOU-start/airgate-studio/backend/internal/studio"
)

func main() {
	sdkgrpc.Serve(&studio.StudioPlugin{})
}
