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

package fill

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/statediff"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"github.com/cerc-io/eth-statediff-fill-service/pkg/serve"
)

// WatchedAddress type is used to process currently watched addresses
type WatchedAddress struct {
	Address      string `db:"address"`
	CreatedAt    uint64 `db:"created_at"`
	WatchedAt    uint64 `db:"watched_at"`
	LastFilledAt uint64 `db:"last_filled_at"`

	StartBlock uint64
	EndBlock   uint64
}

// Service is the underlying struct for the watched address gap filling service
type Service struct {
	db        *sqlx.DB
	client    *rpc.Client
	ethClient *ethclient.Client
	interval  int
	quitChan  chan bool
}

// NewServer creates a new Service
func New(config *serve.Config) *Service {
	return &Service{
		db:        config.DB,
		client:    config.Client,
		ethClient: ethclient.NewClient(config.Client),
		interval:  config.WatchedAddressGapFillInterval,
		quitChan:  make(chan bool),
	}
}

// Start is used to begin the service
func (s *Service) Start(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-s.quitChan:
			log.Info("quiting server process")
			return
		default:
			s.fill()
		}
	}
}

// Stop is used to gracefully stop the service
func (s *Service) Stop() {
	log.Info("stopping watched address gap filler")
	close(s.quitChan)
}

// fill performs the filling of indexing gap for watched addresses
func (s *Service) fill() {
	// Wait for specified interval duration
	time.Sleep(time.Duration(s.interval) * time.Second)

	// Get watched addresses from the db
	rows := s.fetchWatchedAddresses()

	// Get the block number to start fill at
	// Get the block number to end fill at
	fillWatchedAddresses, minStartBlock, maxEndBlock := s.GetFillAddresses(rows)

	if len(fillWatchedAddresses) > 0 {
		log.Infof("running watched address gap filler for block range: (%d, %d)", minStartBlock, maxEndBlock)
	}

	// Fill the missing diffs
	for blockNumber := minStartBlock; blockNumber <= maxEndBlock; blockNumber++ {
		params := statediff.Params{
			IntermediateStateNodes:   true,
			IntermediateStorageNodes: true,
			IncludeBlock:             true,
			IncludeReceipts:          true,
			IncludeTD:                true,
			IncludeCode:              true,
		}

		fillAddresses := []interface{}{}
		for _, fillWatchedAddress := range fillWatchedAddresses {
			if blockNumber >= fillWatchedAddress.StartBlock && blockNumber <= fillWatchedAddress.EndBlock {
				params.WatchedAddresses = append(params.WatchedAddresses, common.HexToAddress(fillWatchedAddress.Address))
				fillAddresses = append(fillAddresses, fillWatchedAddress.Address)
			}
		}

		if len(fillAddresses) > 0 {
			ctx := context.Background()
			sub, err := s.subscribeWrites(ctx)
			if err != nil {
				log.Fatal(err)
			}
			jobID, err := s.writeStateDiffAt(blockNumber, params)
			if err != nil {
				log.Fatal(err)
			}
			ok, err := awaitStatus(sub, jobID, ctx)
			if err != nil {
				log.Fatal(err)
			}
			if ok {
				s.UpdateLastFilledAt(blockNumber, fillAddresses)
			}
		}
	}
}

// GetFillAddresses finds the encompassing range to perform fill for the given watched addresses
// it also sets the address specific fill range
func (s *Service) GetFillAddresses(rows []WatchedAddress) ([]WatchedAddress, uint64, uint64) {
	fillWatchedAddresses := []WatchedAddress{}
	minStartBlock := uint64(math.MaxUint64)
	maxEndBlock := uint64(0)

	for _, row := range rows {
		// Check for a gap between created_at and watched_at
		// CreatedAt and WatchedAt being equal is considered a gap of one block
		if row.CreatedAt > row.WatchedAt {
			continue
		}

		startBlock := uint64(0)
		endBlock := uint64(0)

		// Check if some of the gap was filled earlier
		if row.LastFilledAt > 0 {
			if row.LastFilledAt < row.WatchedAt {
				startBlock = row.LastFilledAt + 1
			}
		} else {
			startBlock = row.CreatedAt
		}

		// Add the address for filling
		if startBlock > 0 {
			row.StartBlock = startBlock
			if startBlock < minStartBlock {
				minStartBlock = startBlock
			}

			endBlock = row.WatchedAt
			row.EndBlock = endBlock
			if endBlock > maxEndBlock {
				maxEndBlock = endBlock
			}

			fillWatchedAddresses = append(fillWatchedAddresses, row)
		}
	}

	return fillWatchedAddresses, minStartBlock, maxEndBlock
}

