package config

type RepoRef struct {
	Owner string
	Repo  string
	URI   string
}

var (
	RepoGh         = RepoRef{"github.com/cli/cli", "cli", "cli"}
	RepoGhpm       = RepoRef{"github.com/meop/ghpm", "meop", "ghpm"}
	RepoGhpmConfig = RepoRef{"github.com/meop/ghpm-config", "meop", "ghpm-config"}
	RepoSheesh     = RepoRef{"github.com/meop/sheesh", "meop", "sheesh"}
)
