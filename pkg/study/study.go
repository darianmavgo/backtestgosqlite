package study

// Study is the unified interface implemented by all analytical studies.
type Study interface {
	// ID returns the unique CLI / programmatic identifier (e.g. "march_april_voo_gld_uten").
	ID() string

	// Name returns the human-readable display name.
	Name() string

	// Description returns a concise summary of the study's objective.
	Description() string

	// SetDatabases injects the required database paths.
	// marketDBPath: Read-only source for historical data.
	// resultsDBPath: Isolated database to write all study calculations and results.
	SetDatabases(marketDBPath, resultsDBPath string)

	// Run executes the study logic.
	Run() error
}

// ClusterDBSetter is implemented by studies that read regime labels from a
// database other than the market-bar source. study.Run calls SetClusterDB
// when the study implements it.
type ClusterDBSetter interface {
	SetClusterDB(path string)
}
