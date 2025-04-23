/// <reference path="./types.d.ts" />
import "../dist/wasm_exec.js"; // Import wasm_exec.js with its extension
import { BallotProofWasm, WasmInputs, WasmResults } from "./types.js";

export class BallotProof {
    private go!: InstanceType<typeof window.Go>;
    private wasmInstance: WebAssembly.Instance | null = null;
    private ballotProofWasm: BallotProofWasm | null = null;

    constructor() {
        this.go = new window.Go();
    }

    public async loadWasm(wasmUrl: string): Promise<void> {
        try {
            const response = await fetch(wasmUrl);
            if (!response.ok) {
                throw new Error(`Failed to fetch wasm file: ${wasmUrl}`);
            }
            const bytes = await response.arrayBuffer();
            const result = await WebAssembly.instantiate(bytes, this.go.importObject);
            this.wasmInstance = result.instance;
            this.go.run(result.instance);
            this.ballotProofWasm = (window as any).BallotProofWasm as BallotProofWasm;
            console.log("Go WebAssembly module loaded and running.");
        } catch (error) {
            console.error("Error loading WASM:", error);
        }
    }

    public generateCircomInputs(inputs: WasmInputs): WasmResults {
        if (!this.ballotProofWasm) {
            throw new Error("WASM module not loaded");
        }
        const results = this.ballotProofWasm.proofInputs(JSON.stringify(inputs));
        if (results.error) {
            throw new Error(`Error generating inputs: ${results.error}`);
        }
        let parsedResults;
        try {
            parsedResults = JSON.parse(results.data) as WasmResults
        } catch (error) {
            throw new Error(`Error parsing results: ${error}`);
        }
        if (!parsedResults) {
            throw new Error("Parsed results are null or undefined");
        }
        return parsedResults;
    }
}
