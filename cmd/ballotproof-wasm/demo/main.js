const go = new Go(); 
const WASM_URL = 'main.wasm';
var wasm;

// A function to run after WebAssembly is instantiated.
function postInstantiate(obj) {
    wasm = obj.instance;
    go.run(wasm);

    const inputs = {
        address: "397d72b25676d42f899b18f06633fab9d854235d",
        processId: "1f1e0cd27b4ecd1b71b6333790864ace2870222c",
        secret: "881f648d417540772883ea70e3592d36",
        encryptionKey: [
            "9893338637931860616720507408105297162588837225464624604186540472082423274495",
            "12595438123836047903232785676476920953357035744165788772034206819455277990072"
        ],
        k: "964256131946492867709099996647243890828558919187",
        ballotMode: {
            maxCount: 5,
            forceUniqueness: false,
            maxValue: 16,
            minValue: 0,
            maxTotalCost: 1280,
            minTotalCost: 5,
            costExponent: 2,
            costFromWeight: false
        },
        weight: "10",
        fieldValues: ["14", "9", "8", "9", "0", "0", "0", "0"]
    }

    const fResult = BallotProofWasm.proofInputs(JSON.stringify(inputs));
    if (fResult.error) {
        console.error("Error:", fResult.error);
    }
    console.log("Result:", JSON.parse(fResult.data));
}

(function() {
    if ('instantiateStreaming' in WebAssembly) {
        WebAssembly.instantiateStreaming(fetch(WASM_URL), go.importObject).then(postInstantiate);
    } else {
        fetch(WASM_URL).then(resp => resp.arrayBuffer())
            .then(bytes => WebAssembly.instantiate(bytes, go.importObject)
            .then(postInstantiate))
    }
})()


