# Threshold ElGamal Encryption with Distributed Key Generation over bn254

This repository provides a Go implementation of threshold ElGamal encryption with a Distributed Key Generation (DKG) protocol using elliptic curve. 

## Distributed Key Generation (DKG)

### Purpose

The DKG protocol allows a group of participants to jointly generate a public/private key pair without any single participant knowing the complete private key. Instead, each participant holds a share of the private key, and only a threshold number of participants can collaborate to decrypt messages.


| step                  | message(s)                                                 | purpose                                                 |
| --------------------- | ---------------------------------------------------------- | ------------------------------------------------------- |
| 1. *Setup*            | everyone picks threshold `t` and posts curve generator `G` | parameters                                              |
| 2. *Polynomial*       | each trustee *i* samples secret poly `fᵢ(x)` (deg `t-1`)   | secret sharing                                          |
| 3. *Commitments*      | publish Ci,j=g^{ai,j} for every coefficient                | makes shares publicly verifiable                        |
| 4. *Shares*           | privately send si→j=fᵢ(j) to each peer *j*                 | give peers their piece                                  |
| 5. *Verification*     | peer *j* checks g^{si→j} against commitments               | detects cheating                                        |
| 6. *Aggregate shares* | none                                                       | each trustee sets dj=∑ si→j – **its private key share** |
| 7. *Public key*       | everyone broadcasts Ci,0} once                             |  PK = ∏ Ci,0                                            |



### Protocol Steps

1. **Setup**:
   - Each participant decides on the threshold `t` (minimum number of participants required for decryption) and the total number of participants `n`.

2. **Secret Polynomial Generation**:
   - Each participant generates a random secret polynomial $f_i(x)$ of degree $t - 1$:
     $f_i(x) = a_{i,0} + a_{i,1}x + a_{i,2}x^2 + \dots + a_{i,t-1}x^{t-1}$

     where $a_{i,0}$ is the participant's secret share.

3. **Commitment to Coefficients**:
   - Participants compute public commitments to their polynomial coefficients:
     $C_{i,j} = g^{a_{i,j}}$
     where $g$ is the generator of the elliptic curve group.

4. **Share Computation and Distribution**:
   - Each participant computes shares for every other participant:
     $s_{i,j} = f_i(j)$
     and securely sends $s_{i,j}$ to participant $j$.

5. **Verification of Shares**:
   - Upon receiving shares, participants verify them using the public commitments:
     $g^{s_{i,j}} \stackrel{?}{=} \prod_{k=0}^{t-1} C_{i,k}^{j^k}$
     This ensures that the shares are consistent with the public commitments.

6. **Aggregation of Shares**:
   - Each participant adds up the shares they received, including their own:
     $s_j = \sum_{i=1}^n s_{i,j}$
     This becomes their private key share.

7. **Public Key Computation**:
   - Participants compute the collective public key:
     $PK = \prod_{i=1}^n C_{i,0}$
     which is the product of all participants' constant term commitments.

### Security Features

- **No Trusted Dealer**: The DKG protocol eliminates the need for a trusted party to generate and distribute keys.
- **Threshold Security**: Only a coalition of at least $t$ participants can decrypt messages, enhancing security against collusion and single-point failures.
- **Verifiable Secret Sharing**: Participants can verify the correctness of shares received from others, preventing malicious actors from disrupting the protocol.


## Decryption proof

Given an aggregate ciphertext `(C1,C2)` and a threshold subset `S`:

### 3.1  Each trustee *i ∈ S* publishes **one tuple**

| field | formula             |
| ----- | ------------------- |
| Sᵢ    | dᵢ · C1             |
| A1ᵢ   | rᵢ · G              |
| A2ᵢ   | rᵢ · C1             |
| zᵢ    | rᵢ + e·λᵢ·dᵢ  mod n |

where

* `λᵢ` is the Lagrange coefficient for id *i* (re‑computable by anyone),
* `e = H(G,PK,C1,S,A1,A2)` is the Fiat–Shamir challenge, common to all.

Data exchanged per trustee = 3 group points + 1 scalar.

### 3.2  Combiner (can be *any* node)

```
S   = Σ λᵢ·Sᵢ
A1  = Σ A1ᵢ
A2  = Σ A2ᵢ
z   = Σ zᵢ           (mod n)
M   = C2 – S         // plaintext point
m   = log_G(M)       // baby‑step giant‑step for small domain
proof = (A1,A2,z)
```

The combiner (or several of them for redundancy) then broadcasts `m` **and** `proof`.

### 3.3  Public verification

Anyone—auditors, smart contracts, observers—checks in one call:

```go
err := elgamal.VerifyDecryptionProof(PK, C1, C2, m, proof)
```

### 3.4  Why it is safe

* **No private share dᵢ leaves its owner.**  The value `zᵢ` is information‑theoretically hidden by the random nonce `rᵢ` under the random oracle model.
* If any trustee cheats (wrong `Sᵢ`, forged `zᵢ` …) the final proof fails and the tally is rejected.
* The final proof is a standard Chaum–Pedersen equality‐of‐discrete‐logs statement, so it is compatible with existing verifiers and solidity pre‑compiles.

---

## Reference Implementation

This Go implementation is inspired by and references the Python implementation available at:

[https://github.com/tompetersen/threshold-crypto](https://github.com/tompetersen/threshold-crypto)

---

**Disclaimer**: This code is for educational and experimental purposes. It has not been audited for security and should not be used in production environments without proper security evaluations.
