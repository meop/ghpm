package config

type RepoRef struct {
	Owner string
	Repo  string
	URI   string
}

var (
	RepoGh         = RepoRef{Owner: "cli", Repo: "cli", URI: "github.com/cli/cli"}
	RepoGhpm       = RepoRef{Owner: "meop", Repo: "ghpm", URI: "github.com/meop/ghpm"}
	RepoGhpmConfig = RepoRef{Owner: "meop", Repo: "ghpm-config", URI: "github.com/meop/ghpm-config"}
	RepoSheesh     = RepoRef{Owner: "meop", Repo: "sheesh", URI: "github.com/meop/sheesh"}
)
