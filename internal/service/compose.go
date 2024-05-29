package service

import (
	"fmt"
	"github.com/gammazero/workerpool"
	"github.com/schollz/progressbar/v3"
	"github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sudoku-golang/internal/infra/configs"
)

var env configs.Configs

type Composer struct {
	ComposeFiles          []string
	ForceRebuild          bool
	EnvVars               []string
	progressBar           *progressbar.ProgressBar
	projectName           string
	cmd                   *exec.Cmd
	wp                    *workerpool.WorkerPool
	dockerComposeTemplate string
}

func NewComposer(forceRebuild bool, envVars []string) *Composer {
	var env, err = configs.InitConfigs()

	if err != nil {
		logrus.Infoln(err)
	}

	logrus.Infoln(env)
	c := &Composer{
		ForceRebuild: forceRebuild,
		EnvVars:      envVars,
		wp:           workerpool.New(5),
	}
	c.setDockerComposeTemplate()
	c.FindComposeFiles()
	return c
}

func (c *Composer) FindComposeFiles() {
	var files []string
	globPatterns := []string{"compose.yaml", "*/compose-sudoku.yaml"}

	for _, pattern := range globPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			logrus.WithError(err).Fatal("Ошибка при поиске файлов")
		}
		for _, match := range matches {
			if !strings.Contains(match, "_noscan") {
				absPath, err := filepath.Abs(match)
				if err != nil {
					logrus.WithError(err).Fatal("Ошибка при получении абсолютного пути файла")
				}
				files = append(files, absPath)
			}
		}
	}

	c.ComposeFiles = files
}

func (c *Composer) Build() {
	logrus.Infoln("Сборка контейнеров")

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

			logrus.WithError(err).WithFields(fields).Fatal("Ошибка выполнения команды сборки")
		} else {
			c.addProgress(1, "Сборка окончена")
		}
	}
}

func (c *Composer) Start() {
	logrus.Infoln("Запуск контейнеров")

	c.setProgressBar(len(c.ComposeFiles))
	for _, composeFile := range c.ComposeFiles {
		c.setProjectName(composeFile)

		execCmd := fmt.Sprintf(c.dockerComposeTemplate+" up -d", composeFile)

		c.setCommand(execCmd)

		if err := c.cmd.Run(); err != nil {
			fields := c.setLogFields(composeFile, execCmd)

			logrus.WithError(err).WithFields(fields).Fatal("Ошибка выполнения команды запуска")
		} else {
			c.addProgress(1, "Все контейнеры запущены")
		}
	}
}

func (c *Composer) Stop() {
	logrus.Infoln("Остановка контейнеров")

	c.setProgressBar(len(c.ComposeFiles))
	for _, composeFile := range c.ComposeFiles {
		c.wp.Submit(func() {
			c.setProjectName(composeFile)

			execCmd := fmt.Sprintf(c.dockerComposeTemplate+" stop", composeFile)

			c.setCommand(execCmd)

			if err := c.cmd.Run(); err != nil {
				fields := c.setLogFields(composeFile, execCmd)

				logrus.WithError(err).WithFields(fields).Fatal("Ошибка выполнения команды остановки")
			} else {
				c.addProgress(1, "Все контейнеры остановлены")
			}
		})
	}

	c.wp.StopWait()
}

func (c *Composer) Down() {
	logrus.Infoln("Остановка и удаление контейнеров")

	c.setProgressBar(len(c.ComposeFiles))
	for _, composeFile := range c.ComposeFiles {
		c.wp.Submit(func() {
			c.setProjectName(composeFile)

			execCmd := fmt.Sprintf(c.dockerComposeTemplate+" down", composeFile)

			c.setCommand(execCmd)

			if err := c.cmd.Run(); err != nil {
				fields := c.setLogFields(composeFile, execCmd)

				logrus.WithError(err).WithFields(fields).Fatal("Ошибка выполнения команды остановки и удаления")
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

func (c *Composer) setProgressBar(countFiles int) {
	c.progressBar = progressbar.NewOptions(countFiles,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan]Обработка проектов...[reset]"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]𓂃𓂃𓂃[reset]",
			SaucerHead:    "[yellow]𓂃🌫🏎𓂃[reset]",
			SaucerPadding: " ",
			BarStart:      "🚦",
			BarEnd:        "🏁",
		}),
		progressbar.OptionOnCompletion(func() {
			fprint, err := fmt.Fprint(os.Stderr, "\n")
			if err != nil {
				logrus.Fatalln("Ошибка при завершении прогресса:", fprint)
			}
		}),
	)
}

func (c *Composer) addProgress(step int, finishMessage string) {
	err := c.progressBar.Add(step)

	if c.progressBar.IsFinished() {
		logrus.Infoln(finishMessage)
	}

	if err != nil {
		logrus.Errorln("Ошибка при добавлении шага прогресса")
		os.Exit(1)
	}
}

func (c *Composer) setDockerComposeTemplate() {
	c.dockerComposeTemplate = env.ConfigDockerPaths.DockerComposePath + " -f %s"
}