// UpdateLastFilledAt updates the fill status for the provided addresses in the db
func (s *Service) UpdateLastFilledAt(blockNumber uint64, fillAddresses []interface{}) {
	// Prepare the query
	query := "UPDATE eth_meta.watched_addresses SET last_filled_at=? WHERE address IN (?" + strings.Repeat(",?", len(fillAddresses)-1) + ")"
	query = s.db.Rebind(query)

	args := []interface{}{blockNumber}
	args = append(args, fillAddresses...)

	// Execute the update query
	_, err := s.db.Exec(query, args...)
	if err != nil {
		log.Fatalf(err.Error())
	}
}

// fetchWatchedAddresses fetches watched addresses from the db
func (s *Service) fetchWatchedAddresses() []WatchedAddress {
	rows := []WatchedAddress{}
	pgStr := "SELECT * FROM eth_meta.watched_addresses"

	err := s.db.Select(&rows, pgStr)
	if err != nil {
		log.Fatalf("Error fetching watched addresses: %s", err.Error())
	}

	return rows
}

// writeStateDiffAt makes a RPC call to writeout statediffs at a blocknumber with the given params
func (s *Service) writeStateDiffAt(blockNumber uint64, params statediff.Params) (statediff.JobID, error) {
	var jobID statediff.JobID
	err := s.client.Call(&jobID, "statediff_writeStateDiffAt", blockNumber, params)
	if err != nil {
		return 0, fmt.Errorf("error making a RPC call to write statediff at block number %d: %s", blockNumber, err.Error())
	}
	return jobID, nil
}

// writeStateDiffFor makes a RPC call to writeout statediffs at a block hash with the given params
func (s *Service) writeStateDiffFor(hash common.Hash, params statediff.Params) error {
	err := s.client.Call(nil, "statediff_writeStateDiffFor", hash, params)
	if err != nil {
		return fmt.Errorf("error making a RPC call to write statediff at block hash %s: %s", hash.String(), err.Error())
	}
	return nil
}

// getBlockHashForNum returns the block hash for the provided block number
func (s *Service) getBlockHashForNum(blockNumber uint64) (common.Hash, error) {
	bNum, _ := new(big.Int).SetString(strconv.FormatUint(blockNumber, 10), 10)
	header, err := s.ethClient.HeaderByNumber(context.Background(), bNum)
	if err != nil {
		return common.Hash{}, fmt.Errorf("error making RPC call to get header for provided number %d: %s", blockNumber, err.Error())
	}
	return header.Hash(), nil
}

type writeSub struct {
	sub        *rpc.ClientSubscription
	statusChan <-chan statediff.JobStatus
}

func (ws writeSub) close() {
	ws.sub.Unsubscribe()
}

// subscribeWrites subscribes to the streamWrites job status endpoint
func (s *Service) subscribeWrites(ctx context.Context) (writeSub, error) {
	statusChan := make(chan statediff.JobStatus)
	sub, err := s.client.Subscribe(ctx, "statediff", statusChan, "streamWrites")
	if err != nil {
		return writeSub{}, fmt.Errorf("error subscribing to streamWrites: %w", err)
	}
	return writeSub{sub, statusChan}, err
}

// awaitStatus awaits status update for a writeStateDiffAt job
func awaitStatus(ws writeSub, job statediff.JobID, ctx context.Context) (bool, error) {
	for {
		select {
		case err := <-ws.sub.Err():
			return false, err
		case status := <-ws.statusChan:
			if status.Err != nil {
				return false, status.Err
			}
			if status.ID == job {
				return true, nil
			}
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}
