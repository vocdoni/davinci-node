import fs from 'fs/promises';
import { WASI } from 'wasi';

async function main() {
  const wasmBuffer = await fs.readFile(new URL('./encrypt.wasi', import.meta.url));
  const wasi = new WASI({ version: 'preview1', args: [], env: {}, preopens: {} });
  const importObject = { wasi_snapshot_preview1: wasi.wasiImport };
  const { instance } = await WebAssembly.instantiate(wasmBuffer, importObject);
  wasi.start(instance);
  const { encrypt } = instance.exports;
  if (typeof encrypt !== 'function') {
    console.error('encrypt export is not a function');
    process.exit(1);
  }
  const inputValue = 42;
  const result = encrypt(inputValue);
  console.log('encrypt(', inputValue, ') =', result);
}

main().catch(err => {
  console.error('Error executing wasm:', err);
  process.exit(1);
});