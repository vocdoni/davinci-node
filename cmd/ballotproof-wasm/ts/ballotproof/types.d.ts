// types.d.ts

declare global {
    export interface Window {
        Go: any;
        BallotProofWasm: BallotProofWasm;
    }
}

export interface BallotProofWasmResult {
    data: string;
    error: string;
}

export interface BallotProofWasm {
    proofInputs(jsonInputs: string) : BallotProofWasmResult;
}

// Interface for the ballotMode object
export interface BallotMode {
    maxCount: number;
    forceUniqueness: boolean;
    maxValue: bigint;
    minValue: bigint;
    maxTotalCost: bigint;
    minTotalCost: bigint;
    costExponent: number;
    costFromWeight: boolean;
}

// Interface for the whole inputs object
export interface WasmInputs {
    address: string;
    processId: string;
    secret: string;
    encryptionKey: bigint[];
    k: bigint;
    ballotMode: BallotMode;
    weight: bigint;
    fieldValues: bigint[];
}

export interface BallotProofInputs {
    address: string;
    cipherfields: string[];
    commitment: string;
    cost_exp: string;
    cost_from_weight: string;
    fields: string[];
    force_uniqueness: string;
    k: string;
    max_count: string;
    max_total_cost: string;
    max_value: string;
    min_total_cost: string;
    min_value: string;
    process_id: string;
    secret: string;
    weight: string;
    nullifier: string;
    pk: string[];
}

export interface WasmResults {
    signatureHash: string;
    circuitInputs: BallotProofInputs;
}

export { }