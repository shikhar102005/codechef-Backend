import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

export const options = {
  scenarios: {
    contention: {
      executor: 'shared-iterations',
      vus: 50,
      iterations: 50,
      maxDuration: '10s',
    },
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const successfulHolds = new Counter('successful_holds');
const conflictsPrevented = new Counter('conflicts_prevented');

export default function () {
  const sessionId = 'b0000000-0000-0000-0000-000000000001';

  // 1. Get available seats
  const seatsRes = http.get(`${BASE_URL}/api/v1/sessions/${sessionId}/seats`);
  const seats = seatsRes.json().seats;
  if (!seats || seats.length === 0) return;

  const targetSeatId = seats[0].id;

  // 2. All 50 virtual users attempt to reserve the exact same seat simultaneously
  const payload = JSON.stringify({
    session_id: sessionId,
    seat_ids: [targetSeatId],
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer demo-alice',
    },
  };

  const res = http.post(`${BASE_URL}/api/v1/reservations`, payload, params);

  if (res.status === 201) {
    successfulHolds.add(1);
  } else if (res.status === 409) {
    conflictsPrevented.add(1);
  }

  check(res, {
    'status is 201 or 409': (r) => r.status === 201 || r.status === 409,
  });
}
