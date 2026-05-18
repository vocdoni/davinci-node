function formatDelta(base, head) {
  const delta = head - base;
  return delta > 0 ? `+${delta}` : `${delta}`;
}

function formatValue(value) {
  return value === undefined || value === null ? 'n/a' : `${value}`;
}

const METRICS = [
  { label: 'nbConstraints', field: 'nbConstraints' },
  { label: 'nbPublicInputs', field: 'nbPublicInputs' },
  { label: 'nbPrivateInputs', field: 'nbPrivateInputs' },
];

function formatReviewBody({ marker, baseBranch, baselineSource, diffs }) {
  return [
    marker,
    `Circuit constraint counts changed compared to base branch \`${baseBranch}\` (${baselineSource}).`,
    '',
    '| Circuit | Metric | Base | PR | Diff |',
    '| --- | --- | ---: | ---: | ---: |',
    ...diffs.flatMap(({ circuit, base, head }) =>
      METRICS.map(({ label, field }) => {
        const baseValue = formatValue(base?.[field]);
        const headValue = formatValue(head?.[field]);
        const diff = base && head ? formatDelta(base[field], head[field]) : 'n/a';

        return `| ${circuit} | ${label} | ${baseValue} | ${headValue} | ${diff} |`;
      })
    ),
  ].join('\n');
}

function formatNoDiffReviewBody({ marker, baseBranch, baselineSource }) {
  return [
    marker,
    `This PR no longer changes circuit constraints compared to base branch \`${baseBranch}\` (${baselineSource}).`,
  ].join('\n');
}

module.exports = {
  formatReviewBody,
  formatNoDiffReviewBody,
};
