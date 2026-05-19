const test = require('node:test');
const assert = require('node:assert/strict');

const { parseCompileLog } = require('./circuit-constraints-log');

test('parseCompileLog parses legacy gnark compile logs', () => {
  const metrics = parseCompileLog(`
2026-05-19T10:32:39.246+02:00 INF circuits/statetransition/compile.go:21 > compiling circuit definition circuit=statetransition
10:32:39 INF compiling circuit
10:32:39 INF parsed circuit inputs nbPublic=8 nbSecret=17278
10:33:14 INF building constraint builder nbConstraints=13840443
2026-05-19T10:33:16.051+02:00 DBG circuits/results/compile.go:22 > circuit definition compiled circuit=resultsverifier took=0.015s
2026-05-19T10:33:16.052+02:00 INF circuit-compile/main.go:197 > ResultsVerifier setup skipped; circuit unchanged
`);

  assert.deepEqual(metrics, {
    statetransition: {
      nbConstraints: 13840443,
      nbPublic: 8,
      nbSecret: 17278,
    },
  });
});

test('parseCompileLog parses multiple legacy gnark compile logs', () => {
  const metrics = parseCompileLog(`
2026-05-19T10:32:39.246+02:00 INF circuits/voteverifier/compile.go:21 > compiling circuit definition circuit=voteverifier
10:32:39 INF compiling circuit
10:32:39 INF parsed circuit inputs nbPublic=5 nbSecret=61
10:32:40 INF building constraint builder nbConstraints=1333567
2026-05-19T10:32:41.246+02:00 INF circuits/aggregator/compile.go:21 > compiling circuit definition circuit=aggregator
10:32:41 INF compiling circuit
10:32:41 INF parsed circuit inputs nbPublic=5 nbSecret=960
10:33:07 INF building constraint builder nbConstraints=3296324
`);

  assert.deepEqual(metrics, {
    voteverifier: {
      nbConstraints: 1333567,
      nbPublic: 5,
      nbSecret: 61,
    },
    aggregator: {
      nbConstraints: 3296324,
      nbPublic: 5,
      nbSecret: 960,
    },
  });
});

test('parseCompileLog parses structured compile logs', () => {
  const metrics = parseCompileLog(`
2026-05-19T10:33:16.035+02:00 DBG results/compile.go:22 > circuit definition compiled circuit=resultsverifier nbConstraints=138864 nbPublic=9 nbSecret=208 took=0.268s
`);

  assert.deepEqual(metrics, {
    resultsverifier: {
      nbConstraints: 138864,
      nbPublic: 9,
      nbSecret: 208,
    },
  });
});
