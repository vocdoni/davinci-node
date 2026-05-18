const test = require('node:test');
const assert = require('node:assert/strict');

const { formatReviewBody, formatNoDiffReviewBody } = require('./circuit-constraints-review');

test('formatReviewBody uses a metric table', () => {
  const body = formatReviewBody({
    marker: '<!-- circuit-constraints -->',
    baseBranch: 'main',
    baselineSource: 'compiled base branch',
    diffs: [
      {
        circuit: 'aggregator',
        base: { nbConstraints: 3296324, nbPublicInputs: 6, nbPrivateInputs: 960 },
        head: { nbConstraints: 3296324, nbPublicInputs: 7, nbPrivateInputs: 960 },
      },
      {
        circuit: 'resultsverifier',
        base: { nbConstraints: 138864, nbPublicInputs: 10, nbPrivateInputs: 208 },
        head: { nbConstraints: 138864, nbPublicInputs: 11, nbPrivateInputs: 208 },
      },
      {
        circuit: 'statetransition',
        base: { nbConstraints: 13840443, nbPublicInputs: 9, nbPrivateInputs: 17278 },
        head: { nbConstraints: 13840443, nbPublicInputs: 10, nbPrivateInputs: 17278 },
      },
      {
        circuit: 'voteverifier',
        base: { nbConstraints: 1333567, nbPublicInputs: 6, nbPrivateInputs: 61 },
        head: { nbConstraints: 1333567, nbPublicInputs: 7, nbPrivateInputs: 61 },
      },
    ],
  });

  assert.equal(
    body,
    [
      '<!-- circuit-constraints -->',
      'Circuit constraint counts changed compared to base branch `main` (compiled base branch).',
      '',
      '| Circuit | Metric | Base | PR | Diff |',
      '| --- | --- | ---: | ---: | ---: |',
      '| aggregator | nbConstraints | 3296324 | 3296324 | 0 |',
      '| aggregator | nbPublicInputs | 6 | 7 | +1 |',
      '| aggregator | nbPrivateInputs | 960 | 960 | 0 |',
      '| resultsverifier | nbConstraints | 138864 | 138864 | 0 |',
      '| resultsverifier | nbPublicInputs | 10 | 11 | +1 |',
      '| resultsverifier | nbPrivateInputs | 208 | 208 | 0 |',
      '| statetransition | nbConstraints | 13840443 | 13840443 | 0 |',
      '| statetransition | nbPublicInputs | 9 | 10 | +1 |',
      '| statetransition | nbPrivateInputs | 17278 | 17278 | 0 |',
      '| voteverifier | nbConstraints | 1333567 | 1333567 | 0 |',
      '| voteverifier | nbPublicInputs | 6 | 7 | +1 |',
      '| voteverifier | nbPrivateInputs | 61 | 61 | 0 |',
    ].join('\n')
  );
});

test('formatNoDiffReviewBody describes the zero-diff case', () => {
  const body = formatNoDiffReviewBody({
    marker: '<!-- circuit-constraints -->',
    baseBranch: 'main',
    baselineSource: 'artifact baseline',
  });

  assert.equal(
    body,
    [
      '<!-- circuit-constraints -->',
      'This PR no longer changes circuit constraints compared to base branch `main` (artifact baseline).',
    ].join('\n')
  );
});
