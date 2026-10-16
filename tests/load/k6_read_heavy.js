import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '10s', target: 20 },
    { duration: '20s', target: 50 },
    { duration: '10s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<200'], // 95% of requests should be below 200ms
    http_req_failed: ['rate<0.01'],    // Less than 1% failure rate
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  // 1. Fetch Events
  const eventsRes = http.get(`${BASE_URL}/api/v1/events`);
  check(eventsRes, {
    'events status is 200': (r) => r.status === 200,
    'events list returned': (r) => r.json().events.length > 0,
  });

  const sessionId = 'b0000000-0000-0000-0000-000000000001';

  // 2. Fetch Session Seats
  const seatsRes = http.get(`${BASE_URL}/api/v1/sessions/${sessionId}/seats`);
  check(seatsRes, {
    'seats status is 200': (r) => r.status === 200,
    'seats list returned': (r) => r.json().seats.length > 0,
  });

  sleep(0.5);
}
