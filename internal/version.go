package internal

import "fmt"

const Version = "v1.4.6"

// Homepage is the canonical public repository URL on GitHub.
const Homepage = "https://github.com/snonux/gogios"

// VersionBanner returns the two lines printed by gogios -version.
func VersionBanner() string {
	return fmt.Sprintf("This is Gogios version %s; (C) by Paul Buetow\n%s\n", Version, Homepage)
}
