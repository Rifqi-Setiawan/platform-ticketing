import http from 'k6/http';
import { Counter, Trend } from 'k6/metrics';

const vus = Number(__ENV.VUS);
const iterations = Number(__ENV.ITERATIONS_PER_VU);
const expected = Math.min(Number(__ENV.CAPACITY), vus * iterations);
const created = new Counter('holds_created');
const soldOut = new Counter('holds_sold_out');
const unexpected = new Counter('unexpected_responses');
const latency = new Trend('reservation_ms', true);

export const options = {
  scenarios: { holds: { executor: 'per-vu-iterations', vus, iterations, maxDuration: '4m' } },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'max'],
  thresholds: {
    holds_created: [`count==${expected}`],
    holds_sold_out: [`count==${vus * iterations - expected}`],
    unexpected_responses: ['count==0'],
  },
};

http.setResponseCallback(http.expectedStatuses(201, 409));
export default function () {
  const res = http.post(__ENV.BASE_URL + '/v1/holds', JSON.stringify({ sectionId: __ENV.SECTION_ID }), {
    headers: {
      'Content-Type': 'application/json',
      'Cookie': 'tribunpass_session=' + __ENV.SESSION_TOKEN,
      'Idempotency-Key': `bench-${__VU}-${__ITER}`,
    },
  });
  const issued = res.status === 201 && !!res.json('holdId');
  const exhausted = res.status === 409 && res.json('error.code') === 'SOLD_OUT';
  created.add(issued ? 1 : 0);
  soldOut.add(exhausted ? 1 : 0);
  unexpected.add(issued || exhausted ? 0 : 1);
  latency.add(res.timings.duration);
}

export function handleSummary(data) {
  const m = data.metrics;
  const result = {
    profile: __ENV.PROFILE, vus, iterationsPerVU: iterations, capacity: Number(__ENV.CAPACITY),
    requests: m.http_reqs.values.count, requestsPerSecond: m.http_reqs.values.rate,
    p50ms: m.reservation_ms.values.med, p95ms: m.reservation_ms.values['p(95)'],
    created: m.holds_created.values.count, soldOut: m.holds_sold_out.values.count,
    unexpected: m.unexpected_responses.values.count,
  };
  const json = JSON.stringify(result, null, 2) + '\n';
  return { stdout: json, [__ENV.RESULT_PATH]: json };
}
