function stripAnsi(line) {
  return line.replace(/\u001B\[[0-9;]*[A-Za-z]/g, '');
}

function parseStructuredMetrics(line) {
  if (!line.includes('circuit definition compiled')) {
    return null;
  }

  const circuit = line.match(/\bcircuit=([^\s]+)/)?.[1];
  const nbConstraints = Number(line.match(/\bnbConstraints=(\d+)/)?.[1]);
  const nbPublic = Number(line.match(/\bnbPublic=(\d+)/)?.[1]);
  const nbSecret = Number(line.match(/\bnbSecret=(\d+)/)?.[1]);

  if (
    !circuit ||
    Number.isNaN(nbConstraints) ||
    Number.isNaN(nbPublic) ||
    Number.isNaN(nbSecret)
  ) {
    return null;
  }

  return {
    circuit,
    metrics: { nbConstraints, nbPublic, nbSecret },
  };
}

function parseLegacyStart(line) {
  if (!line.includes('compiling circuit definition')) {
    return null;
  }

  return line.match(/\bcircuit=([^\s]+)/)?.[1] ?? null;
}

function parseLegacyPublicSecret(line) {
  if (!line.includes('parsed circuit inputs')) {
    return null;
  }

  const nbPublic = Number(line.match(/\bnbPublic=(\d+)/)?.[1]);
  const nbSecret = Number(line.match(/\bnbSecret=(\d+)/)?.[1]);
  if (Number.isNaN(nbPublic) || Number.isNaN(nbSecret)) {
    return null;
  }

  return { nbPublic, nbSecret };
}

function parseLegacyConstraints(line) {
  if (!line.includes('building constraint builder')) {
    return null;
  }

  const nbConstraints = Number(line.match(/\bnbConstraints=(\d+)/)?.[1]);
  if (Number.isNaN(nbConstraints)) {
    return null;
  }

  return { nbConstraints };
}

function parseCompileLog(log) {
  const metrics = {};
  const legacyState = {};
  let currentCircuit = null;

  for (const rawLine of log.split(/\r?\n/)) {
    const line = stripAnsi(rawLine);

    const structured = parseStructuredMetrics(line);
    if (structured) {
      metrics[structured.circuit] = structured.metrics;
      continue;
    }

    const legacyCircuit = parseLegacyStart(line);
    if (legacyCircuit) {
      currentCircuit = legacyCircuit;
      legacyState[currentCircuit] = legacyState[currentCircuit] ?? {};
      continue;
    }

    if (!currentCircuit) {
      continue;
    }

    const currentState = legacyState[currentCircuit] ?? {};
    const publicSecret = parseLegacyPublicSecret(line);
    if (publicSecret) {
      legacyState[currentCircuit] = {
        ...currentState,
        ...publicSecret,
      };
    }

    const constraints = parseLegacyConstraints(line);
    if (constraints) {
      legacyState[currentCircuit] = {
        ...legacyState[currentCircuit],
        ...constraints,
      };
    }

    const finalized = legacyState[currentCircuit];
    if (
      finalized &&
      finalized.nbConstraints !== undefined &&
      finalized.nbPublic !== undefined &&
      finalized.nbSecret !== undefined &&
      !metrics[currentCircuit]
    ) {
      metrics[currentCircuit] = {
        nbConstraints: finalized.nbConstraints,
        nbPublic: finalized.nbPublic,
        nbSecret: finalized.nbSecret,
      };
    }
  }

  return metrics;
}

module.exports = {
  parseCompileLog,
};
