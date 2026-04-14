
// SPDX-License-Identifier: MIT

pragma solidity ^0.8.28;

/// @title Groth16 verifier template.
/// @author Remco Bloemen
/// @notice Supports verifying Groth16 proofs. Proofs can be in uncompressed
/// (256 bytes) and compressed (128 bytes) format. A view function is provided
/// to compress proofs.
/// @notice See <https://2π.com/23/bn254-compression> for further explanation.
contract Verifier {
    bytes32 constant PROVING_KEY_HASH = 0x541153047c2ce114eb9e5951f869edea62e21e01ffcf6ba427e4e210a594af48;

    /// Some of the provided public input values are larger than the field modulus.
    /// @dev Public input elements are not automatically reduced, as this is can be
    /// a dangerous source of bugs.
    error PublicInputNotInField();

    /// The proof is invalid.
    /// @dev This can mean that provided Groth16 proof points are not on their
    /// curves, that pairing equation fails, or that the proof is not for the
    /// provided public input.
    error ProofInvalid();
    /// The commitment is invalid
    /// @dev This can mean that provided commitment points and/or proof of knowledge are not on their
    /// curves, that pairing equation fails, or that the commitment and/or proof of knowledge is not for the
    /// commitment key.
    error CommitmentInvalid();

    // Addresses of precompiles
    uint256 constant PRECOMPILE_MODEXP = 0x05;
    uint256 constant PRECOMPILE_ADD = 0x06;
    uint256 constant PRECOMPILE_MUL = 0x07;
    uint256 constant PRECOMPILE_VERIFY = 0x08;

    // Base field Fp order P and scalar field Fr order R.
    // For BN254 these are computed as follows:
    //     t = 4965661367192848881
    //     P = 36⋅t⁴ + 36⋅t³ + 24⋅t² + 6⋅t + 1
    //     R = 36⋅t⁴ + 36⋅t³ + 18⋅t² + 6⋅t + 1
    uint256 constant P = 0x30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd47;
    uint256 constant R = 0x30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000001;

    // Extension field Fp2 = Fp[i] / (i² + 1)
    // Note: This is the complex extension field of Fp with i² = -1.
    //       Values in Fp2 are represented as a pair of Fp elements (a₀, a₁) as a₀ + a₁⋅i.
    // Note: The order of Fp2 elements is *opposite* that of the pairing contract, which
    //       expects Fp2 elements in order (a₁, a₀). This is also the order in which
    //       Fp2 elements are encoded in the public interface as this became convention.

    // Constants in Fp
    uint256 constant FRACTION_1_2_FP = 0x183227397098d014dc2822db40c0ac2ecbc0b548b438e5469e10460b6c3e7ea4;
    uint256 constant FRACTION_27_82_FP = 0x2b149d40ceb8aaae81be18991be06ac3b5b4c5e559dbefa33267e6dc24a138e5;
    uint256 constant FRACTION_3_82_FP = 0x2fcd3ac2a640a154eb23960892a85a68f031ca0c8344b23a577dcf1052b9e775;

    // Exponents for inversions and square roots mod P
    uint256 constant EXP_INVERSE_FP = 0x30644E72E131A029B85045B68181585D97816A916871CA8D3C208C16D87CFD45; // P - 2
    uint256 constant EXP_SQRT_FP = 0xC19139CB84C680A6E14116DA060561765E05AA45A1C72A34F082305B61F3F52; // (P + 1) / 4;

    // Groth16 alpha point in G1
    uint256 constant ALPHA_X = 14192944036342696593098888501531188446654942018859364987082484377357951273614;
    uint256 constant ALPHA_Y = 8245132665753946015332534536779101679139707985291402507106036038260746130988;

    // Groth16 beta point in G2 in powers of i
    uint256 constant BETA_NEG_X_0 = 16205663851980398226884240317853271223207400860741133259906337661810363824902;
    uint256 constant BETA_NEG_X_1 = 9924792458806233121700284531797828048285721041097569509749222134151340314890;
    uint256 constant BETA_NEG_Y_0 = 11917506927804361962320192144667321769135279572259863636616597829438098996460;
    uint256 constant BETA_NEG_Y_1 = 915045204947945044635692648942486019934573960889361552473534046118848743927;

    // Groth16 gamma point in G2 in powers of i
    uint256 constant GAMMA_NEG_X_0 = 17732917879262773490399858352297519846448428023227194846744342634390779934392;
    uint256 constant GAMMA_NEG_X_1 = 7878566181408430864633181710878642308805415904843437466611058506663490931337;
    uint256 constant GAMMA_NEG_Y_0 = 7241371546248154035298399873465853232984762308976764567282709658470392671887;
    uint256 constant GAMMA_NEG_Y_1 = 21813087073777708940667668304179058348848778629454008839625628117469489504866;

    // Groth16 delta point in G2 in powers of i
    uint256 constant DELTA_NEG_X_0 = 18346325241539358589722326610760090880841020195699979165905496128005658424606;
    uint256 constant DELTA_NEG_X_1 = 12198996894522886843776041301313829428037832763265171638209717133545207366194;
    uint256 constant DELTA_NEG_Y_0 = 14651527661028271036882549024116179106680027589403152789895961970791658481158;
    uint256 constant DELTA_NEG_Y_1 = 5340630624035613143033724135298369882264576610904253709450609145250518177126;
    // Pedersen G point in G2 in powers of i
    uint256 constant PEDERSEN_G_X_0 = 6424660541098550766553501121319515890400501799047826138221343207368362889397;
    uint256 constant PEDERSEN_G_X_1 = 2341383299406621316887916929070244460672161932073576258943881121142599907370;
    uint256 constant PEDERSEN_G_Y_0 = 6389850836809771896203447658485775881059691234070463890383364109529759475750;
    uint256 constant PEDERSEN_G_Y_1 = 14109933186369235167735035575607510682302955967168604877549013152604890131635;

    // Pedersen GSigmaNeg point in G2 in powers of i
    uint256 constant PEDERSEN_GSIGMANEG_X_0 = 1208058776422324806696360424404077397900823615680640844669811419279228406297;
    uint256 constant PEDERSEN_GSIGMANEG_X_1 = 10074629633755857822841484417047590427557114963572222625998453807801834564284;
    uint256 constant PEDERSEN_GSIGMANEG_Y_0 = 6576019012195637990408210153992537377046124175164123127998661033417139154224;
    uint256 constant PEDERSEN_GSIGMANEG_Y_1 = 6778454956287257365219239004369733608624334963468734743069226538554795892686;

    // Constant and public input points
    uint256 constant CONSTANT_X = 10487277559079336437771227221036066843707272665170068085869652767829266125405;
    uint256 constant CONSTANT_Y = 4902050826074685551679142593340489039201971971295759471327158957446980077704;
    uint256 constant PUB_0_X = 12738338308864401028415955554509255252915304433529553352589841035605216736098;
    uint256 constant PUB_0_Y = 2538074274274969113671623112969711427034733532013072370808401872167669959916;
    uint256 constant PUB_1_X = 8186403179734481925622374181323073929359544882416054566856325139561065434869;
    uint256 constant PUB_1_Y = 3480662275958679154626933584631665834223047495887886295710980650752840519400;
    uint256 constant PUB_2_X = 14441323283251358273561664804139661062396167599250168810367182433012944499106;
    uint256 constant PUB_2_Y = 11803378616763534929163997219759013661917659931083331877993411907196080174297;
    uint256 constant PUB_3_X = 887980065855752063453843299910486141760250351045049824610425747743158397346;
    uint256 constant PUB_3_Y = 18331259915569782035025383595464856224758079950355733285044941440051406599851;
    uint256 constant PUB_4_X = 6111447708785405191049845193049958477781277693007062179017219620942610595855;
    uint256 constant PUB_4_Y = 17398601474829722465914769996880822559071610873216846691944949596262220195878;
    uint256 constant PUB_5_X = 7973907049819880466582118042298871047274702531341898193021263756750709820919;
    uint256 constant PUB_5_Y = 2977966192551468128101182107051715067353162036547889775413751989244452412028;
    uint256 constant PUB_6_X = 19851754139524185306848688486486627281685516691513498108513867843208388924330;
    uint256 constant PUB_6_Y = 15739633213002159132851918372893664274952503901144435219415289031133162346473;
    uint256 constant PUB_7_X = 378425114436046347018401181458277456255844029599110269676502531447442662346;
    uint256 constant PUB_7_Y = 13951119982512105214877661972083999805803285053316680003530173588851995601281;
    uint256 constant PUB_8_X = 6650777129114962640079811378371910836295521711270977106236359111646031622904;
    uint256 constant PUB_8_Y = 3014639397442274993825415611424640776650843530808801345401067228232062758614;
    uint256 constant PUB_9_X = 20426407683246320044340488789308936794478971588185242106805212994421249872436;
    uint256 constant PUB_9_Y = 8589018338357235497378670344886242256507147946496922847302611084668104523900;

    /// Negation in Fp.
    /// @notice Returns a number x such that a + x = 0 in Fp.
    /// @notice The input does not need to be reduced.
    /// @param a the base
    /// @return x the result
    function negate(uint256 a) internal pure returns (uint256 x) {
        unchecked {
            x = (P - (a % P)) % P; // Modulo is cheaper than branching
        }
    }

    /// Exponentiation in Fp.
    /// @notice Returns a number x such that a ^ e = x in Fp.
    /// @notice The input does not need to be reduced.
    /// @param a the base
    /// @param e the exponent
    /// @return x the result
    function exp(uint256 a, uint256 e) internal view returns (uint256 x) {
        bool success;
        assembly ("memory-safe") {
            let f := mload(0x40)
            mstore(f, 0x20)
            mstore(add(f, 0x20), 0x20)
            mstore(add(f, 0x40), 0x20)
            mstore(add(f, 0x60), a)
            mstore(add(f, 0x80), e)
            mstore(add(f, 0xa0), P)
            success := staticcall(gas(), PRECOMPILE_MODEXP, f, 0xc0, f, 0x20)
            x := mload(f)
        }
        if (!success) {
            // Exponentiation failed.
            // Should not happen.
            revert ProofInvalid();
        }
    }

    /// Invertsion in Fp.
    /// @notice Returns a number x such that a * x = 1 in Fp.
    /// @notice The input does not need to be reduced.
    /// @notice Reverts with ProofInvalid() if the inverse does not exist
    /// @param a the input
    /// @return x the solution
    function invert_Fp(uint256 a) internal view returns (uint256 x) {
        x = exp(a, EXP_INVERSE_FP);
        if (mulmod(a, x, P) != 1) {
            // Inverse does not exist.
            // Can only happen during G2 point decompression.
            revert ProofInvalid();
        }
    }

    /// Square root in Fp.
    /// @notice Returns a number x such that x * x = a in Fp.
    /// @notice Will revert with InvalidProof() if the input is not a square
    /// or not reduced.
    /// @param a the square
    /// @return x the solution
    function sqrt_Fp(uint256 a) internal view returns (uint256 x) {
        x = exp(a, EXP_SQRT_FP);
        if (mulmod(x, x, P) != a) {
            // Square root does not exist or a is not reduced.
            // Happens when G1 point is not on curve.
            revert ProofInvalid();
        }
    }

    /// Square test in Fp.
    /// @notice Returns whether a number x exists such that x * x = a in Fp.
    /// @notice Will revert with InvalidProof() if the input is not a square
    /// or not reduced.
    /// @param a the square
    /// @return x the solution
    function isSquare_Fp(uint256 a) internal view returns (bool) {
        uint256 x = exp(a, EXP_SQRT_FP);
        return mulmod(x, x, P) == a;
    }

    /// Square root in Fp2.
    /// @notice Fp2 is the complex extension Fp[i]/(i^2 + 1). The input is
    /// a0 + a1 ⋅ i and the result is x0 + x1 ⋅ i.
    /// @notice Will revert with InvalidProof() if
    ///   * the input is not a square,
    ///   * the hint is incorrect, or
    ///   * the input coefficients are not reduced.
    /// @param a0 The real part of the input.
    /// @param a1 The imaginary part of the input.
    /// @param hint A hint which of two possible signs to pick in the equation.
    /// @return x0 The real part of the square root.
    /// @return x1 The imaginary part of the square root.
    function sqrt_Fp2(uint256 a0, uint256 a1, bool hint) internal view returns (uint256 x0, uint256 x1) {
        // If this square root reverts there is no solution in Fp2.
        uint256 d = sqrt_Fp(addmod(mulmod(a0, a0, P), mulmod(a1, a1, P), P));
        if (hint) {
            d = negate(d);
        }
        // If this square root reverts there is no solution in Fp2.
        x0 = sqrt_Fp(mulmod(addmod(a0, d, P), FRACTION_1_2_FP, P));
        x1 = mulmod(a1, invert_Fp(mulmod(x0, 2, P)), P);

        // Check result to make sure we found a root.
        // Note: this also fails if a0 or a1 is not reduced.
        if (a0 != addmod(mulmod(x0, x0, P), negate(mulmod(x1, x1, P)), P)
        ||  a1 != mulmod(2, mulmod(x0, x1, P), P)) {
            revert ProofInvalid();
        }
    }

    /// Compress a G1 point.
    /// @notice Reverts with InvalidProof if the coordinates are not reduced
    /// or if the point is not on the curve.
    /// @notice The point at infinity is encoded as (0,0) and compressed to 0.
    /// @param x The X coordinate in Fp.
    /// @param y The Y coordinate in Fp.
    /// @return c The compresed point (x with one signal bit).
    function compress_g1(uint256 x, uint256 y) internal view returns (uint256 c) {
        if (x >= P || y >= P) {
            // G1 point not in field.
            revert ProofInvalid();
        }
        if (x == 0 && y == 0) {
            // Point at infinity
            return 0;
        }

        // Note: sqrt_Fp reverts if there is no solution, i.e. the x coordinate is invalid.
        uint256 y_pos = sqrt_Fp(addmod(mulmod(mulmod(x, x, P), x, P), 3, P));
        if (y == y_pos) {
            return (x << 1) | 0;
        } else if (y == negate(y_pos)) {
            return (x << 1) | 1;
        } else {
            // G1 point not on curve.
            revert ProofInvalid();
        }
    }

    /// Decompress a G1 point.
    /// @notice Reverts with InvalidProof if the input does not represent a valid point.
    /// @notice The point at infinity is encoded as (0,0) and compressed to 0.
    /// @param c The compresed point (x with one signal bit).
    /// @return x The X coordinate in Fp.
    /// @return y The Y coordinate in Fp.
    function decompress_g1(uint256 c) internal view returns (uint256 x, uint256 y) {
        // Note that X = 0 is not on the curve since 0³ + 3 = 3 is not a square.
        // so we can use it to represent the point at infinity.
        if (c == 0) {
            // Point at infinity as encoded in EIP196 and EIP197.
            return (0, 0);
        }
        bool negate_point = c & 1 == 1;
        x = c >> 1;
        if (x >= P) {
            // G1 x coordinate not in field.
            revert ProofInvalid();
        }

        // Note: (x³ + 3) is irreducible in Fp, so it can not be zero and therefore
        //       y can not be zero.
        // Note: sqrt_Fp reverts if there is no solution, i.e. the point is not on the curve.
        y = sqrt_Fp(addmod(mulmod(mulmod(x, x, P), x, P), 3, P));
        if (negate_point) {
            y = negate(y);
        }
    }

    /// Compress a G2 point.
    /// @notice Reverts with InvalidProof if the coefficients are not reduced
    /// or if the point is not on the curve.
    /// @notice The G2 curve is defined over the complex extension Fp[i]/(i^2 + 1)
    /// with coordinates (x0 + x1 ⋅ i, y0 + y1 ⋅ i).
    /// @notice The point at infinity is encoded as (0,0,0,0) and compressed to (0,0).
    /// @param x0 The real part of the X coordinate.
    /// @param x1 The imaginary poart of the X coordinate.
    /// @param y0 The real part of the Y coordinate.
    /// @param y1 The imaginary part of the Y coordinate.
    /// @return c0 The first half of the compresed point (x0 with two signal bits).
    /// @return c1 The second half of the compressed point (x1 unmodified).
    function compress_g2(uint256 x0, uint256 x1, uint256 y0, uint256 y1)
    internal view returns (uint256 c0, uint256 c1) {
        if (x0 >= P || x1 >= P || y0 >= P || y1 >= P) {
            // G2 point not in field.
            revert ProofInvalid();
        }
        if ((x0 | x1 | y0 | y1) == 0) {
            // Point at infinity
            return (0, 0);
        }

        // Compute y^2
        // Note: shadowing variables and scoping to avoid stack-to-deep.
        uint256 y0_pos;
        uint256 y1_pos;
        {
            uint256 n3ab = mulmod(mulmod(x0, x1, P), P-3, P);
            uint256 a_3 = mulmod(mulmod(x0, x0, P), x0, P);
            uint256 b_3 = mulmod(mulmod(x1, x1, P), x1, P);
            y0_pos = addmod(FRACTION_27_82_FP, addmod(a_3, mulmod(n3ab, x1, P), P), P);
            y1_pos = negate(addmod(FRACTION_3_82_FP,  addmod(b_3, mulmod(n3ab, x0, P), P), P));
        }

        // Determine hint bit
        // If this sqrt fails the x coordinate is not on the curve.
        bool hint;
        {
            uint256 d = sqrt_Fp(addmod(mulmod(y0_pos, y0_pos, P), mulmod(y1_pos, y1_pos, P), P));
            hint = !isSquare_Fp(mulmod(addmod(y0_pos, d, P), FRACTION_1_2_FP, P));
        }

        // Recover y
        (y0_pos, y1_pos) = sqrt_Fp2(y0_pos, y1_pos, hint);
        if (y0 == y0_pos && y1 == y1_pos) {
            c0 = (x0 << 2) | (hint ? 2  : 0) | 0;
            c1 = x1;
        } else if (y0 == negate(y0_pos) && y1 == negate(y1_pos)) {
            c0 = (x0 << 2) | (hint ? 2  : 0) | 1;
            c1 = x1;
        } else {
            // G1 point not on curve.
            revert ProofInvalid();
        }
    }

    /// Decompress a G2 point.
    /// @notice Reverts with InvalidProof if the input does not represent a valid point.
    /// @notice The G2 curve is defined over the complex extension Fp[i]/(i^2 + 1)
    /// with coordinates (x0 + x1 ⋅ i, y0 + y1 ⋅ i).
    /// @notice The point at infinity is encoded as (0,0,0,0) and compressed to (0,0).
    /// @param c0 The first half of the compresed point (x0 with two signal bits).
    /// @param c1 The second half of the compressed point (x1 unmodified).
    /// @return x0 The real part of the X coordinate.
    /// @return x1 The imaginary poart of the X coordinate.
    /// @return y0 The real part of the Y coordinate.
    /// @return y1 The imaginary part of the Y coordinate.
    function decompress_g2(uint256 c0, uint256 c1)
    internal view returns (uint256 x0, uint256 x1, uint256 y0, uint256 y1) {
        // Note that X = (0, 0) is not on the curve since 0³ + 3/(9 + i) is not a square.
        // so we can use it to represent the point at infinity.
        if (c0 == 0 && c1 == 0) {
            // Point at infinity as encoded in EIP197.
            return (0, 0, 0, 0);
        }
        bool negate_point = c0 & 1 == 1;
        bool hint = c0 & 2 == 2;
        x0 = c0 >> 2;
        x1 = c1;
        if (x0 >= P || x1 >= P) {
            // G2 x0 or x1 coefficient not in field.
            revert ProofInvalid();
        }

        uint256 n3ab = mulmod(mulmod(x0, x1, P), P-3, P);
        uint256 a_3 = mulmod(mulmod(x0, x0, P), x0, P);
        uint256 b_3 = mulmod(mulmod(x1, x1, P), x1, P);

        y0 = addmod(FRACTION_27_82_FP, addmod(a_3, mulmod(n3ab, x1, P), P), P);
        y1 = negate(addmod(FRACTION_3_82_FP,  addmod(b_3, mulmod(n3ab, x0, P), P), P));

        // Note: sqrt_Fp2 reverts if there is no solution, i.e. the point is not on the curve.
        // Note: (X³ + 3/(9 + i)) is irreducible in Fp2, so y can not be zero.
        //       But y0 or y1 may still independently be zero.
        (y0, y1) = sqrt_Fp2(y0, y1, hint);
        if (negate_point) {
            y0 = negate(y0);
            y1 = negate(y1);
        }
    }

    /// Compute the public input linear combination.
    /// @notice Reverts with PublicInputNotInField if the input is not in the field.
    /// @notice Computes the multi-scalar-multiplication of the public input
    /// elements and the verification key including the constant term.
    /// @param input The public inputs. These are elements of the scalar field Fr.
    /// @param publicCommitments public inputs generated from pedersen commitments.
    /// @param commitments The Pedersen commitments from the proof.
    /// @return x The X coordinate of the resulting G1 point.
    /// @return y The Y coordinate of the resulting G1 point.
    function publicInputMSM(
        uint256[9] calldata input,
        uint256[1] memory publicCommitments,
        uint256[2] memory commitments
    )
    internal view returns (uint256 x, uint256 y) {
        // Note: The ECMUL precompile does not reject unreduced values, so we check this.
        // Note: Unrolling this loop does not cost much extra in code-size, the bulk of the
        //       code-size is in the PUB_ constants.
        // ECMUL has input (x, y, scalar) and output (x', y').
        // ECADD has input (x1, y1, x2, y2) and output (x', y').
        // We reduce commitments(if any) with constants as the first point argument to ECADD.
        // We call them such that ecmul output is already in the second point
        // argument to ECADD so we can have a tight loop.
        bool success = true;
        assembly ("memory-safe") {
            let f := mload(0x40)
            let g := add(f, 0x40)
            let s
            mstore(f, CONSTANT_X)
            mstore(add(f, 0x20), CONSTANT_Y)
            mstore(g, mload(commitments))
            mstore(add(g, 0x20), mload(add(commitments, 0x20)))
            success := and(success,  staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_0_X)
            mstore(add(g, 0x20), PUB_0_Y)
            s :=  calldataload(input)
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_1_X)
            mstore(add(g, 0x20), PUB_1_Y)
            s :=  calldataload(add(input, 32))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_2_X)
            mstore(add(g, 0x20), PUB_2_Y)
            s :=  calldataload(add(input, 64))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_3_X)
            mstore(add(g, 0x20), PUB_3_Y)
            s :=  calldataload(add(input, 96))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_4_X)
            mstore(add(g, 0x20), PUB_4_Y)
            s :=  calldataload(add(input, 128))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_5_X)
            mstore(add(g, 0x20), PUB_5_Y)
            s :=  calldataload(add(input, 160))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_6_X)
            mstore(add(g, 0x20), PUB_6_Y)
            s :=  calldataload(add(input, 192))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_7_X)
            mstore(add(g, 0x20), PUB_7_Y)
            s :=  calldataload(add(input, 224))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_8_X)
            mstore(add(g, 0x20), PUB_8_Y)
            s :=  calldataload(add(input, 256))
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))
            mstore(g, PUB_9_X)
            mstore(add(g, 0x20), PUB_9_Y)
            s := mload(publicCommitments)
            mstore(add(g, 0x40), s)
            success := and(success, lt(s, R))
            success := and(success, staticcall(gas(), PRECOMPILE_MUL, g, 0x60, g, 0x40))
            success := and(success, staticcall(gas(), PRECOMPILE_ADD, f, 0x80, f, 0x40))

            x := mload(f)
            y := mload(add(f, 0x20))
        }
        if (!success) {
            // Either Public input not in field, or verification key invalid.
            // We assume the contract is correctly generated, so the verification key is valid.
            revert PublicInputNotInField();
        }
    }

    /// Compress a proof.
    /// @notice Will revert with InvalidProof if the curve points are invalid,
    /// but does not verify the proof itself.
    /// @param proof The uncompressed Groth16 proof. Elements are in the same order as for
    /// verifyProof. I.e. Groth16 points (A, B, C) encoded as in EIP-197.
    /// @param commitments Pedersen commitments from the proof.
    /// @param commitmentPok proof of knowledge for the Pedersen commitments.
    /// @return compressed The compressed proof. Elements are in the same order as for
    /// verifyCompressedProof. I.e. points (A, B, C) in compressed format.
    /// @return compressedCommitments compressed Pedersen commitments from the proof.
    /// @return compressedCommitmentPok compressed proof of knowledge for the Pedersen commitments.
    function compressProof(
        uint256[8] calldata proof,
        uint256[2] calldata commitments,
        uint256[2] calldata commitmentPok
    )
    public view returns (
        uint256[4] memory compressed,
        uint256[1] memory compressedCommitments,
        uint256 compressedCommitmentPok
    ) {
        compressed[0] = compress_g1(proof[0], proof[1]);
        (compressed[2], compressed[1]) = compress_g2(proof[3], proof[2], proof[5], proof[4]);
        compressed[3] = compress_g1(proof[6], proof[7]);
        compressedCommitments[0] = compress_g1(commitments[0], commitments[1]);
        compressedCommitmentPok = compress_g1(commitmentPok[0], commitmentPok[1]);
    }

    /// Verify a Groth16 proof with compressed points.
    /// @notice Reverts with InvalidProof if the proof is invalid or
    /// with PublicInputNotInField the public input is not reduced.
    /// @notice There is no return value. If the function does not revert, the
    /// proof was successfully verified.
    /// @param compressedProof the points (A, B, C) in compressed format
    /// matching the output of compressProof.
    /// @param compressedCommitments compressed Pedersen commitments from the proof.
    /// @param compressedCommitmentPok compressed proof of knowledge for the Pedersen commitments.
    /// @param input the public input field elements in the scalar field Fr.
    /// Elements must be reduced.
    function verifyCompressedProof(
        uint256[4] calldata compressedProof,
        uint256[1] calldata compressedCommitments,
        uint256 compressedCommitmentPok,
        uint256[9] calldata input
    ) public view {
        uint256[1] memory publicCommitments;
        uint256[2] memory commitments;
        uint256[24] memory pairings;
        {
            (commitments[0], commitments[1]) = decompress_g1(compressedCommitments[0]);
            (uint256 Px, uint256 Py) = decompress_g1(compressedCommitmentPok);

            uint256[] memory publicAndCommitmentCommitted;

            publicCommitments[0] = uint256(
                keccak256(
                    abi.encodePacked(
                        commitments[0],
                        commitments[1],
                        publicAndCommitmentCommitted
                    )
                )
            ) % R;
            // Commitments
            pairings[ 0] = commitments[0];
            pairings[ 1] = commitments[1];
            pairings[ 2] = PEDERSEN_GSIGMANEG_X_1;
            pairings[ 3] = PEDERSEN_GSIGMANEG_X_0;
            pairings[ 4] = PEDERSEN_GSIGMANEG_Y_1;
            pairings[ 5] = PEDERSEN_GSIGMANEG_Y_0;
            pairings[ 6] = Px;
            pairings[ 7] = Py;
            pairings[ 8] = PEDERSEN_G_X_1;
            pairings[ 9] = PEDERSEN_G_X_0;
            pairings[10] = PEDERSEN_G_Y_1;
            pairings[11] = PEDERSEN_G_Y_0;

            // Verify pedersen commitments
            bool success;
            assembly ("memory-safe") {
                let f := mload(0x40)

                success := staticcall(gas(), PRECOMPILE_VERIFY, pairings, 0x180, f, 0x20)
                success := and(success, mload(f))
            }
            if (!success) {
                revert CommitmentInvalid();
            }
        }

        {
            (uint256 Ax, uint256 Ay) = decompress_g1(compressedProof[0]);
            (uint256 Bx0, uint256 Bx1, uint256 By0, uint256 By1) = decompress_g2(compressedProof[2], compressedProof[1]);
            (uint256 Cx, uint256 Cy) = decompress_g1(compressedProof[3]);
            (uint256 Lx, uint256 Ly) = publicInputMSM(
                input,
                publicCommitments,
                commitments
            );

            // Verify the pairing
            // Note: The precompile expects the F2 coefficients in big-endian order.
            // Note: The pairing precompile rejects unreduced values, so we won't check that here.
            // e(A, B)
            pairings[ 0] = Ax;
            pairings[ 1] = Ay;
            pairings[ 2] = Bx1;
            pairings[ 3] = Bx0;
            pairings[ 4] = By1;
            pairings[ 5] = By0;
            // e(C, -δ)
            pairings[ 6] = Cx;
            pairings[ 7] = Cy;
            pairings[ 8] = DELTA_NEG_X_1;
            pairings[ 9] = DELTA_NEG_X_0;
            pairings[10] = DELTA_NEG_Y_1;
            pairings[11] = DELTA_NEG_Y_0;
            // e(α, -β)
            pairings[12] = ALPHA_X;
            pairings[13] = ALPHA_Y;
            pairings[14] = BETA_NEG_X_1;
            pairings[15] = BETA_NEG_X_0;
            pairings[16] = BETA_NEG_Y_1;
            pairings[17] = BETA_NEG_Y_0;
            // e(L_pub, -γ)
            pairings[18] = Lx;
            pairings[19] = Ly;
            pairings[20] = GAMMA_NEG_X_1;
            pairings[21] = GAMMA_NEG_X_0;
            pairings[22] = GAMMA_NEG_Y_1;
            pairings[23] = GAMMA_NEG_Y_0;

            // Check pairing equation.
            bool success;
            uint256[1] memory output;
            assembly ("memory-safe") {
                success := staticcall(gas(), PRECOMPILE_VERIFY, pairings, 0x300, output, 0x20)
            }
            if (!success || output[0] != 1) {
                // Either proof or verification key invalid.
                // We assume the contract is correctly generated, so the verification key is valid.
                revert ProofInvalid();
            }
        }
    }

    /// Verify an uncompressed Groth16 proof.
    /// @notice Reverts with InvalidProof if the proof is invalid or
    /// with PublicInputNotInField the public input is not reduced.
    /// @notice There is no return value. If the function does not revert, the
    /// proof was successfully verified.
    /// @param proof the points (A, B, C) in EIP-197 format matching the output
    /// of compressProof.
    /// @param commitments the Pedersen commitments from the proof.
    /// @param commitmentPok the proof of knowledge for the Pedersen commitments.
    /// @param input the public input field elements in the scalar field Fr.
    /// Elements must be reduced.
    function verifyProof(
        uint256[8] calldata proof,
        uint256[2] calldata commitments,
        uint256[2] calldata commitmentPok,
        uint256[9] calldata input
    ) public view {
        // HashToField
        uint256[1] memory publicCommitments;
        uint256[] memory publicAndCommitmentCommitted;

            publicCommitments[0] = uint256(
                keccak256(
                    abi.encodePacked(
                        commitments[0],
                        commitments[1],
                        publicAndCommitmentCommitted
                    )
                )
            ) % R;

        // Verify pedersen commitments
        bool success;
        assembly ("memory-safe") {
            let f := mload(0x40)

            calldatacopy(f, commitments, 0x40) // Copy Commitments
            mstore(add(f, 0x40), PEDERSEN_GSIGMANEG_X_1)
            mstore(add(f, 0x60), PEDERSEN_GSIGMANEG_X_0)
            mstore(add(f, 0x80), PEDERSEN_GSIGMANEG_Y_1)
            mstore(add(f, 0xa0), PEDERSEN_GSIGMANEG_Y_0)
            calldatacopy(add(f, 0xc0), commitmentPok, 0x40)
            mstore(add(f, 0x100), PEDERSEN_G_X_1)
            mstore(add(f, 0x120), PEDERSEN_G_X_0)
            mstore(add(f, 0x140), PEDERSEN_G_Y_1)
            mstore(add(f, 0x160), PEDERSEN_G_Y_0)

            success := staticcall(gas(), PRECOMPILE_VERIFY, f, 0x180, f, 0x20)
            success := and(success, mload(f))
        }
        if (!success) {
            revert CommitmentInvalid();
        }

        (uint256 x, uint256 y) = publicInputMSM(
            input,
            publicCommitments,
            commitments
        );

        // Note: The precompile expects the F2 coefficients in big-endian order.
        // Note: The pairing precompile rejects unreduced values, so we won't check that here.
        assembly ("memory-safe") {
            let f := mload(0x40) // Free memory pointer.

            // Copy points (A, B, C) to memory. They are already in correct encoding.
            // This is pairing e(A, B) and G1 of e(C, -δ).
            calldatacopy(f, proof, 0x100)

            // Complete e(C, -δ) and write e(α, -β), e(L_pub, -γ) to memory.
            // OPT: This could be better done using a single codecopy, but
            //      Solidity (unlike standalone Yul) doesn't provide a way to
            //      to do this.
            mstore(add(f, 0x100), DELTA_NEG_X_1)
            mstore(add(f, 0x120), DELTA_NEG_X_0)
            mstore(add(f, 0x140), DELTA_NEG_Y_1)
            mstore(add(f, 0x160), DELTA_NEG_Y_0)
            mstore(add(f, 0x180), ALPHA_X)
            mstore(add(f, 0x1a0), ALPHA_Y)
            mstore(add(f, 0x1c0), BETA_NEG_X_1)
            mstore(add(f, 0x1e0), BETA_NEG_X_0)
            mstore(add(f, 0x200), BETA_NEG_Y_1)
            mstore(add(f, 0x220), BETA_NEG_Y_0)
            mstore(add(f, 0x240), x)
            mstore(add(f, 0x260), y)
            mstore(add(f, 0x280), GAMMA_NEG_X_1)
            mstore(add(f, 0x2a0), GAMMA_NEG_X_0)
            mstore(add(f, 0x2c0), GAMMA_NEG_Y_1)
            mstore(add(f, 0x2e0), GAMMA_NEG_Y_0)

            // Check pairing equation.
            success := staticcall(gas(), PRECOMPILE_VERIFY, f, 0x300, f, 0x20)
            // Also check returned value (both are either 1 or 0).
            success := and(success, mload(f))
        }
        if (!success) {
            // Either proof or verification key invalid.
            // We assume the contract is correctly generated, so the verification key is valid.
            revert ProofInvalid();
        }
    }
}
