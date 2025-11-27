package elgamal

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/types/params"
)

// RandK function generates a random k value for encryption,
// inside the scalar field of the BallotProof curve
func RandK() (*big.Int, error) {
	kBytes := make([]byte, 20)
	_, err := rand.Read(kBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random k: %v", err)
	}
	k := new(big.Int).SetBytes(kBytes)
	return crypto.BigToFF(params.BallotProofCurve.ScalarField(), k), nil
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

// Decrypt decrypts (c1,c2) with the secret key d and searches the
// discrete log m in the interval [0,maxMessage].
//
// It always returns the plaintext point M = c2 – d·c1.
// If m is not contained in the requested interval an error is returned.
func Decrypt(
	publicKey ecc.Point, // the curve generator G is obtained from this value
	privateKey *big.Int, // secret scalar d
	c1, c2 ecc.Point, // ciphertext
	maxMessage uint64, // inclusive upper bound for m
) (M ecc.Point, message *big.Int, err error) {
	if privateKey == nil || privateKey.Sign() <= 0 {
		return nil, nil, fmt.Errorf("Decrypt: empty or negative private key")
	}
	if maxMessage == 0 {
		return nil, nil, fmt.Errorf("Decrypt: maxMessage == 0")
	}

	// recover the plaintext point

	M = c2.New() // allocate point on the correct curve
	M.Set(c2)

	tmp := c1.New()
	tmp.ScalarMult(c1, privateKey) // tmp = d·c1
	tmp.Neg(tmp)                   //        –d·c1
	M.Add(M, tmp)                  // M = c2 – d·c1

	// solve M == m·G on the small interval

	G := publicKey.New()
	G.SetGenerator() // ensure we use the correct generator point
	message, err = BabyStepGiantStepECC(M, G, maxMessage)
	if err != nil {
		return nil, nil, err
	}
	return M, message, nil
}

// BabyStepGiantStepECC implements baby‑step / giant‑step
// algorithm for a known bounded interval.
//
// It is deterministic (so it always finds m when it exists) and uses a
// compressed point encoding as hash‑map key to remove an O(1) string
// allocation at every iteration present in the original version.
func BabyStepGiantStepECC(beta, alpha ecc.Point, max uint64) (*big.Int, error) {
	// compute m = ⌈sqrt(max)⌉ using integer arithmetic only
	m := new(big.Int).Sqrt(new(big.Int).SetUint64(max))
	if new(big.Int).Mul(m, m).Cmp(new(big.Int).SetUint64(max)) < 0 {
		m.Add(m, big.NewInt(1)) // ceil
	}
	mU64 := m.Uint64() // safe: m ≤ sqrt(2·10^9) < 46341

	// baby steps
	baby := alpha.New()
	baby.SetZero()
	table := make(map[string]uint64, mU64+1)

	for j := uint64(0); j < mU64; j++ { // j in [0,m‑1]
		table[pointKey(baby)] = j
		baby.Add(baby, alpha) // (j+1)·G
	}

	// prepare the constant giant‑step increment
	c := alpha.New()
	c.ScalarMult(alpha, m) //  m·G
	c.Neg(c)               // –m·G

	// giant steps
	giant := beta.New()
	giant.Set(beta)
	for i := uint64(0); i <= mU64; i++ { // i in [0,m]
		if j, ok := table[pointKey(giant)]; ok {
			x := new(big.Int).SetUint64(i*mU64 + j)
			if x.Cmp(new(big.Int).SetUint64(max)) <= 0 {
				return x, nil // success
			}
		}
		giant.Add(giant, c) // β ← β – m·G
	}
	return nil, fmt.Errorf("bsgs: discrete log not found in interval")
}

// pointKey returns a compact encoding to use as map key.
func pointKey(p ecc.Point) string {
	return string(p.Marshal())
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
