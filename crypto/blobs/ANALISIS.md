Why TestProgressiveElements blows up

The first thing the prover does is build
diff[i] = z – ωᵢ.
If z = ωⱼ for any j, then diff[j] = 0.

Your circuit goes straight on to the “batch-inverse”

go
Copy
Edit
finalProd  = prefixProd[4095] * diff[4095]      // = ∏ᵢ diff[i]
invProd    = frAPI.Inverse(finalProd)           // ← undefined if one diff[i] = 0
frAPI.Inverse(0) produces a witness that cannot satisfy the
multiplication check inside the emulated field gadget, so the solver
fails and you get

scss
Copy
Edit
[assertIsEqual] in emulated.(*mulCheck…).check
When the test has a single non-zero blob element the randomly drawn
evaluation point z sometimes does not hit a root of unity and the
circuit accidentally passes; with 5, 20, 100 elements the test harness
is much more likely to pick a z that does coincide with one of the
ωᵢ, so it fails deterministically.

Fix: handle the “z equals ωᵢ” branch inside the circuit
The KZG reference code treats this as a special case:

kotlin
Copy
Edit
if (z == ω_j) return blob[j];
In a circuit we can’t branch, but we can do the same with constraints.

go
Copy
Edit
// 1.  flag[j] = 1  ⇔  z == ω_j
flag := make([]*emulated.Element[Fr], 4096)
for i := 0; i < 4096; i++ {
    flag[i] = frAPI.IsZero(frAPI.Sub(&c.Z, &omega[i])) // 0/1 boolean
}

// 2.  does z coincide with any root?
isSpecial := flag[0]
for i := 1; i < 4096; i++ {
    isSpecial = frAPI.Add(isSpecial, flag[i])          // will still be 0 or 1
}

// 3.  direct value in the special case
directY := frAPI.Zero()
for i := 0; i < 4096; i++ {
    term := frAPI.Mul(flag[i], &c.Blob[i])             // flag[i]*blob[i]
    directY = frAPI.Add(directY, term)
}

// 4.  make denominators non-zero *only* when we will invert them
safeDiff := make([]*emulated.Element[Fr], 4096)
for i := 0; i < 4096; i++ {
    // if flag[i]=1 add 1 so the denominator becomes 1 instead of 0
    safeDiff[i] = frAPI.Add(diff[i], flag[i])
}
Use safeDiff in the batch-inverse exactly as before; compute the
barycentric sum sum, the factor (z⁴⁰⁹⁶ – 1)/4096, and

go
Copy
Edit
baryY := frAPI.Mul(factor, sum)
Selecting the correct result without branching
go
Copy
Edit
// y = isSpecial * directY  +  (1 - isSpecial) * baryY
one        := frAPI.One()
notSpecial := frAPI.Sub(one, isSpecial)
yExpected  := frAPI.Add(frAPI.Mul(isSpecial, directY),
                        frAPI.Mul(notSpecial, baryY))

frAPI.AssertIsEqual(yExpected, &c.Y)
Because isSpecial is constrained to be 0 or 1 the formula enforces the
right equality in both branches, while every denominator that is
actually inverted is now guaranteed non-zero.

Side notes
Prefix-product indexing (starting prefixProd[0] = 1) is fine; it
matches Montgomery’s variant and produces the same inverses, so it is
not the source of the failure.

Your 12-step repeated-squaring of z already computes z⁴⁰⁹⁶
correctly.

With the special-case path above TestProgressiveElements should turn
green and the circuit will be sound for all evaluation points.
