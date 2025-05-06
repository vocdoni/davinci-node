package elgamal

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"

	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
)

// RandK function generates a random k value for encryption.
func RandK() (*big.Int, error) {
	kBytes := make([]byte, 20)
	_, err := rand.Read(kBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random k: %v", err)
	}
	k := new(big.Int).SetBytes(kBytes)
	return crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), k), nil
}

// Encrypt function encrypts a message using the public key provided as
// elliptic curve point. It generates a random k and returns the two points
// that represent the encrypted message and the random k used to encrypt it.
// It returns an error if any.
func Encrypt(publicKey ecc.Point, msg *big.Int) (ecc.Point, ecc.Point, *big.Int, error) {
	k, err := RandK()
	if err != nil {
		return nil, nil, nil, err
	}
	// encrypt the message using the random k generated
	c1, c2 := EncryptWithK(publicKey, msg, k)
	return c1, c2, k, nil
}

// EncryptWithK function encrypts a message using the public key provided as
// elliptic curve point and the random k value provided. It returns the two
// points that represent the encrypted message.
func EncryptWithK(pubKey ecc.Point, msg, k *big.Int) (ecc.Point, ecc.Point) {
	order := pubKey.Order()
	// ensure the message is within the field
	msg.Mod(msg, order)
	// compute C1 = k * G
	c1 := pubKey.New()
	c1.ScalarBaseMult(k)
	// compute s = k * pubKey
	s := pubKey.New()
	s.ScalarMult(pubKey, k)
	// encode message as point M = message * G
	m := pubKey.New()
	m.ScalarBaseMult(msg)
	// compute C2 = M + s
	c2 := pubKey.New()
	c2.Add(m, s)
	return c1, c2
}

// GenerateKey generates a new public/private ElGamal encryption key pair.
func GenerateKey(curve ecc.Point) (publicKey ecc.Point, privateKey *big.Int, err error) {
	order := curve.Order()
	d, err := rand.Int(rand.Reader, order)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key scalar: %v", err)
	}
	if d.Sign() == 0 {
		d = big.NewInt(1) // avoid zero private keys
	}
	publicKey = curve.New()
	publicKey.SetGenerator()
	publicKey.ScalarMult(publicKey, d)
	return publicKey, d, nil
}

// Decrypt decrypts the given ciphertext (c1, c2) using the private key.
// It returns the point M = c2 - d*c1 and the discrete log message scalar.
// If no solution is found, returns an error.
func Decrypt(publicKey ecc.Point, privateKey *big.Int, c1, c2 ecc.Point, maxMessage uint64) (M ecc.Point, message *big.Int, err error) {
	// Compute M = c2 - d*c1
	dC1 := c2.New()
	dC1.ScalarMult(c1, privateKey)
	dC1.Neg(dC1) // dC1 = -d*c1

	M = c2.New()
	M.Set(c2)
	M.Add(M, dC1) // M = c2 - d*c1

	// Solve discrete log M = message * G
	// We'll use baby-step giant-step from our local function.
	G := publicKey.New()
	G.SetGenerator()

	message, err = BabyStepGiantStepECC(M, G, maxMessage)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find discrete log: %v", err)
	}

	return M, message, nil
}

// BabyStepGiantStepECC solves M = x*G for x in [0, maxMessage]
// using the baby-step giant-step algorithm over elliptic curves.
func BabyStepGiantStepECC(M, G ecc.Point, maxMessage uint64) (*big.Int, error) {
	mSqrt := uint64(math.Sqrt(float64(maxMessage))) + 1

	// Create a map for baby steps
	babySteps := make(map[string]uint64)
	babyStep := M.New()
	babyStep.SetZero()

	// Precompute baby steps: store g1, g2,..., g^mSqrt in a map
	for j := uint64(0); j < mSqrt; j++ {
		key := babyStep.String()
		babySteps[key] = j
		babyStep.Add(babyStep, G)
	}

	// Compute c = mSqrt * (-G)
	c := M.New()
	c.ScalarBaseMult(new(big.Int).SetUint64(mSqrt))
	c.Neg(c)

	// Initialize giant step
	giantStep := M.New()
	giantStep.Set(M)

	for i := uint64(0); i <= mSqrt; i++ {
		key := giantStep.String()
		if j, found := babySteps[key]; found {
			// x = i*mSqrt + j
			x := new(big.Int).SetUint64(i*mSqrt + j)
			return x, nil
		}
		giantStep.Add(giantStep, c)
	}

	return nil, fmt.Errorf("failed to compute discrete logarithm using Baby-Step Giant-Step algorithm")
}

// CheckK checks if a given k was used to produce the ciphertext (c1, c2) under the given publicKey.
// It returns true if c1 == k * G, false otherwise.
// This does not require decrypting the message or computing the discrete log.
func CheckK(c1 ecc.Point, k *big.Int) bool {
	// Compute KCheck = k * G
	KCheck := c1.New()
	KCheck.ScalarBaseMult(k)

	// Compare KCheck with c1
	return KCheck.Equal(c1)
}
