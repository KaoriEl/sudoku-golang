package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sudoku-golang/internal/admin"
	"sudoku-golang/internal/docker"
	"sudoku-golang/internal/engine"
	"sudoku-golang/internal/infra/configs"
	"sudoku-golang/internal/metrics"
	"sudoku-golang/internal/webui"
	"syscall"

	"github.com/alperdrsnn/clime"
	"github.com/spf13/cobra"
)

const descServeAddr = "Адрес админки: loopback host:port или unix:/путь/к/сокету; по умолчанию ADMIN_ADDR из .env или 127.0.0.1:7777"

func commandServe(a *app) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Запустить веб-админку",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			banner("serve")
			if addr == "" {
				addr = a.cfg.AdminAddr
			}
			return serve(a, addr, cmd.Flags().Changed("addr"))
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", descServeAddr)
	return cmd
}

func serve(a *app, addr string, addrFromFlag bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := docker.NewRunner(a.cfg.DockerPath, a.cfg.DockerComposePath, os.Environ())
	ep := engine.ResolveEndpoint(ctx, runner.DockerArgv(), runner.Env())
	eng := engine.New(ep)
	collector := metrics.NewCollector(a.log, eng)
	go collector.Run(ctx)
	svc := admin.NewService(a.log, a.cfg, runner, collector)
	svc.SetRestarter(reexec, addrFromFlag)
	svc.SetAuth(a.cfg.AdminToken, admin.IsUnixAddr(addr))
	svc.Start(ctx)
	server := admin.NewServer(a.log, svc, webui.Handler())

	clime.InfoLine("Docker Engine: " + ep.String())
	url := admin.DisplayURL(addr)
	if a.cfg.AdminToken != "" && !admin.IsUnixAddr(addr) {
		clime.SuccessLine(fmt.Sprintf("Админка доступна: %s/?token=%s", url, a.cfg.AdminToken))
	} else {
		clime.SuccessLine("Админка доступна: " + url)
	}
	if err := server.ListenAndServe(ctx, addr); err != nil {
		return fmt.Errorf("admin server: %w", err)
	}
	return nil
}

// reexecEnv marks the re-executed process so it retries binding the port the old one just released.
const reexecEnv = "SUDOKU_REEXEC=1"

func reexecArgs(dropAddrFlag bool) ([]string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("executable: %w", err)
	}
	args := os.Args
	if dropAddrFlag {
		args = withoutFlag(args, "--addr")
	}
	env := append(configs.CleanEnviron(), reexecEnv)
	return append([]string{exe}, args[1:]...), env, nil
}

func withoutFlag(args []string, name string) []string {
	out := make([]string, 0, len(args))
	short := "-" + strings.TrimPrefix(name, "--")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == name || a == short:
			i++ // skip the value
		case strings.HasPrefix(a, name+"=") || strings.HasPrefix(a, short+"="):
		default:
			out = append(out, a)
		}
	}
	return out
}
