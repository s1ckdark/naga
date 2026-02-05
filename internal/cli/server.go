package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	var (
		host string
		port int
	)

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the web server",
		Long:  "Start the clusterctl web server with dashboard UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := getConfig()
			if err != nil {
				return err
			}

			if host == "" {
				host = cfg.Server.Host
			}
			if port == 0 {
				port = cfg.Server.Port
			}

			fmt.Printf("Starting server at http://%s:%d\n", host, port)
			fmt.Println("Press Ctrl+C to stop")

			// TODO: Start actual web server
			// server := web.NewServer(cfg)
			// return server.Start(host, port)

			fmt.Println("\n(Web server not yet implemented)")
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "Server host (default from config)")
	cmd.Flags().IntVar(&port, "port", 0, "Server port (default from config)")

	return cmd
}
