package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func GetVersion() VersionInfo {
	return VersionInfo{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
	}
}
