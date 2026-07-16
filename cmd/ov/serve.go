// cmd/ov/serve.go
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/web"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var vaultFlag, bindFlag string
	var allowNonlocal bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the ov2 web server (capture form + inbox)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			errw := cmd.ErrOrStderr()
			host, _, err := net.SplitHostPort(bindFlag)
			if err != nil {
				return fmt.Errorf("--bind must be host:port: %w", err)
			}
			if err := web.AllowBind(host, allowNonlocal); err != nil {
				return err
			}
			ln, err := net.Listen("tcp", bindFlag)
			if err != nil {
				return err
			}
			defer ln.Close()
			srv := web.New(web.Config{
				VaultDir:  cfg.VaultDir,
				Inbox:     cfg.Inbox,
				Resources: cfg.Resources,
				Bind:      ln.Addr().String(), // the resolved address (bindFlag may end in ":0"), so Host-header validation matches what clients actually dial
			}, capture.NewHTTPTitleFetcher(), nil)
			fmt.Fprintf(errw, "ov2 serve: listening on http://%s\n", ln.Addr())
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return srv.Serve(ctx, ln)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().StringVar(&bindFlag, "bind", "127.0.0.1:8420", "address to listen on")
	cmd.Flags().BoolVar(&allowNonlocal, "allow-nonlocal-bind", false, "bind to a non-loopback, non-Tailscale address (dangerous)")
	return cmd
}
