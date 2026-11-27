// Package sudokugolang embeds the default infrastructure config (sudoku-config/) and .env template,
// so `sudoku setup` can bootstrap ROOT_PROJECTS_FOLDER on any OS without the repository or bash.
package sudokugolang

import "embed"

// Defaults holds sudoku-config/ (without runtime data) and .env.example. Files are listed explicitly so
// local data that may sit in a developer's checkout (database dirs) is never compiled in.
//
//go:embed .env.example
//go:embed sudoku-config/compose.yaml
//go:embed sudoku-config/traefik/traefik.yaml
//go:embed sudoku-config/traefik/custom/.gitignore
//go:embed sudoku-config/centrifugo/config.json
//go:embed sudoku-config/elasticsearch/data/.gitignore
//go:embed sudoku-config/rabbitmq/data/.gitignore
//go:embed sudoku-config/redis/data/.gitignore
var Defaults embed.FS
