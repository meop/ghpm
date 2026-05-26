package config

type RepoRef struct {
	URI   string
	Owner string
	Repo  string
}

var (
	RepoGhpm       = RepoRef{"github.com/meop/ghpm", "meop", "ghpm"}
	RepoGh         = RepoRef{"github.com/cli/cli", "cli", "cli"}
	RepoSheesh     = RepoRef{"github.com/meop/sheesh", "meop", "sheesh"}
	RepoGhpmConfig = RepoRef{"github.com/meop/ghpm-config", "meop", "ghpm-config"}
)
