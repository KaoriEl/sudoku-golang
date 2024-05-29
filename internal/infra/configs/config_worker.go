package configs

type ConfigWorker struct {
	MaxWorkers int `envconfig:"MAX_WORKERS,default=5"`
}
