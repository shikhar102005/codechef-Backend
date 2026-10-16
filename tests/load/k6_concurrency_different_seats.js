import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    different_seats: {
      executor: 'per-vu-iterations',
      vus: 20,
      iterations: 1,
      maxDuration: '15s',
    },
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const sessionId = 'b0000000-0000-0000-0000-000000000001';

  // Fetch available seats
  const seatsRes = http.get(`${BASE_URL}/api/v1/sessions/${sessionId}/seats`);
  const seats = seatsRes.json().seats;
  if (!seats || seats.length < 20) return;

  // Each VU targets its own distinct seat index (__VU - 1)
  const targetSeat = seats[__VU - 1];

  const payload = JSON.stringify({
    session_id: sessionId,
    seat_ids: [targetSeat.id],
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer demo-alice',
    },
  };

  const res = http.post(`${BASE_URL}/api/v1/reservations`, payload, params);

  check(res, {
    'reservation succeeded with 201': (r) => r.status === 201,
  });
}
