const test = require('node:test');
const assert = require('node:assert/strict');

const { parseCompileLog } = require('./circuit-constraints-log');

test('parseCompileLog parses structured compile logs', () => {
  const metrics = parseCompileLog(`
2026-05-19T10:33:16.035+02:00 DBG results/compile.go:22 > circuit definition compiled circuit=resultsverifier nbConstraints=138864 nbPublicInputs=8 nbPrivateInputs=208 took=0.268s
`);

  assert.deepEqual(metrics, {
    resultsverifier: {
      nbConstraints: 138864,
      nbPublicInputs: 8,
      nbPrivateInputs: 208,
    },
  });
});

test('parseCompileLog parses legacy structured compile logs', () => {
  const metrics = parseCompileLog(`
2026-05-19T10:33:16.035+02:00 DBG results/compile.go:22 > circuit definition compiled circuit=resultsverifier nbConstraints=138864 nbPublic=8 nbSecret=208 took=0.268s
`);

  assert.deepEqual(metrics, {
    resultsverifier: {
      nbConstraints: 138864,
      nbPublicInputs: 8,
      nbPrivateInputs: 208,
    },
  });
});

test('parseCompileLog ignores legacy gnark compile logs', () => {
  const metrics = parseCompileLog(`
2026-05-19T10:32:39.246+02:00 INF circuits/voteverifier/compile.go:21 > compiling circuit definition circuit=voteverifier
10:32:39 INF compiling circuit
10:32:39 INF parsed circuit inputs nbPublicInputs=5 nbPrivateInputs=61
10:32:40 INF building constraint builder nbConstraints=1333567
`);

  assert.deepEqual(metrics, {});
});
