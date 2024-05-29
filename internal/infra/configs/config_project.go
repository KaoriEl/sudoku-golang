package configs

type ConfigProject struct {
	RootProjectsFolder string `envconfig:"ROOT_PROJECTS_FOLDER,default=cdek"`
}
