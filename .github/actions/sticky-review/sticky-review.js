function formatStickyReviewBody({ marker, body }) {
  return [marker, body].join('\n');
}

function resolveStickyReviewPlan({
  marker,
  bodyText,
  dismissIf = false,
  existingReview,
  isMinimized = false,
}) {
  if (dismissIf) {
    if (!existingReview) {
      return { action: 'noop' };
    }

    return {
      action: 'cleanup',
      cleanup: existingReview.state === 'CHANGES_REQUESTED' ? 'dismiss' : 'minimize',
      reviewId: existingReview.id,
    };
  }

  const normalizedBodyText = typeof bodyText === 'string' ? bodyText : '';
  if (normalizedBodyText.trim() === '') {
    return { action: 'noop' };
  }

  const body = formatStickyReviewBody({ marker, body: normalizedBodyText });

  if (!existingReview) {
    return { action: 'create', body };
  }

  if (existingReview.body !== body) {
    return {
      action: 'update',
      body,
      reviewId: existingReview.id,
      restore: isMinimized,
    };
  }

  if (isMinimized) {
    return {
      action: 'restore',
      body,
      reviewId: existingReview.id,
    };
  }

  return { action: 'noop' };
}

module.exports = {
  formatStickyReviewBody,
  resolveStickyReviewPlan,
};
