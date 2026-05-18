function stripAnsi(line) {
  return line.replace(/\u001B\[[0-9;]*[A-Za-z]/g, '');
}

function parseFirstAvailableNumber(line, fieldNames) {
  for (const fieldName of fieldNames) {
    const value = Number(line.match(new RegExp(`\\b${fieldName}=(\\d+)`))?.[1]);
    if (!Number.isNaN(value)) {
      return value;
    }
  }

  return Number.NaN;
}

function parseStructuredMetrics(line) {
  if (!line.includes('circuit definition compiled')) {
    return null;
  }

  const circuit = line.match(/\bcircuit=([^\s]+)/)?.[1];
  const nbConstraints = Number(line.match(/\bnbConstraints=(\d+)/)?.[1]);
  const nbPublicInputs = parseFirstAvailableNumber(line, ['nbPublicInputs', 'nbPublic']);
  const nbPrivateInputs = parseFirstAvailableNumber(line, ['nbPrivateInputs', 'nbSecret']);

  if (
    !circuit ||
    Number.isNaN(nbConstraints) ||
    Number.isNaN(nbPublicInputs) ||
    Number.isNaN(nbPrivateInputs)
  ) {
    return null;
  }

  return {
    circuit,
    metrics: { nbConstraints, nbPublicInputs, nbPrivateInputs },
  };
}

function parseCompileLog(log) {
  const metrics = {};

  for (const rawLine of log.split(/\r?\n/)) {
    const line = stripAnsi(rawLine);
    const structured = parseStructuredMetrics(line);
    if (structured) {
      metrics[structured.circuit] = structured.metrics;
    }
  }

  return metrics;
}

module.exports = {
  parseCompileLog,
};
