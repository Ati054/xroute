package controlplane

import (
	"runtime"

	"github.com/Ati054/xroute/internal/releasecontract"
)

func (server *Server) imageArchitecture() string {
	if server.testImageArchitecture != "" {
		return releasecontract.ImageArchitecture(server.testImageArchitecture)
	}
	return releasecontract.ImageArchitecture(runtime.GOARCH)
}
