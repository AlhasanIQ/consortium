package main

import (
	"os"

	"github.com/alhasaniq/consortium/internal/appenv"
	"github.com/alhasaniq/consortium/internal/conctl"
	"github.com/alhasaniq/consortium/internal/conctl/app"
)

// Build-time variables set via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	appenv.LoadLocalEnv()

	cli := app.New(version, commit)

	// Register all resource groups.
	cli.Register(conctl.OverviewResource())
	cli.Register(conctl.JobsResource())
	cli.Register(conctl.WorkflowsResource())
	cli.Register(conctl.APIResource())
	cli.Register(conctl.BenchmarksResource())
	cli.Register(conctl.BenchmarkResource())
	cli.Register(conctl.OptimizeResource())
	cli.Register(conctl.TestResource())
	cli.Register(conctl.LocalResource())
	cli.Register(conctl.AdmissionResource())
	cli.Register(conctl.SystemResource())

	os.Exit(cli.Run(os.Args[1:]))
}
