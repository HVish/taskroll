package cli

import (
	"errors"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/hvish/taskroll"
	"github.com/hvish/taskroll/internal/web"
)

func serveCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Open a board and list view of the tracker in the browser",
		Long: "Serves the tracker on 127.0.0.1 only, behind a per-session token in the printed\n" +
			"URL: open that URL, not the bare address. Moving a card, commenting and adding a\n" +
			"task write through the same locked store as the commands, so the generated files\n" +
			"stay current. Stop it with Ctrl-C.",
		Example: "  TRACKER serve\n  TRACKER serve --port 7788",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := writable()
			if err != nil {
				return err
			}
			dir := p.Dir
			srv, err := web.New(func() (*taskroll.Project, error) { return openAt(dir) }, actor(dir))
			if err != nil {
				return err
			}
			ln, err := srv.Listen(port)
			if err != nil {
				return err
			}
			out{cmd.OutOrStdout()}.printf("taskroll at %s\n(open this URL; the bare address answers forbidden)\n", srv.URL())
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				_ = ln.Close()
			}()
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port on 127.0.0.1; 0 picks a free one")
	return cmd
}
