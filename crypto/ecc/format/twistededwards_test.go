package format

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/test"
)

func TestTE2RTETransform(t *testing.T) {
	x, _ := new(big.Int).SetString("20284931487578954787250358776722960153090567235942462656834196519767860852891", 10)
	y, _ := new(big.Int).SetString("21185575020764391300398134415668786804224896114060668011215204645513129497221", 10)

	expectedRTE, _ := new(big.Int).SetString("5730906301301611931737915251485454905492689746504994962065413628158661689313", 10)
	xPrime, yPrime := FromTEtoRTE(x, y)
	if xPrime.Cmp(expectedRTE) != 0 {
		t.Errorf("Expected %v, got %v", expectedRTE, xPrime)
	} else if yPrime.Cmp(y) != 0 {
		t.Errorf("Expected %v, got %v", y, yPrime)
	}
	xPrimePrime, yPrimePrime := FromRTEtoTE(xPrime, yPrime)
	if xPrimePrime.Cmp(x) != 0 {
		t.Errorf("Expected %v, got %v", x, xPrimePrime)
	} else if yPrimePrime.Cmp(y) != 0 {
		t.Errorf("Expected %v, got %v", y, yPrimePrime)
	}
}

type rteToTEVarCircuit struct {
	XRTE      frontend.Variable `gnark:",public"`
	YRTE      frontend.Variable `gnark:",public"`
	ExpectedX frontend.Variable
	ExpectedY frontend.Variable
}

func (circuit *rteToTEVarCircuit) Define(api frontend.API) error {
	xTE, yTE := FromRTEtoTEVar(api, circuit.XRTE, circuit.YRTE)
	api.AssertIsEqual(xTE, circuit.ExpectedX)
	api.AssertIsEqual(yTE, circuit.ExpectedY)
	xRTE, yRTE := FromTEtoRTEVar(api, xTE, yTE)
	api.AssertIsEqual(xRTE, circuit.XRTE)
	api.AssertIsEqual(yRTE, circuit.YRTE)
	return nil
}

func TestRTEtoTEVarTransform(t *testing.T) {
	x, _ := new(big.Int).SetString("20284931487578954787250358776722960153090567235942462656834196519767860852891", 10)
	y, _ := new(big.Int).SetString("21185575020764391300398134415668786804224896114060668011215204645513129497221", 10)

	xRTE, yRTE := FromTEtoRTE(x, y)

	witness := rteToTEVarCircuit{
		XRTE:      xRTE,
		YRTE:      yRTE,
		ExpectedX: x,
		ExpectedY: y,
	}

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&rteToTEVarCircuit{}, &witness,
		test.WithCurves(ecc.BN254), test.WithBackends(backend.GROTH16))
}

type emulatedRTEtoTECircuit struct {
	XRTE      emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	YRTE      emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	ExpectedX emulated.Element[sw_bn254.ScalarField]
	ExpectedY emulated.Element[sw_bn254.ScalarField]
}

func (circuit *emulatedRTEtoTECircuit) Define(api frontend.API) error {
	xTE, yTE, err := FromEmulatedRTEtoTE(api, circuit.XRTE, circuit.YRTE)
	if err != nil {
		return err
	}
	field, err := emulated.NewField[sw_bn254.ScalarField](api)
	if err != nil {
		return err
	}
	field.AssertIsEqual(&xTE, &circuit.ExpectedX)
	field.AssertIsEqual(&yTE, &circuit.ExpectedY)
	xRTE, yRTE, err := FromEmulatedTEtoRTE(api, xTE, yTE)
	if err != nil {
		return err
	}
	field.AssertIsEqual(&xRTE, &circuit.XRTE)
	field.AssertIsEqual(&yRTE, &circuit.YRTE)
	return nil
}

func TestEmulatedRTEtoTETransform(t *testing.T) {
	x, _ := new(big.Int).SetString("20284931487578954787250358776722960153090567235942462656834196519767860852891", 10)
	y, _ := new(big.Int).SetString("21185575020764391300398134415668786804224896114060668011215204645513129497221", 10)

	xRTE, yRTE := FromTEtoRTE(x, y)

	witness := emulatedRTEtoTECircuit{
		XRTE:      emulated.ValueOf[sw_bn254.ScalarField](xRTE),
		YRTE:      emulated.ValueOf[sw_bn254.ScalarField](yRTE),
		ExpectedX: emulated.ValueOf[sw_bn254.ScalarField](x),
		ExpectedY: emulated.ValueOf[sw_bn254.ScalarField](y),
	}

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&emulatedRTEtoTECircuit{}, &witness,
		test.WithCurves(ecc.BN254), test.WithBackends(backend.GROTH16))
}
