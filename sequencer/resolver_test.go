package sequencer

import (
	"fmt"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

type testContractsResolver struct {
	contractsByProcess map[types.ProcessID]*web3.Contracts
	errByProcess       map[types.ProcessID]error
}

func (r *testContractsResolver) ContractsForProcess(processID types.ProcessID) (*web3.Contracts, error) {
	if err := r.errByProcess[processID]; err != nil {
		return nil, err
	}
	return r.contractsByProcess[processID], nil
}

func TestResolveContractsForProcess(t *testing.T) {
	c := qt.New(t)

	processID := types.NewProcessID(
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		[4]byte{0x01, 0x02, 0x03, 0x04},
		1,
	)
	contracts := &web3.Contracts{}

	resolved, err := resolveContractsForProcess(&testContractsResolver{
		contractsByProcess: map[types.ProcessID]*web3.Contracts{
			processID: contracts,
		},
	}, processID)
	c.Assert(err, qt.IsNil)
	c.Assert(resolved, qt.Equals, contracts)
}

func TestResolveContractsForProcessErrors(t *testing.T) {
	c := qt.New(t)

	processID := types.NewProcessID(
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
		[4]byte{0x05, 0x06, 0x07, 0x08},
		2,
	)

	_, err := resolveContractsForProcess(nil, processID)
	c.Assert(err, qt.ErrorMatches, "contracts resolver is not configured")

	_, err = resolveContractsForProcess(&testContractsResolver{
		errByProcess: map[types.ProcessID]error{
			processID: fmt.Errorf("runtime not found"),
		},
	}, processID)
	c.Assert(err, qt.ErrorMatches, fmt.Sprintf("resolve contracts for process %s: runtime not found", processID.String()))

	_, err = resolveContractsForProcess(&testContractsResolver{
		contractsByProcess: map[types.ProcessID]*web3.Contracts{
			processID: nil,
		},
	}, processID)
	c.Assert(err, qt.ErrorMatches, fmt.Sprintf("resolve contracts for process %s: nil contracts", processID.String()))
}

func TestStateRootGetterUsesResolver(t *testing.T) {
	c := qt.New(t)

	processID := types.NewProcessID(
		common.HexToAddress("0x0000000000000000000000000000000000000003"),
		[4]byte{0x09, 0x0a, 0x0b, 0x0c},
		3,
	)

	getter := stateRootGetter(&testContractsResolver{
		errByProcess: map[types.ProcessID]error{
			processID: fmt.Errorf("runtime not found"),
		},
	})
	c.Assert(getter, qt.Not(qt.IsNil))

	root, err := getter(processID)
	c.Assert(root, qt.IsNil)
	c.Assert(err, qt.ErrorMatches, fmt.Sprintf("resolve contracts for process %s: runtime not found", processID.String()))
}
