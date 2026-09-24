// Package sqlfiles embeds the strategy SQL pipelines (sql/strategies/**) into
// the binary. Binaries that import backtestgosqlite as a library (e.g.
// trade_orchestrator on App Engine) have no repo checkout on disk, so a pipeline
// directory resolved against the working directory does not exist there.
package sqlfiles

import "embed"

// Strategies holds every file under sql/strategies, addressed as
// "strategies/<pipeline>/<file>.sql".
//
//go:embed strategies
var Strategies embed.FS
