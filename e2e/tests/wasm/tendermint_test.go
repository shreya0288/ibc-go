//go:build !test_e2e

package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"

	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	testifysuite "github.com/stretchr/testify/suite"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/cosmos/ibc-go/e2e/testsuite"
	"github.com/cosmos/ibc-go/e2e/testsuite/query"
	"github.com/cosmos/ibc-go/e2e/testvalues"
	wasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

const (
	wasmSimdImage = "ghcr.io/cosmos/ibc-go-wasm-simd"

	defaultWasmClientID = "08-wasm-0"
)

func TestWasmTmTestSuite(t *testing.T) {
	// TODO: this value should be passed in via the config file / CI, not hard coded in the test.
	// This configuration can be handled in https://github.com/cosmos/ibc-go/issues/4697
	if testsuite.IsCI() && !testsuite.IsFork() {
		t.Setenv(testsuite.ChainImageEnv, wasmSimdImage)
	}

	// wasm tests require a longer voting period to account for the time it takes to upload a contract.
	testvalues.VotingPeriod = time.Minute * 5

	testifysuite.Run(t, new(WasmTmTestSuite))
}

type WasmTmTestSuite struct {
	testsuite.E2ETestSuite
}

func (s *WasmTmTestSuite) SetupWasmTendermintPath(testName string) {
	ctx := context.TODO()
	chainA, chainB := s.GetChains()

	simd1, ok := chainA.(*cosmos.CosmosChain)
	s.Require().True(ok)

	simd2, ok := chainB.(*cosmos.CosmosChain)
	s.Require().True(ok)

	file, err := os.Open("./contracts/cw_ics07_tendermint.wasm.gz")
	s.Require().NoError(err)

	cosmosWallet := s.CreateUserOnChainB(ctx, testvalues.StartingTokenAmount)

	err = testutil.WaitForBlocks(ctx, 1, simd2)
	s.Require().NoError(err, "cosmos chain failed to make blocks")

	s.T().Logf("waited for blocks cosmos wallet")

	checksum := s.PushNewWasmClientProposal(ctx, simd2, cosmosWallet, file)
	s.Require().NotEmpty(checksum, "checksum was empty but should not have been")
	s.T().Log("pushed wasm client proposal")

	r := s.GetRelayerForTest(testName)

	err = r.SetClientContractHash(ctx, s.GetRelayerExecReporter(), simd2.Config(), checksum)
	s.Require().NoError(err)
	s.T().Logf("set contract hash %s", checksum)

	err = testutil.WaitForBlocks(ctx, 1, simd1)
	s.Require().NoError(err, "polkadot chain failed to make blocks")

	channelOpts := ibc.DefaultChannelOpts()
	channelOpts.Version = transfertypes.V1
	s.CreatePaths(ibc.DefaultClientOpts(), channelOpts, testName)
}

// TestMsgTransfer_Succeeds_TendermintContract features
// * Pushes a wasm client contract
// * create client, connection, and channel
// * start relayer
// * send transfer over ibc
func (s *WasmTmTestSuite) TestMsgTransfer_Succeeds_TendermintContract() {
}

// TestMsgTransfer_TimesOut_TendermintContract
// * this transfer should timeout
func (s *WasmTmTestSuite) TestMsgTransfer_TimesOut_TendermintContract() {
}

// TestMsgMigrateContract_Success_TendermintContract features
// * Pushes a wasm client contract
// * Pushes a new wasm client contract
// * Migrates the wasm client contract
func (s *WasmTmTestSuite) TestMsgMigrateContract_Success_TendermintContract() {
	t := s.T()
	ctx := context.Background()

	testName := t.Name()
	s.SetupWasmTendermintPath(testName)

	_, chainB := s.GetChains()

	simd2, ok := chainB.(*cosmos.CosmosChain)
	s.Require().True(ok)

	cosmosWallet := s.CreateUserOnChainB(ctx, testvalues.StartingTokenAmount)

	// Do not start relayer

	// This contract is a dummy contract that will always succeed migration.
	// Other entry points are unimplemented.
	migrateFile, err := os.Open("contracts/migrate_success.wasm.gz")
	s.Require().NoError(err)

	// First Store the code
	newChecksum := s.PushNewWasmClientProposal(ctx, simd2, cosmosWallet, migrateFile)
	s.Require().NotEmpty(newChecksum, "checksum was empty but should not have been")

	newChecksumBz, err := hex.DecodeString(newChecksum)
	s.Require().NoError(err)

	// Attempt to migrate the contract
	message := wasmtypes.NewMsgMigrateContract(
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		defaultWasmClientID,
		newChecksumBz,
		[]byte("{}"),
	)

	s.ExecuteAndPassGovV1Proposal(ctx, message, simd2, cosmosWallet)

	clientState, err := query.ClientState(ctx, simd2, defaultWasmClientID)
	s.Require().NoError(err)

	wasmClientState, ok := clientState.(*wasmtypes.ClientState)
	s.Require().True(ok)

	s.Require().Equal(newChecksumBz, wasmClientState.Checksum)
}

