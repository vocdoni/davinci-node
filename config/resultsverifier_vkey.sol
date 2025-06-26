
// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

/// @title Groth16 verifier template.
/// @author Remco Bloemen
/// @notice Supports verifying Groth16 proofs. Proofs can be in uncompressed
/// (256 bytes) and compressed (128 bytes) format. A view function is provided
/// to compress proofs.
/// @notice See <https://2π.com/23/bn254-compression> for further explanation.
contract Verifier {
    bytes32 constant PROVING_KEY_HASH = 0x5f5cae2546e6eb0e4895653ef94e3378ec360e9a5d109af1b52e42425ad0c380;

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
<<<<<<< HEAD
<<<<<<< HEAD
    uint256 constant ALPHA_X = 15442289787703640505733256222852540946574026057639149133767444046290342453650;
    uint256 constant ALPHA_Y = 14527559513477681430547757313114843637492287944251358523352373755245478361201;

    // Groth16 beta point in G2 in powers of i
    uint256 constant BETA_NEG_X_0 = 19326423185726068349891353929264990032783074105060233296631749026673854612012;
    uint256 constant BETA_NEG_X_1 = 11084943766617112440122890069364072479618412379340796823025620351016629631560;
    uint256 constant BETA_NEG_Y_0 = 4471007249261476989195569748937903476776388736024222450777660098682665114547;
    uint256 constant BETA_NEG_Y_1 = 6581648093713496213619175282491551073235600205193690257937277304349293319690;

    // Groth16 gamma point in G2 in powers of i
    uint256 constant GAMMA_NEG_X_0 = 12096509189755338429160004915362119982684070118372348203439525384735953312876;
    uint256 constant GAMMA_NEG_X_1 = 15352728482577884841774724808001767893435325139379333207753174595661470835433;
    uint256 constant GAMMA_NEG_Y_0 = 4820939672290876111828055701270417750773503571178047745314788660348843755903;
    uint256 constant GAMMA_NEG_Y_1 = 20767392801442583497314666552580736920843223089292161398892982463251374986978;

    // Groth16 delta point in G2 in powers of i
    uint256 constant DELTA_NEG_X_0 = 21611819422141176088680757554219868590450079210596345920637789441588789400230;
    uint256 constant DELTA_NEG_X_1 = 18789972710907156277164283964110642800811053402150745980062745460846753694746;
    uint256 constant DELTA_NEG_Y_0 = 5203505599425525337816558069269864388118271652267931829212822365023045978672;
    uint256 constant DELTA_NEG_Y_1 = 7163016217307944448933099672027756630043316516031817955256070081997936239537;
    // Pedersen G point in G2 in powers of i
    uint256 constant PEDERSEN_G_X_0 = 10574584105584644739777045703824483517158463316590707213579234962261333763107;
    uint256 constant PEDERSEN_G_X_1 = 2040769973226211684657048913884297528663972372660896848710264965612572612849;
    uint256 constant PEDERSEN_G_Y_0 = 5231058685308370300425770998145280107976422319414994523555318304732457211942;
    uint256 constant PEDERSEN_G_Y_1 = 12211918024101810895839260925533699470428874370608258881843321334379748302985;

    // Pedersen GSigmaNeg point in G2 in powers of i
    uint256 constant PEDERSEN_GSIGMANEG_X_0 = 3157640819419338012170031020570040899995852216633247086604885956264094463990;
    uint256 constant PEDERSEN_GSIGMANEG_X_1 = 19905628054791222966773180355351479934781964036087318760508963251384150954987;
    uint256 constant PEDERSEN_GSIGMANEG_Y_0 = 11934681583513382518816530456514649760175555917815910091675630027785834318;
    uint256 constant PEDERSEN_GSIGMANEG_Y_1 = 17997771179360327367294063695523710375753732227532732432573699905273119969455;

    // Constant and public input points
    uint256 constant CONSTANT_X = 18563567128732874123734156829320457279388575233673789673781177144503429508778;
    uint256 constant CONSTANT_Y = 7441083746880508534149389293947592631578607126403879777011938456973451282486;
    uint256 constant PUB_0_X = 12542886780465006485915418255830993613482756464575526392650949574726406360014;
    uint256 constant PUB_0_Y = 17626357939988536522089467116474433126952329524239042195679228157233889255903;
    uint256 constant PUB_1_X = 14291518819969287756771165212485246440612581355534349167032575376923938907315;
    uint256 constant PUB_1_Y = 18305283274867318202748828126973915068955713846748114013162875305678604298595;
    uint256 constant PUB_2_X = 4611092477600391927074033748429244914580320765154816179606398242691040766237;
    uint256 constant PUB_2_Y = 10242018922439300962228357930936577959358199858505402804108171377295176069539;
    uint256 constant PUB_3_X = 19687127653095890351026184410043319664933540625690678348955894640214664044673;
    uint256 constant PUB_3_Y = 3123189454088669164037250324032782223912670296260642457637596467983767092231;
    uint256 constant PUB_4_X = 15855726724895679810293463861517414110187735968726236988881136318368919867009;
    uint256 constant PUB_4_Y = 8930754830069747533360702525704281473092101657435641569138318501521411160339;
    uint256 constant PUB_5_X = 11305844719979433793308067280887087192889231460031547659634087445650518089046;
    uint256 constant PUB_5_Y = 15044555314373983805706559292929762921739183175948094200609724836934359904158;
    uint256 constant PUB_6_X = 13823410728901438732541987917151426044840262939809314257596850248800995613714;
    uint256 constant PUB_6_Y = 1518503476906861567860178871863102095161263949174762020238251130049891865621;
    uint256 constant PUB_7_X = 19488379839755123769723458986290092143848782301670978173519237349616745335467;
    uint256 constant PUB_7_Y = 8725007813158050567173776245664950242103168673768729557268726169839321391204;
    uint256 constant PUB_8_X = 1437342364652726823108339281976794059467900518380395441162922818526379116522;
    uint256 constant PUB_8_Y = 19553008746930343418487263082206556220533111774489999849899924303903321676135;
    uint256 constant PUB_9_X = 6366268300940357919361797350957938022468618383909845872039375674552449438621;
    uint256 constant PUB_9_Y = 9021484468232551298435415656398410348416822426242107820814689793053280026056;
=======
    uint256 constant ALPHA_X = 1921332029170745868012543969479316127934247375208950967283614543417403893174;
    uint256 constant ALPHA_Y = 12025449458738792277735914545635653309784483091322413950543395417475673331781;
=======
    uint256 constant ALPHA_X = 7090718406640443178753360388704537645182913072342994963018242157267080103247;
    uint256 constant ALPHA_Y = 10161030047588324929165848332235427145995391829891765156161292989140751191589;
>>>>>>> 2dcd329 (all circuits working)

    // Groth16 beta point in G2 in powers of i
    uint256 constant BETA_NEG_X_0 = 9364357890979235801594251578366102318515771589941856701296076364645190275911;
    uint256 constant BETA_NEG_X_1 = 5204599541485112369195755874532964989226947754547785382211109805481406011830;
    uint256 constant BETA_NEG_Y_0 = 3571360392610065527279529895911583699311133401591516111581128830203192833175;
    uint256 constant BETA_NEG_Y_1 = 16550662296433583370121350545551927740506942725372950030402190383566334612824;

    // Groth16 gamma point in G2 in powers of i
    uint256 constant GAMMA_NEG_X_0 = 6089524723183478433889577178937151960361207748916335638769199500970891238090;
    uint256 constant GAMMA_NEG_X_1 = 10253355188894555844842383062231150883174296695519613505824345854668429399039;
    uint256 constant GAMMA_NEG_Y_0 = 18035762124557091519031635427125074099895070329771532262510868081688918743467;
    uint256 constant GAMMA_NEG_Y_1 = 14956872544294650852675567979840341542528966640222866536936745015751008731469;

    // Groth16 delta point in G2 in powers of i
    uint256 constant DELTA_NEG_X_0 = 386599778640699832016807485615881296757363712974930912179545399571749707659;
    uint256 constant DELTA_NEG_X_1 = 3935670394416784633350221545805058673557040258973926176146523765116746897864;
    uint256 constant DELTA_NEG_Y_0 = 18505373593776673203250685062334488662840168026588043485324303444768146578998;
    uint256 constant DELTA_NEG_Y_1 = 619704391594347588564705988494463472450332271305908000496649807331090643497;
    // Pedersen G point in G2 in powers of i
    uint256 constant PEDERSEN_G_X_0 = 20667836562100249496200689929398078463694612144504584853506986256599186913074;
    uint256 constant PEDERSEN_G_X_1 = 6810848676954841781665154820580092470513831348224547622596221646053026990079;
    uint256 constant PEDERSEN_G_Y_0 = 17545121428782573947173803018954912908790005669009763661545714991533199016822;
    uint256 constant PEDERSEN_G_Y_1 = 2088826518209708866458418735501006709153903462011642887181390038007529846809;

    // Pedersen GSigmaNeg point in G2 in powers of i
    uint256 constant PEDERSEN_GSIGMANEG_X_0 = 12882062512352660419277177929300507012260958941418075040793840937813800633998;
    uint256 constant PEDERSEN_GSIGMANEG_X_1 = 20514324148892369718583177222047586071146461571319252850029002082496020920652;
    uint256 constant PEDERSEN_GSIGMANEG_Y_0 = 3088633079683150045404433021663218263051122394612094297823207689579520901798;
    uint256 constant PEDERSEN_GSIGMANEG_Y_1 = 11616646020285210738297676372172858749280854404747258642906973734122622209169;

    // Constant and public input points
<<<<<<< HEAD
    uint256 constant CONSTANT_X = 9149571782768992277571717292348900489763832861187550297836095547341521448136;
    uint256 constant CONSTANT_Y = 19641153936182095024324756824358599143430656616265052629948296148116844088866;
    uint256 constant PUB_0_X = 20140369216235644696051417793149308466542335581773635045539774928889849940606;
    uint256 constant PUB_0_Y = 8861360130949048079725982207992341015360674065396446864401739901406149732232;
    uint256 constant PUB_1_X = 10497862566245987676119883404808638292865714431308302911855295101045718256047;
    uint256 constant PUB_1_Y = 21344925833216106339154887523847879218149662295295583843392529198620131450687;
    uint256 constant PUB_2_X = 1055124733919394796244742485961360689061014895701272378901331582219053869738;
    uint256 constant PUB_2_Y = 16620058806931485887261769461769891346276989571252940387894026596395723251022;
    uint256 constant PUB_3_X = 9764168679806116291691734632409953150715739120707702554607027721390589523081;
    uint256 constant PUB_3_Y = 17785247691041480786770474995532363488548780164913079247769951154400444181834;
    uint256 constant PUB_4_X = 15419807166130862466874159451126458217834683011508412623991092907361123464531;
    uint256 constant PUB_4_Y = 11288745253216437413219621818392394540753600635776658909117107240184568878848;
    uint256 constant PUB_5_X = 13561337900556167170491611066860066563701086423516414871020063471213623204714;
    uint256 constant PUB_5_Y = 14515034783269815805876261656731660005880527295583096797279068452318152486301;
    uint256 constant PUB_6_X = 9246936368211535203675462578501521482884643995989059226397168565872525573609;
    uint256 constant PUB_6_Y = 10923525221876051820206433848711216006762761170998609451215110685318963910067;
    uint256 constant PUB_7_X = 19514936819140013461734554482175266127822808383514543015021335483120505592509;
    uint256 constant PUB_7_Y = 18908526472001389109553888168282802867493349201441750633407777004193271963345;
    uint256 constant PUB_8_X = 9482650115072173020634766613630472482630209442583955336964885189678209350222;
    uint256 constant PUB_8_Y = 5784042373768728076215841638940285913260660655079928679124416830177913479720;
    uint256 constant PUB_9_X = 11817118366523969781349816727117516277087092053129431088562040286555511952513;
    uint256 constant PUB_9_Y = 1504578881754258765541091898805391792606330467810009388296504081097639674731;
>>>>>>> d41b773 (nullifier and commitment removed from api, circuits and storage, replaced by voter address in every layer)
=======
    uint256 constant CONSTANT_X = 8267010227457523091848171409954852529431940204340503610735256240742380910170;
    uint256 constant CONSTANT_Y = 13485166473480916416209166923347464140251327036629287157798881611783824384156;
    uint256 constant PUB_0_X = 1047800571514798314092929715103006408416044830457646518797238570375661961069;
    uint256 constant PUB_0_Y = 16161762533525151247623469415084944082335794301194632672738497643617830752268;
    uint256 constant PUB_1_X = 20245646726177346801444536812671644137867157313184662063161368177312780492919;
    uint256 constant PUB_1_Y = 1716719761912040786028886724711963966438432883976661407099936970722381425736;
    uint256 constant PUB_2_X = 461678160129389687437266513641234979497955066775845761265224105438541195631;
    uint256 constant PUB_2_Y = 7403299702177207930140161718620907243958290977811126926279233929938490246555;
    uint256 constant PUB_3_X = 7191587789810472795793763575879288939661183536304098651326703539420808918775;
    uint256 constant PUB_3_Y = 21710125264944057306818651286102248752142681312290534856767927467379592538814;
    uint256 constant PUB_4_X = 516768359150022920229975274252326518103645753300420026596923073917061138998;
    uint256 constant PUB_4_Y = 5475190626979425877353778553821961018893768924730897695436994109570803748094;
    uint256 constant PUB_5_X = 3124496293620120793125049971675147178777314209695986057422227069983454866718;
    uint256 constant PUB_5_Y = 1988290941518173940604170005684265767943191264276878591986883664724916698994;
    uint256 constant PUB_6_X = 6867389373004653872362831538955312472743582508776620654757572734836214613935;
    uint256 constant PUB_6_Y = 4267575345237678409353884410603151456169334689747068849944674128613327675545;
    uint256 constant PUB_7_X = 10572022371055852906650083456930222702497942576997191210306469644859641660361;
    uint256 constant PUB_7_Y = 19061267779012643216296394387321473369957473950036167202433210836137782324411;
    uint256 constant PUB_8_X = 1808960397936355154830673637309308430299777902588151691053695931191870142699;
    uint256 constant PUB_8_Y = 19743897459388202713696706506695824475523272731501726923620053159980910678484;
    uint256 constant PUB_9_X = 15231106088942214779650019579220788965034147010315930486171911331651711045822;
    uint256 constant PUB_9_Y = 11587754635593615400225149340750542574908504121399279470760336254967842662837;
>>>>>>> 2dcd329 (all circuits working)

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
