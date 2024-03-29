package integration_test

import (
	"context"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/statediff/types"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	integration "github.com/cerc-io/ipld-eth-server/v4/test"
)

var (
	ipldMethod           = "vdb_watchAddress"
	sleepInterval        = 2 * time.Second
	serviceInterval      int64
	gethRPCClient        *rpc.Client
	ipldClient           *ethclient.Client
	fillServiceRPCClient *rpc.Client
)

var _ = Describe("Watched address gap filling service integration test", func() {
	It("service init", func() {
		var err error
		serviceInterval, err = strconv.ParseInt(os.Getenv("WATCHED_ADDRESS_GAP_FILLER_INTERVAL"), 10, 0)
		Expect(err).To(BeNil())

		gethHttpPath := "http://127.0.0.1:8545"
		gethRPCClient, err = rpc.Dial(gethHttpPath)
		Expect(err).ToNot(HaveOccurred())

		ipldEthHttpPath := "http://127.0.0.1:8081"
		ipldClient, err = ethclient.Dial(ipldEthHttpPath)
		Expect(err).ToNot(HaveOccurred())

		fillServiceHttpPath := "http://127.0.0.1:8085"
		fillServiceRPCClient, err = rpc.Dial(fillServiceHttpPath)
		Expect(err).ToNot(HaveOccurred())
	})

	var (
		ctx = context.Background()

		contractErr error
		txErr       error

		GLD1 *integration.ContractDeployed
		SLV1 *integration.ContractDeployed
		SLV2 *integration.ContractDeployed

		countAIndex string
		countBIndex string

		oldCountA1 = big.NewInt(0)
		oldCountA2 = big.NewInt(0)
		oldCountB2 = big.NewInt(0)

		updatedCountA1 = big.NewInt(1)
		updatedCountA2 = big.NewInt(1)
		updatedCountB2 = big.NewInt(1)

		SLV2CountBIncrementedAt *integration.CountIncremented

		contract1Salt = "contract1SLV"
	)

	It("test init", func() {
		// Clear out watched addresses
		err := integration.ClearWatchedAddresses(gethRPCClient)
		Expect(err).ToNot(HaveOccurred())

		// Deploy a GLD contract
		GLD1, contractErr = integration.DeployContract()
		Expect(contractErr).ToNot(HaveOccurred())

		// Watch GLD1 contract
		operation := types.Add
		args := []types.WatchAddressArg{
			{
				Address:   GLD1.Address,
				CreatedAt: uint64(GLD1.BlockNumber),
			},
		}
		ipldErr := fillServiceRPCClient.Call(nil, ipldMethod, operation, args)
		Expect(ipldErr).ToNot(HaveOccurred())

		// Deploy two SLV contracts and update storage slots
		SLV1, contractErr = integration.Create2Contract("SLVToken", contract1Salt)
		Expect(contractErr).ToNot(HaveOccurred())

		_, txErr = integration.IncrementCount(SLV1.Address, "A")
		Expect(txErr).ToNot(HaveOccurred())

		SLV2, contractErr = integration.DeploySLVContract()
		Expect(contractErr).ToNot(HaveOccurred())

		_, txErr = integration.IncrementCount(SLV2.Address, "A")
		Expect(txErr).ToNot(HaveOccurred())
		SLV2CountBIncrementedAt, txErr = integration.IncrementCount(SLV2.Address, "B")
		Expect(txErr).ToNot(HaveOccurred())

		// Get storage slot keys
		storageSlotAKey, err := integration.GetStorageSlotKey("SLVToken", "countA")
		Expect(err).ToNot(HaveOccurred())
		countAIndex = storageSlotAKey.Key

		storageSlotBKey, err := integration.GetStorageSlotKey("SLVToken", "countB")
		Expect(err).ToNot(HaveOccurred())
		countBIndex = storageSlotBKey.Key
	})

	defer It("test cleanup", func() {
		// Destroy create2 contract
		_, err := integration.DestroySLVContract(SLV1.Address)
		Expect(err).ToNot(HaveOccurred())

		// Clear out watched addresses
		err = integration.ClearWatchedAddresses(gethRPCClient)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("previously unwatched contract watched", func() {
		It("indexes state only for watched contract", func() {
			// WatchedAddresses = [GLD1]
			// SLV1, countA
			countA1Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV1.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA1 := new(big.Int).SetBytes(countA1Storage)
			Expect(countA1.String()).To(Equal(oldCountA1.String()))

			// SLV2, countA
			countA2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA2 := new(big.Int).SetBytes(countA2Storage)
			Expect(countA2.String()).To(Equal(oldCountA2.String()))

			// SLV2, countB
			countB2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countBIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countB2 := new(big.Int).SetBytes(countB2Storage)
			Expect(countB2.String()).To(Equal(oldCountB2.String()))
		})

		It("indexes past state on watching a contract", func() {
			// Watch SLV1 contract
			args := []types.WatchAddressArg{
				{
					Address:   SLV1.Address,
					CreatedAt: uint64(SLV1.BlockNumber),
				},
			}
			ipldErr := fillServiceRPCClient.Call(nil, ipldMethod, types.Add, args)
			Expect(ipldErr).ToNot(HaveOccurred())

			// Sleep for service interval + few extra seconds
			time.Sleep(time.Duration(serviceInterval+2) * time.Second)

			// WatchedAddresses = [GLD1, SLV1]
			// SLV1, countA
			countA1Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV1.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA1 := new(big.Int).SetBytes(countA1Storage)
			Expect(countA1.String()).To(Equal(updatedCountA1.String()))

			// SLV2, countA
			countA2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA2 := new(big.Int).SetBytes(countA2Storage)
			Expect(countA2.String()).To(Equal(oldCountA2.String()))

			// SLV2, countB
			countB2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countBIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countB2 := new(big.Int).SetBytes(countB2Storage)
			Expect(countB2.String()).To(Equal(oldCountB2.String()))
		})
	})

	Context("previously unwatched contract watched (different 'created_at')", func() {
		It("indexes past state from 'created_at' onwards on watching a contract", func() {
			// Watch SLV2 (created_at -> countB incremented) contract
			args := []types.WatchAddressArg{
				{
					Address:   SLV2.Address,
					CreatedAt: uint64(SLV2CountBIncrementedAt.BlockNumber),
				},
			}
			ipldErr := fillServiceRPCClient.Call(nil, ipldMethod, types.Add, args)
			Expect(ipldErr).ToNot(HaveOccurred())

			// Sleep for service interval + few extra seconds
			time.Sleep(time.Duration(serviceInterval+2) * time.Second)

			// WatchedAddresses = [GLD1, SLV1, SLV2]
			// SLV2, countA
			countA2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA2 := new(big.Int).SetBytes(countA2Storage)
			Expect(countA2.String()).To(Equal(oldCountA2.String()))

			// SLV2, countB
			countB2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countBIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countB2 := new(big.Int).SetBytes(countB2Storage)
			Expect(countB2.String()).To(Equal(updatedCountB2.String()))
		})

		It("indexes missing past state on watching a contract from an earlier 'created_at'", func() {
			// Remove SLV2 (created_at -> countB incremented) watched contract
			args := []types.WatchAddressArg{
				{
					Address:   SLV2.Address,
					CreatedAt: uint64(SLV2CountBIncrementedAt.BlockNumber),
				},
			}
			ipldErr := fillServiceRPCClient.Call(nil, ipldMethod, types.Remove, args)
			Expect(ipldErr).ToNot(HaveOccurred())

			// Watch SLV2 (created_at -> deployment) contract
			args = []types.WatchAddressArg{
				{
					Address:   SLV2.Address,
					CreatedAt: uint64(SLV2.BlockNumber),
				},
			}
			ipldErr = fillServiceRPCClient.Call(nil, ipldMethod, types.Add, args)
			Expect(ipldErr).ToNot(HaveOccurred())

			// Sleep for service interval + few extra seconds
			time.Sleep(time.Duration(serviceInterval+2) * time.Second)

			// WatchedAddresses = [GLD1, SLV1, SLV2]
			// SLV2, countA
			countA2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA2 := new(big.Int).SetBytes(countA2Storage)
			Expect(countA2.String()).To(Equal(updatedCountA2.String()))

			// SLV2, countB
			countB2Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV2.Address), common.HexToHash(countBIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countB2 := new(big.Int).SetBytes(countB2Storage)
			Expect(countB2.String()).To(Equal(updatedCountB2.String()))
		})
	})

	Context("destroyed contract redeployed and watched", func() {
		It("returns zero value for destroyed contract", func() {
			_, err := integration.DestroySLVContract(SLV1.Address)
			Expect(err).ToNot(HaveOccurred())

			updatedCountA1 = big.NewInt(0)

			operation := types.Remove
			args := []types.WatchAddressArg{
				{
					Address:   SLV1.Address,
					CreatedAt: uint64(SLV1.BlockNumber),
				},
			}
			ipldErr := fillServiceRPCClient.Call(nil, ipldMethod, operation, args)
			Expect(ipldErr).ToNot(HaveOccurred())

			// WatchedAddresses = [GLD1, SLV2]
			// SLV1, countA
			countA1Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV1.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA1 := new(big.Int).SetBytes(countA1Storage)
			Expect(countA1.String()).To(Equal(updatedCountA1.String()))
			oldCountA1.Set(updatedCountA1)
		})

		It("indexes state for redeployed contract", func() {
			// Redeploy contract
			SLV1, contractErr = integration.Create2Contract("SLVToken", contract1Salt)
			Expect(contractErr).ToNot(HaveOccurred())

			// Add to watched address
			operation := types.Add
			args := []types.WatchAddressArg{
				{
					Address:   SLV1.Address,
					CreatedAt: uint64(SLV1.BlockNumber),
				},
			}
			ipldErr := fillServiceRPCClient.Call(nil, ipldMethod, operation, args)
			Expect(ipldErr).ToNot(HaveOccurred())

			// Sleep for service interval + few extra seconds
			time.Sleep(time.Duration(serviceInterval+2) * time.Second)

			// WatchedAddresses = [GLD1, SLV2, SLV1]
			// SLV1, countA
			countA1Storage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV1.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA1 := new(big.Int).SetBytes(countA1Storage)
			Expect(countA1.String()).To(Equal(updatedCountA1.String()))
			oldCountA1.Set(updatedCountA1)

			// Update storage slots
			_, txErr = integration.IncrementCount(SLV1.Address, "A")
			time.Sleep(sleepInterval)
			Expect(txErr).ToNot(HaveOccurred())
			updatedCountA1.Add(updatedCountA1, big.NewInt(1))

			countA1AfterIncrementStorage, err := ipldClient.StorageAt(ctx, common.HexToAddress(SLV1.Address), common.HexToHash(countAIndex), nil)
			Expect(err).ToNot(HaveOccurred())
			countA1AfterIncrement := new(big.Int).SetBytes(countA1AfterIncrementStorage)
			Expect(countA1AfterIncrement.String()).To(Equal(updatedCountA1.String()))
			oldCountA1.Set(updatedCountA1)
		})
	})
})
