const test = require('node:test');
const assert = require('node:assert/strict');

const {
  formatStickyReviewBody,
  resolveStickyReviewPlan,
} = require('./sticky-review');

test('formatStickyReviewBody prefixes the marker and preserves body text', () => {
  const body = formatStickyReviewBody({
    marker: '<!-- sticky-review -->',
    body: [
      'This PR is stacked on top of #12, please merge that one first',
      '',
      'Extra context stays unchanged.',
    ].join('\n'),
  });

  assert.equal(
    body,
    [
      '<!-- sticky-review -->',
      'This PR is stacked on top of #12, please merge that one first',
      '',
      'Extra context stays unchanged.',
    ].join('\n')
  );
});

test('resolveStickyReviewPlan no-ops when dismiss-if is true and no review exists', () => {
  const plan = resolveStickyReviewPlan({
    marker: '<!-- sticky-review -->',
    dismissIf: true,
    bodyText: 'This text should be ignored',
    existingReview: null,
  });

  assert.deepEqual(plan, { action: 'noop' });
});

test('resolveStickyReviewPlan dismisses stale request changes reviews when dismiss-if is true', () => {
  const plan = resolveStickyReviewPlan({
    marker: '<!-- sticky-review -->',
    dismissIf: true,
    bodyText: 'This text should be ignored',
    existingReview: {
      id: 123,
      state: 'CHANGES_REQUESTED',
      body: '<!-- sticky-review -->\nThis PR is stacked on top of #12, please merge that one first',
    },
  });

  assert.deepEqual(plan, {
    action: 'cleanup',
    cleanup: 'dismiss',
    reviewId: 123,
  });
});

test('resolveStickyReviewPlan minimizes stale comment reviews when dismiss-if is true', () => {
  const plan = resolveStickyReviewPlan({
    marker: '<!-- sticky-review -->',
    dismissIf: true,
    bodyText: 'This text should be ignored',
    existingReview: {
      id: 456,
      state: 'COMMENTED',
      body: '<!-- sticky-review -->\nCircuit sources changed in this PR, so the artifacts hashes in config are stale.',
    },
  });

  assert.deepEqual(plan, {
    action: 'cleanup',
    cleanup: 'minimize',
    reviewId: 456,
  });
});

test('resolveStickyReviewPlan creates a review when dismiss-if is false and body exists', () => {
  const plan = resolveStickyReviewPlan({
    marker: '<!-- sticky-review -->',
    dismissIf: false,
    bodyText: 'This PR is stacked on top of #12, please merge that one first',
    existingReview: null,
  });

  assert.deepEqual(plan, {
    action: 'create',
    body: [
      '<!-- sticky-review -->',
      'This PR is stacked on top of #12, please merge that one first',
    ].join('\n'),
  });
});
