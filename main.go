package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/AgustinLorenzo/pathvector/cmd"
)

// Build process flags
var (
	version = "devel"
	commit  = "unknown"
	date    = "unknown"
)

func buildMetadata() (string, string) {
	buildCommit, buildDate := commit, date
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return buildCommit, buildDate
	}

	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	if buildCommit == "unknown" {
		buildCommit = settings["vcs.revision"]
		if buildCommit != "" && settings["vcs.modified"] == "true" {
			buildCommit += "-dirty"
		}
		if buildCommit == "" {
			buildCommit = "unknown"
		}
	}
	if buildDate == "unknown" {
		buildDate = settings["vcs.time"]
		if buildDate == "" {
			buildDate = "unknown"
		}
	}

	return buildCommit, buildDate
}

func main() {
	if //goland:noinspection GoBoolExpressions
	version == "devel" || strings.Contains(version, "SNAPSHOT") {
		fmt.Fprintln(os.Stderr, `*******************************************************************************
WARNING: This is a development build. It is not recommended for production use.
Please submit any bugs to https://github.com/AgustinLorenzo/pathvector/issues
*******************************************************************************`)
	}

	buildCommit, buildDate := buildMetadata()
	if err := cmd.Execute(version, buildCommit, buildDate); err != nil {
		log.Fatal(err)
	}
}
