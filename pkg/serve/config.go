// VulcanizeDB
// Copyright © 2022 Vulcanize

// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.

// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/statediff/indexer/database/sql/postgres"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/viper"

	"github.com/cerc-io/eth-statediff-fill-service/pkg/prom"
	ethServerShared "github.com/cerc-io/ipld-eth-server/v4/pkg/shared"
)

// Env variables
const (
	SERVER_IPC_PATH  = "SERVER_IPC_PATH"
	SERVER_HTTP_PATH = "SERVER_HTTP_PATH"

	SERVER_MAX_IDLE_CONNECTIONS = "SERVER_MAX_IDLE_CONNECTIONS"
	SERVER_MAX_OPEN_CONNECTIONS = "SERVER_MAX_OPEN_CONNECTIONS"
	SERVER_MAX_CONN_LIFETIME    = "SERVER_MAX_CONN_LIFETIME"

	WATCHED_ADDRESS_GAP_FILLER_INTERVAL = "WATCHED_ADDRESS_GAP_FILLER_INTERVAL"
)

// Config struct
type Config struct {
	DB       *sqlx.DB
	DBConfig postgres.Config

	HTTPEnabled  bool
	HTTPEndpoint string

	IPCEnabled  bool
	IPCEndpoint string

	Client *rpc.Client

	WatchedAddressGapFillInterval int
}

// NewConfig is used to initialize a watcher config from a .toml file
// Separate chain watcher instances need to be ran with separate ipfs path in order to avoid lock contention on the ipfs repository lockfile
func NewConfig() (*Config, error) {
	c := new(Config)

	viper.BindEnv("ethereum.httpPath", ETH_HTTP_PATH)

	c.dbInit()
	ethHTTP := viper.GetString("ethereum.httpPath")
	ethHTTPEndpoint := fmt.Sprintf("http://%s", ethHTTP)
	cli, err := getEthClient(ethHTTPEndpoint)
	if err != nil {
		return nil, err
	}
	c.Client = cli

	// ipc server
	ipcEnabled := viper.GetBool("eth.server.ipc")
	if ipcEnabled {
		ipcPath := viper.GetString("eth.server.ipcPath")
		if ipcPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			ipcPath = filepath.Join(home, ".vulcanize/vulcanize.ipc")
		}
		c.IPCEndpoint = ipcPath
	}
	c.IPCEnabled = ipcEnabled

	// http server
	httpEnabled := viper.GetBool("eth.server.http")
	if httpEnabled {
		httpPath := viper.GetString("eth.server.httpPath")
		if httpPath == "" {
			httpPath = "127.0.0.1:8085"
		}
		c.HTTPEndpoint = httpPath
	}
	c.HTTPEnabled = httpEnabled

	overrideDBConnConfig(&c.DBConfig)
	serveDB, err := ethServerShared.NewDB(c.DBConfig.DbConnectionString(), c.DBConfig)
	if err != nil {
		return nil, err
	}

	prom.RegisterDBCollector(c.DBConfig.DatabaseName, serveDB)
	c.DB = serveDB

	c.loadWatchedAddressGapFillerConfig()

	return c, err
}

func overrideDBConnConfig(con *postgres.Config) {
	viper.BindEnv("database.server.maxIdle", SERVER_MAX_IDLE_CONNECTIONS)
	viper.BindEnv("database.server.maxOpen", SERVER_MAX_OPEN_CONNECTIONS)
	viper.BindEnv("database.server.maxLifetime", SERVER_MAX_CONN_LIFETIME)
	con.MaxIdle = viper.GetInt("database.server.maxIdle")
	con.MaxConns = viper.GetInt("database.server.maxOpen")
	con.MaxConnLifetime = time.Duration(viper.GetInt("database.server.maxLifetime"))
}

func (c *Config) dbInit() {
	viper.BindEnv("database.name", DATABASE_NAME)
	viper.BindEnv("database.hostname", DATABASE_HOSTNAME)
	viper.BindEnv("database.port", DATABASE_PORT)
	viper.BindEnv("database.user", DATABASE_USER)
	viper.BindEnv("database.password", DATABASE_PASSWORD)
	viper.BindEnv("database.maxIdle", DATABASE_MAX_IDLE_CONNECTIONS)
	viper.BindEnv("database.maxOpen", DATABASE_MAX_OPEN_CONNECTIONS)
	viper.BindEnv("database.maxLifetime", DATABASE_MAX_CONN_LIFETIME)

	c.DBConfig.DatabaseName = viper.GetString("database.name")
	c.DBConfig.Hostname = viper.GetString("database.hostname")
	c.DBConfig.Port = viper.GetInt("database.port")
	c.DBConfig.Username = viper.GetString("database.user")
	c.DBConfig.Password = viper.GetString("database.password")
	c.DBConfig.MaxIdle = viper.GetInt("database.maxIdle")
	c.DBConfig.MaxConns = viper.GetInt("database.maxOpen")
	c.DBConfig.MaxConnLifetime = time.Duration(viper.GetInt("database.maxLifetime"))
}

func (c *Config) loadWatchedAddressGapFillerConfig() {
	viper.BindEnv("watch.fill.interval", WATCHED_ADDRESS_GAP_FILLER_INTERVAL)

	c.WatchedAddressGapFillInterval = viper.GetInt("watch.fill.interval")
}
