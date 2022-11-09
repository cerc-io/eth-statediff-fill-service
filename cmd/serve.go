// Copyright © 2022 Vulcanize, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"os"
	"os/signal"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	fill "github.com/cerc-io/eth-statediff-fill-service/pkg/fill"
	srpc "github.com/cerc-io/eth-statediff-fill-service/pkg/rpc"
	s "github.com/cerc-io/eth-statediff-fill-service/pkg/serve"
	v "github.com/cerc-io/eth-statediff-fill-service/version"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "start service to fill indexing gap for watched addresses",
	Long: `This command configures a watched address gap filler service.

`,
	Run: func(cmd *cobra.Command, args []string) {
		subCommand = cmd.CalledAs()
		logWithCommand = *log.WithField("SubCommand", subCommand)
		serve()
	},
}

func serve() {
	logWithCommand.Infof("running eth-statediff-fill-service version: %s", v.VersionWithMeta)

	wg := new(sync.WaitGroup)
	logWithCommand.Debug("loading server configuration variables")
	serverConfig, err := s.NewConfig()
	if err != nil {
		logWithCommand.Fatal(err)
	}
	logWithCommand.Infof("server config: %+v", serverConfig)
	logWithCommand.Debug("initializing new server service")
	server, err := s.NewServer(serverConfig)
	if err != nil {
		logWithCommand.Fatal(err)
	}

	logWithCommand.Info("starting up servers")
	server.Serve(wg)
	if err := startServers(server, serverConfig); err != nil {
		logWithCommand.Fatal(err)
	}

	watchedAddressFillService := fill.New(serverConfig)
	wg.Add(1)
	go watchedAddressFillService.Start(wg)
	logWithCommand.Info("watched address gap filler enabled")

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown

	watchedAddressFillService.Stop()
	server.Stop()
	wg.Wait()
}

func startServers(server s.Server, settings *s.Config) error {
	if settings.IPCEnabled {
		logWithCommand.Info("starting up IPC server")
		_, _, err := srpc.StartIPCEndpoint(settings.IPCEndpoint, server.APIs())
		if err != nil {
			return err
		}
	} else {
		logWithCommand.Info("IPC server is disabled")
	}

	if settings.HTTPEnabled {
		logWithCommand.Info("starting up HTTP server")
		_, err := srpc.StartHTTPEndpoint(settings.HTTPEndpoint, server.APIs(), []string{"vdb"}, nil, []string{"*"}, rpc.HTTPTimeouts{})
		if err != nil {
			return err
		}
	} else {
		logWithCommand.Info("HTTP server is disabled")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(serveCmd)

	addDatabaseFlags(serveCmd)

	// flags for all config variables
	// json-rpc parameters
	serveCmd.PersistentFlags().Bool("eth-server-http", true, "turn on the eth http json-rpc server")
	serveCmd.PersistentFlags().String("eth-server-http-path", "", "endpoint url for eth http json-rpc server (host:port)")
	serveCmd.PersistentFlags().Bool("eth-server-ipc", false, "turn on the eth ipc json-rpc server")
	serveCmd.PersistentFlags().String("eth-server-ipc-path", "", "path for eth ipc json-rpc server")

	serveCmd.PersistentFlags().String("eth-http-path", "", "http url for ethereum node")

	// watched address gap filler flags
	serveCmd.PersistentFlags().Int("watched-address-gap-filler-interval", 60, "watched address gap fill interval in secs")

	// eth http json-rpc server
	viper.BindPFlag("eth.server.http", serveCmd.PersistentFlags().Lookup("eth-server-http"))
	viper.BindPFlag("eth.server.httpPath", serveCmd.PersistentFlags().Lookup("eth-server-http-path"))

	// eth ipc json-rpc server
	viper.BindPFlag("eth.server.ipc", serveCmd.PersistentFlags().Lookup("eth-server-ipc"))
	viper.BindPFlag("eth.server.ipcPath", serveCmd.PersistentFlags().Lookup("eth-server-ipc-path"))

	viper.BindPFlag("ethereum.httpPath", serveCmd.PersistentFlags().Lookup("eth-http-path"))

	// watched address gap filler flags
	viper.BindPFlag("watch.fill.interval", serveCmd.PersistentFlags().Lookup("watched-address-gap-filler-interval"))
}