// TestMsgMigrateContract_Error_TendermintContract features
// * Pushes a wasm client contract
// * Pushes a new wasm client contract
// * Migrates the wasm client contract with a contract that will always fail migration
func (s *WasmTmTestSuite) TestMsgMigrateContract_Error_TendermintContract() {
	t := s.T()
	ctx := context.Background()

	testName := t.Name()
	s.SetupWasmTendermintPath(testName)

	_, chainB := s.GetChains()

	cosmosChain, ok := chainB.(*cosmos.CosmosChain)
	s.Require().True(ok)

	cosmosWallet := s.CreateUserOnChainB(ctx, testvalues.StartingTokenAmount)

	// Do not start the relayer

	// This contract is a dummy contract that will always fail migration.
	// Other entry points are unimplemented.
	migrateFile, err := os.Open("contracts/migrate_error.wasm.gz")
	s.Require().NoError(err)

	// First Store the code
	newChecksum := s.PushNewWasmClientProposal(ctx, cosmosChain, cosmosWallet, migrateFile)
	s.Require().NotEmpty(newChecksum, "checksum was empty but should not have been")

	newChecksumBz, err := hex.DecodeString(newChecksum)
	s.Require().NoError(err)

	// Attempt to migrate the contract
	message := wasmtypes.NewMsgMigrateContract(
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		defaultWasmClientID,
		newChecksumBz,
		[]byte("{}"),
	)

	err = s.ExecuteGovV1Proposal(ctx, message, cosmosChain, cosmosWallet)
	s.Require().Error(err)

	version := cosmosChain.Nodes()[0].Image.Version
	if govV1FailedReasonFeatureReleases.IsSupported(version) {
		// This is the error string that is returned from the contract
		s.Require().ErrorContains(err, "migration not supported")
	}
}

// TestRecoverClient_Succeeds_TendermintContract features:
// * stores a wasm client contract
// * waits the expiry period and asserts the subject client status has expired
// * creates a substitute wasm client
// * executes a gov proposal to recover the expired client
// * asserts the status of the subject client has been restored to active
func (s *WasmTmTestSuite) TestRecoverClient_Succeeds_TendermintContract() {}

// extractChecksumFromGzippedContent takes a gzipped wasm contract and returns the checksum.
func (s *WasmTmTestSuite) extractChecksumFromGzippedContent(zippedContent []byte) string {
	content, err := wasmtypes.Uncompress(zippedContent, wasmtypes.MaxWasmSize)
	s.Require().NoError(err)

	checksum32 := sha256.Sum256(content)
	return hex.EncodeToString(checksum32[:])
}

// PushNewWasmClientProposal submits a new wasm client governance proposal to the chain.
func (s *WasmTmTestSuite) PushNewWasmClientProposal(ctx context.Context, chain *cosmos.CosmosChain, wallet ibc.Wallet, proposalContentReader io.Reader) string {
	zippedContent, err := io.ReadAll(proposalContentReader)
	s.Require().NoError(err)

	computedChecksum := s.extractChecksumFromGzippedContent(zippedContent)

	s.Require().NoError(err)
	message := wasmtypes.MsgStoreCode{
		Signer:       authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		WasmByteCode: zippedContent,
	}

	s.ExecuteAndPassGovV1Proposal(ctx, &message, chain, wallet)

	codeResp, err := query.GRPCQuery[wasmtypes.QueryCodeResponse](ctx, chain, &wasmtypes.QueryCodeRequest{Checksum: computedChecksum})
	s.Require().NoError(err)

	checksumBz := codeResp.Data
	checksum32 := sha256.Sum256(checksumBz)
	actualChecksum := hex.EncodeToString(checksum32[:])
	s.Require().Equal(computedChecksum, actualChecksum, "checksum returned from query did not match the computed checksum")

	return actualChecksum
}
