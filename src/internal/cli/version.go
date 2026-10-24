package cli

// Release builds set these values with -ldflags. Source builds stay identifiable.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)
