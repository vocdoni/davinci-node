# ScalarECIES Encryption Scheme

**sECIES** is an asymmetric encryption scheme based on elliptic curves. It allows encryption of scalar messages (big integers). 

Traditional ECIES typically encrypts arbitrary-length messages using symmetric encryption (like AES) after deriving a shared secret. In contrast, sECIES directly encrypts scalar messages, simplifying the scheme.

### Key Generation

- **Private Key ($sk$)**: A randomly selected scalar in $\mathbb{F}_q$, the scalar field of the curve.
- **Public Key ($PK$)**: The point on the curve computed as:

  $PK = sk \cdot G$

  where $G$ is the generator point of the BabyJubJub curve.

### Encryption

Given:

- **Message ($m$)**: A scalar in $\mathbb{F}_q$.
- **Recipient's Public Key ($PK$)**.

Steps:

1. **Generate Ephemeral Scalar ($r$)**:
   Randomly select $r$ in $\mathbb{F}_q$, ensuring $r \neq 0$.

2. **Compute Ephemeral Public Key ($R$)**:

   $R = r \cdot G$

3. **Compute Shared Secret Point ($S$)**:

   $S = r \cdot PK$

4. **Derive Shared Secret Scalar ($s$)**:

   Hash the point $S$ to obtain a scalar:

   $s = \text{Hash}(S)$

   The hash function maps the elliptic curve point $S$ to a scalar in $\mathbb{F}_q$.

5. **Compute Ciphertext ($c$)**:

   $c = (m + s) \mod q$

6. **Output**:

   The ciphertext is the pair $(R, c)$, where:

   - $R$ is the ephemeral public key (a point on the curve).
   - $c$ is the masked message scalar.

### Decryption

Given:

- **Ciphertext ($R, c$)**.
- **Recipient's Private Key ($sk$)**.

Steps:

1. **Compute Shared Secret Point ($S'$)**:

   $S' = sk \cdot R$

2. **Derive Shared Secret Scalar ($s'$)**:

   Hash the point $S'$ to obtain the same scalar used during encryption:

   $s' = \text{Hash}(S')$

3. **Recover Message ($m$)**:

   $m = (c - s') \mod q$

   This effectively reverses the masking applied during encryption.

## Security Justification

### Confidentiality

- **Ephemeral Scalar ($r$)**: The use of a random ephemeral scalar $r$ for each encryption ensures that even if the same message $m$ is encrypted multiple times, the ciphertexts will be different due to different $R$ and $s$.
- **Shared Secret**: The shared secret point $S$ is derived using the sender's ephemeral private key $r$ and the recipient's public key $pk$. Without knowledge of $r$ or $sk$, an attacker cannot compute $S$ or $s$.
- **Hash Function**: Hashing $S$ to derive $s$ ensures that the scalar used to mask the message is uniformly random in $\mathbb{F}_r$, making it computationally infeasible to recover $s$ without the private key.

### Security Assumptions

The security of ScalarECIES relies on the **Elliptic Curve Decisional Diffie-Hellman (DDH)** assumption:

- **DDH Assumption**: Given $G$, $aG$, $bG$, and $cG$, it is computationally infeasible to determine whether $c = ab \mod q$ without knowing $a$ or $b$.

In this scheme:

- **Encryption**:

  - $R = rG$
  - $S = r(PK) = r(sk)G = (r \cdot sk)G$

- **Decryption**:

  - $S' = sk(R) = sk(rG) = (sk \cdot r)G$

Since $S = S'$, both parties derive the same shared secret point.

An attacker observing $R$ and $c$ cannot compute $s$ without solving the Diffie-Hellman problem, which is considered hard.

### Resistance to Attacks

- **Chosen Plaintext Attack (CPA)**: The scheme is secure under CPA, as the ciphertext does not leak information about $m$ due to the random $s$ masking.
- **Replay Attack**: The use of an ephemeral $r$ for each encryption prevents replay attacks.
- **Collision Resistance**: The hash function used must be collision-resistant to prevent an attacker from finding two different $S$ that hash to the same $s$.
