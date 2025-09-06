package service

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/infra/configs"

	"github.com/alperdrsnn/clime"
	"github.com/gammazero/workerpool"
)

type Composer struct {
	log                   *slog.Logger
	ComposeFiles          []string
	ForceRebuild          bool
	EnvVars               []string
	progressBar           *clime.ProgressBar
	currentProgress       int64
	totalProgress         int64
	projectName           string
	cmd                   *exec.Cmd
	wp                    *workerpool.WorkerPool
	dockerComposeTemplate string
	config                *configs.Config
}

func NewComposer(log *slog.Logger, forceRebuild bool, envVars []string, config *configs.Config) *Composer {
	c := &Composer{
		log:          log,
		ForceRebuild: forceRebuild,
		EnvVars:      envVars,
		wp:           workerpool.New(config.MaxWorkers),
		config:       config,
	}
	c.setDockerComposeTemplate()
	c.FindComposeFiles()
	return c
}

func (c *Composer) FindComposeFiles() {
	var files []string
	globPatterns := []string{"compose.yaml", "*/compose-sudoku.yaml"}

	rootProjectsFolder := c.config.RootProjectsFolder
	if rootProjectsFolder == "" {
		c.log.Error("ROOT_PROJECTS_FOLDER env is not set")
		panic("ROOT_PROJECTS_FOLDER env is not set")
	}

	for _, pattern := range globPatterns {
		fullPattern := filepath.Join(rootProjectsFolder, pattern)
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			c.log.Error("Ошибка при поиске файлов", slog.Any("error", err))
			panic(err)
		}
		for _, match := range matches {
			if !strings.Contains(match, "_noscan") {
				absPath, err := filepath.Abs(match)
				if err != nil {
					c.log.Error("Ошибка при получении абсолютного пути файла", slog.Any("error", err))
					panic(err)
				}
				files = append(files, absPath)
			}
		}
	}

	c.ComposeFiles = files
}

func (c *Composer) setProgressBar(countFiles int) {
	c.totalProgress = int64(countFiles)
	c.currentProgress = 0
	c.progressBar = clime.NewProgressBar(c.totalProgress).
		WithLabel("Обработка проектов...").
		WithStyle(clime.ProgressStyleModern).
		WithColor(clime.CyanColor).
		ShowRate(true)
	c.progressBar.Set(0)
	c.progressBar.Print()
}

func (c *Composer) addProgress(step int, finishMessage string) {
	c.currentProgress += int64(step)
	if c.currentProgress > c.totalProgress {
		c.currentProgress = c.totalProgress
	}
	c.progressBar.Set(c.currentProgress)
	c.progressBar.Print()

	if c.currentProgress == c.totalProgress {
		c.log.Info(finishMessage)
	}
}

func (c *Composer) Build() {
	c.log.Info("Сборка контейнеров")
	c.setProgressBar(len(c.ComposeFiles))

	for _, composeFile := range c.ComposeFiles {
		c.setProjectName(composeFile)

		buildCmd := "build"
		if c.ForceRebuild {
			buildCmd = "build --no-cache"
		}

		execCmd := fmt.Sprintf(c.dockerComposeTemplate+" %s", composeFile, buildCmd)
		c.setCommand(execCmd)

		if err := c.cmd.Run(); err != nil {
			fields := c.setLogFields(composeFile, execCmd)
			c.log.Error("Ошибка выполнения команды сборки", slog.Any("error", err), slog.Any("fields", fields))
		} else {
			c.addProgress(1, "Сборка окончена")
		}
	}
}

func (c *Composer) Start() {
	c.log.Info("Запуск контейнеров")
	c.setProgressBar(len(c.ComposeFiles))

	for _, composeFile := range c.ComposeFiles {
		c.setProjectName(composeFile)

		execCmd := fmt.Sprintf(c.dockerComposeTemplate+" up -d", composeFile)
		c.setCommand(execCmd)

		if err := c.cmd.Run(); err != nil {
			fields := c.setLogFields(composeFile, execCmd)
			c.log.Error("Ошибка выполнения команды запуска", slog.Any("error", err), slog.Any("fields", fields))
		} else {
			c.addProgress(1, "Все контейнеры запущены")
		}
	}
}

func (c *Composer) Stop() {
	c.log.Info("Остановка контейнеров")
	c.setProgressBar(len(c.ComposeFiles))

	for _, composeFile := range c.ComposeFiles {
		c.wp.Submit(func() {
			c.setProjectName(composeFile)

			execCmd := fmt.Sprintf(c.dockerComposeTemplate+" stop", composeFile)
			c.setCommand(execCmd)

			if err := c.cmd.Run(); err != nil {
				fields := c.setLogFields(composeFile, execCmd)
				c.log.Error("Ошибка выполнения команды остановки", slog.Any("error", err), slog.Any("fields", fields))
			} else {
				c.addProgress(1, "Все контейнеры остановлены")
			}
		})
	}
	c.wp.StopWait()
}

func (c *Composer) Down() {
	c.log.Info("Остановка и удаление контейнеров")
	c.setProgressBar(len(c.ComposeFiles))

	for _, composeFile := range c.ComposeFiles {
		c.wp.Submit(func() {
			c.setProjectName(composeFile)

			execCmd := fmt.Sprintf(c.dockerComposeTemplate+" down", composeFile)
			c.setCommand(execCmd)

			if err := c.cmd.Run(); err != nil {
				fields := c.setLogFields(composeFile, execCmd)
				c.log.Error("Ошибка выполнения команды остановки и удаления", slog.Any("error", err), slog.Any("fields", fields))
			} else {
				c.addProgress(1, "Все контейнеры остановлены и удалены")
			}
		})
	}
	c.wp.StopWait()
}

func (c *Composer) setCommand(execCmd string) {
	cmd := exec.Command("bash", "-c", execCmd)
	cmd.Env = append(cmd.Env, c.EnvVars...)
	c.cmd = cmd
}

func (c *Composer) setProjectName(composeFile string) {
	c.projectName = filepath.Base(filepath.Dir(composeFile))
}

func (c *Composer) setLogFields(composeFile string, execCmd string) map[string]interface{} {
	return map[string]interface{}{
		"composeFile": composeFile,
		"execCmd":     execCmd,
	}
}

func (c *Composer) setDockerComposeTemplate() {
	c.dockerComposeTemplate = c.config.DockerComposePath + " -f %s"
}
