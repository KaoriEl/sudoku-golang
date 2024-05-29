package configs

type ConfigDockerPaths struct {
	DockerPath        string `envconfig:"DOCKER_PATH,default=/usr/bin/env docker"`
	DockerComposePath string `envconfig:"DOCKER_COMPOSE_PATH,default=/usr/bin/env docker compose"`
}
